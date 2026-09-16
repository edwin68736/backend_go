package service

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"tukifac/pkg/database"
	"tukifac/pkg/gormutil"
	"tukifac/pkg/modifierkind"
	"tukifac/pkg/money"
	"tukifac/pkg/saleunit"
	"tukifac/pkg/sunat"
	"tukifac/pkg/tax"

	"gorm.io/gorm"
)

type ProductService struct {
	db *gorm.DB
}

func NewProductService(db *gorm.DB) *ProductService {
	return &ProductService{db: db}
}

type ProductListParams struct {
	Query                    string
	CategoryID               uint
	BrandID                  uint
	Type                     string
	ActiveOnly               bool
	InactiveOnly             bool   // solo productos inactivos (panel restaurante)
	ManageStockOnly          bool   // solo productos con manage_stock (para transferencias/inventario)
	NoManageStockOnly        bool   // solo productos sin control de stock (reporte restaurante)
	RestaurantOnly           bool   // solo productos con is_restaurant (para panel restaurante)
	CombosOnly               bool   // solo combos (has_combo)
	ExcludeCombos            bool   // sin combos: candidatos a componente de un combo
	PreparationArea          string // filtrar por slug (legacy)
	PreparationAreaID        uint   // filtrar por FK
	StockLessThan            *float64
	MinPrice                 *float64 // filtro de rango de precio (tienda pública)
	MaxPrice                 *float64
	ShowInDigitalCatalogOnly bool // solo productos publicados en el Catálogo Digital (tienda pública)
	BranchID                 uint // >0: restaurante → tenant_products.branch_id; inventario ERP → stock en sucursal
	Limit                    int  // 0 = sin límite (comportamiento anterior)
	Offset                   int
	SortBy                   string // id, code, name, category, price, stock
	SortDir                  string // asc, desc
}

const maxReportSerialsPerProduct = 120

// BranchStockRow es una fila de stock por sucursal (reportes).
type BranchStockRow struct {
	BranchID   uint    `json:"branch_id"`
	BranchName string  `json:"branch_name"`
	Quantity   float64 `json:"quantity"`
}

// ProductListItem producto en listados API con nombre de categoría/marca.
type ProductListItem struct {
	database.TenantProduct
	CategoryName string `json:"category_name,omitempty"`
	BrandName    string `json:"brand_name,omitempty"`
}

// ProductReportItem extiende el producto con totales, stock por sucursal y series.
type ProductReportItem struct {
	database.TenantProduct
	CategoryName  string           `json:"category_name"`
	StockTotal    float64          `json:"stock_total"`
	StockByBranch []BranchStockRow `json:"stock_by_branch"`
	Serials       []string         `json:"serials"`
	SerialCount   int              `json:"serial_count"`
}

func (s *ProductService) buildListQuery(params ProductListParams) *gorm.DB {
	const p = "tenant_products."
	q := s.db.Model(&database.TenantProduct{})
	if params.Query != "" {
		q = q.Where(p+"name LIKE ? OR "+p+"code LIKE ? OR "+p+"description LIKE ?",
			"%"+params.Query+"%", "%"+params.Query+"%", "%"+params.Query+"%")
	}
	if params.CategoryID > 0 {
		q = q.Where(p+"category_id = ?", params.CategoryID)
	}
	if params.BrandID > 0 {
		q = q.Where(p+"brand_id = ?", params.BrandID)
	}
	t := strings.ToLower(strings.TrimSpace(params.Type))
	if t != "" {
		switch t {
		case "product":
			// Catálogo de bienes: filas creadas antes de `type` quedan NULL o ''; no deben excluirse del listado.
			q = q.Where("("+p+"type IS NULL OR TRIM(COALESCE("+p+"type, '')) = '' OR LOWER(TRIM("+p+"type)) = ?)", "product")
		case "service":
			q = q.Where("LOWER(TRIM(COALESCE("+p+"type, ''))) = ?", "service")
		default:
			q = q.Where(p+"type = ?", params.Type)
		}
	}
	if params.InactiveOnly {
		q = q.Where(p+"active = ?", false)
	} else if params.ActiveOnly {
		q = q.Where(p+"active = ?", true)
	}
	if params.ManageStockOnly {
		q = q.Where(p+"manage_stock = ?", true)
	} else if params.NoManageStockOnly {
		q = q.Where(p+"manage_stock = ?", false)
	}
	if params.RestaurantOnly {
		q = q.Where(p+"is_restaurant = ?", true)
	}
	if params.ShowInDigitalCatalogOnly {
		q = q.Where(p+"show_in_digital_catalog = ?", true)
	}
	if params.MinPrice != nil {
		q = q.Where(p+"sale_price >= ?", *params.MinPrice)
	}
	if params.MaxPrice != nil {
		q = q.Where(p+"sale_price <= ?", *params.MaxPrice)
	}
	// has_combo puede ser NULL en filas anteriores a V099: tratarlas como no-combo.
	if params.CombosOnly {
		q = q.Where(p+"has_combo = ?", true)
	} else if params.ExcludeCombos {
		q = q.Where("COALESCE("+p+"has_combo, ?) = ?", false, false)
	}
	if params.PreparationAreaID > 0 {
		q = q.Where(p+"preparation_area_id = ?", params.PreparationAreaID)
	} else if params.PreparationArea != "" {
		q = q.Where(p+"preparation_area = ?", params.PreparationArea)
	}
	if params.BranchID > 0 {
		bid := params.BranchID
		// Un producto con branch_id propio (>0) queda exclusivo de esa sucursal en CUALQUIER
		// listado filtrado por sucursal — antes esto solo se respetaba con RestaurantOnly
		// (carta Tukichef); POS/inventario/listado general de Tukifac ignoraban por completo
		// el branch_id del producto y mostraban todo sin importar la sucursal activa, aunque
		// el tenant hubiera asignado sus productos a una sucursal específica. branch_id vacío/0
		// sigue significando "disponible en todas las sucursales" (comportamiento de siempre
		// para el comercio general, que no asigna productos por sucursal).
		q = q.Where(p+"branch_id IS NULL OR "+p+"branch_id = 0 OR "+p+"branch_id = ?", bid)
		if !params.RestaurantOnly {
			// Fuera de la carta, además hay que respetar el stock por sucursal para productos
			// que sí lo gestionan (comportamiento sin cambios).
			// Productos con variantes: el stock vive en tenant_product_presentation_stocks, no en
			// tenant_product_stocks — sin este OR, un producto con variantes nunca tiene fila en
			// la tabla vieja y quedaría invisible en cualquier listado filtrado por sucursal
			// (transferencias, POS, etc.) aunque sí tenga stock real en alguna presentación.
			q = q.Where(`(`+p+`manage_stock = ? OR EXISTS (
				SELECT 1 FROM tenant_product_stocks s WHERE s.product_id = tenant_products.id AND s.branch_id = ?
			) OR EXISTS (
				SELECT 1 FROM tenant_product_presentation_stocks ps
				JOIN tenant_product_presentations pr ON pr.id = ps.presentation_id AND pr.deleted_at IS NULL
				WHERE pr.product_id = tenant_products.id AND ps.branch_id = ?
			))`, false, bid, bid)
		}
	}
	if params.StockLessThan != nil {
		thr := *params.StockLessThan
		// Productos con variantes: sumar tenant_product_presentation_stocks en vez de la tabla
		// vieja (que para estos productos queda congelada en 0 y los marcaría siempre "bajo stock").
		if params.BranchID > 0 {
			bid := params.BranchID
			q = q.Where(p+"manage_stock = ?", true).
				Where(`(CASE WHEN `+p+`has_variants THEN COALESCE((
						SELECT SUM(ps.quantity) FROM tenant_product_presentation_stocks ps
						JOIN tenant_product_presentations pr ON pr.id = ps.presentation_id AND pr.deleted_at IS NULL
						WHERE pr.product_id = tenant_products.id AND ps.branch_id = ?
					), 0) ELSE COALESCE((
						SELECT s.quantity FROM tenant_product_stocks s
						WHERE s.product_id = tenant_products.id AND s.branch_id = ?
						LIMIT 1
					), 0) END) < ?`, bid, bid, thr)
		} else {
			q = q.Where(p+"manage_stock = ?", true).
				Where(`(CASE WHEN `+p+`has_variants THEN COALESCE((
						SELECT SUM(ps.quantity) FROM tenant_product_presentation_stocks ps
						JOIN tenant_product_presentations pr ON pr.id = ps.presentation_id AND pr.deleted_at IS NULL
						WHERE pr.product_id = tenant_products.id
					), 0) ELSE COALESCE((
						SELECT SUM(s.quantity) FROM tenant_product_stocks s WHERE s.product_id = tenant_products.id
					), 0) END) < ?`, thr)
		}
	}
	return q
}

func (s *ProductService) applyProductListOrder(q *gorm.DB, params ProductListParams) *gorm.DB {
	col := strings.ToLower(strings.TrimSpace(params.SortBy))
	if col == "" {
		col = "id"
	}
	dir := "DESC"
	if strings.EqualFold(params.SortDir, "asc") {
		dir = "ASC"
	} else if col == "id" && strings.TrimSpace(params.SortDir) == "" {
		dir = "DESC"
	}

	tie := ", tenant_products.id DESC"
	switch col {
	case "code":
		return q.Order("tenant_products.code " + dir + tie)
	case "name":
		return q.Order("tenant_products.name " + dir + tie)
	case "category":
		q = q.Joins("LEFT JOIN tenant_categories ON tenant_categories.id = tenant_products.category_id")
		return q.Order("COALESCE(tenant_categories.name, '') " + dir + tie)
	case "price":
		return q.Order("tenant_products.sale_price " + dir + tie)
	case "stock":
		if params.BranchID > 0 {
			stockExpr := fmt.Sprintf(
				"COALESCE((SELECT s.quantity FROM tenant_product_stocks s WHERE s.product_id = tenant_products.id AND s.branch_id = %d LIMIT 1), 0)",
				params.BranchID,
			)
			return q.Order(stockExpr + " " + dir + tie)
		}
		stockExpr := "COALESCE((SELECT SUM(s.quantity) FROM tenant_product_stocks s WHERE s.product_id = tenant_products.id), 0)"
		return q.Order(stockExpr + " " + dir + tie)
	case "id":
		return q.Order("tenant_products.id " + dir)
	default:
		return q.Order("tenant_products.id DESC")
	}
}

func (s *ProductService) List(params ProductListParams) ([]database.TenantProduct, int64, error) {
	var products []database.TenantProduct
	q := s.buildListQuery(params)

	var total int64
	if params.Limit > 0 {
		if err := q.Count(&total).Error; err != nil {
			return nil, 0, err
		}
		q = q.Offset(params.Offset).Limit(params.Limit)
	}
	q = s.applyProductListOrder(q, params)
	err := q.Find(&products).Error
	return products, total, err
}

// ListWithCategoryNames igual que List con category_name para el panel tenant.
func (s *ProductService) ListWithCategoryNames(params ProductListParams) ([]ProductListItem, int64, error) {
	products, total, err := s.List(params)
	if err != nil {
		return nil, 0, err
	}
	return s.attachCategoryNames(products), total, nil
}

func (s *ProductService) attachCategoryNames(products []database.TenantProduct) []ProductListItem {
	if len(products) == 0 {
		return nil
	}
	catName := map[uint]string{}
	seenCat := map[uint]struct{}{}
	var catIDs []uint
	brandName := map[uint]string{}
	seenBrand := map[uint]struct{}{}
	var brandIDs []uint
	for _, p := range products {
		if p.CategoryID != nil {
			cid := *p.CategoryID
			if _, ok := seenCat[cid]; !ok {
				seenCat[cid] = struct{}{}
				catIDs = append(catIDs, cid)
			}
		}
		if p.BrandID != nil {
			bid := *p.BrandID
			if _, ok := seenBrand[bid]; !ok {
				seenBrand[bid] = struct{}{}
				brandIDs = append(brandIDs, bid)
			}
		}
	}
	if len(catIDs) > 0 {
		var cats []database.TenantCategory
		s.db.Where("id IN ?", catIDs).Find(&cats)
		for _, c := range cats {
			catName[c.ID] = c.Name
		}
	}
	if len(brandIDs) > 0 {
		var brands []database.TenantBrand
		s.db.Where("id IN ?", brandIDs).Find(&brands)
		for _, b := range brands {
			brandName[b.ID] = b.Name
		}
	}
	out := make([]ProductListItem, len(products))
	for i, p := range products {
		item := ProductListItem{TenantProduct: p}
		if p.CategoryID != nil {
			item.CategoryName = catName[*p.CategoryID]
		}
		if p.BrandID != nil {
			item.BrandName = brandName[*p.BrandID]
		}
		out[i] = item
	}
	return out
}

// ProductListItemFrom devuelve un ítem de listado con category_name para un solo producto.
func (s *ProductService) ProductListItemFrom(p database.TenantProduct) ProductListItem {
	items := s.attachCategoryNames([]database.TenantProduct{p})
	if len(items) == 0 {
		return ProductListItem{TenantProduct: p}
	}
	return items[0]
}

// ListReport igual que List pero devuelve filas enriquecidas (stock por sucursal, series, categoría).
func (s *ProductService) ListReport(params ProductListParams) ([]ProductReportItem, int64, error) {
	var products []database.TenantProduct
	q := s.buildListQuery(params)

	var total int64
	if params.Limit > 0 {
		if err := q.Count(&total).Error; err != nil {
			return nil, 0, err
		}
		q = q.Offset(params.Offset).Limit(params.Limit)
	}
	q = s.applyProductListOrder(q, params)
	if err := q.Find(&products).Error; err != nil {
		return nil, 0, err
	}
	return s.enrichReport(products, params.BranchID), total, nil
}

func (s *ProductService) enrichReport(products []database.TenantProduct, branchID uint) []ProductReportItem {
	if len(products) == 0 {
		return nil
	}
	ids := make([]uint, len(products))
	for i, p := range products {
		ids[i] = p.ID
	}

	catName := map[uint]string{}
	seenCat := map[uint]struct{}{}
	var catIDs []uint
	for _, p := range products {
		if p.CategoryID != nil {
			cid := *p.CategoryID
			if _, ok := seenCat[cid]; ok {
				continue
			}
			seenCat[cid] = struct{}{}
			catIDs = append(catIDs, cid)
		}
	}
	if len(catIDs) > 0 {
		var cats []database.TenantCategory
		s.db.Where("id IN ?", catIDs).Find(&cats)
		for _, c := range cats {
			catName[c.ID] = c.Name
		}
	}

	type stockScan struct {
		ProductID  uint
		BranchID   uint
		BranchName string
		Quantity   float64
	}
	var srows []stockScan
	sq := s.db.Table("tenant_product_stocks AS s").
		Select("s.product_id, s.branch_id, b.name AS branch_name, s.quantity").
		Joins("JOIN tenant_branches b ON b.id = s.branch_id").
		Where("s.product_id IN ?", ids)
	if branchID > 0 {
		sq = sq.Where("s.branch_id = ?", branchID)
	}
	_ = sq.Order("b.name ASC").Scan(&srows).Error

	stockMap := map[uint][]BranchStockRow{}
	totals := map[uint]float64{}
	for _, r := range srows {
		stockMap[r.ProductID] = append(stockMap[r.ProductID], BranchStockRow{
			BranchID: r.BranchID, BranchName: r.BranchName, Quantity: r.Quantity,
		})
		totals[r.ProductID] += r.Quantity
	}

	// Productos con variantes: el stock vive por presentación, no en tenant_product_stocks.
	// Se descarta lo que haya salido de la consulta anterior (puede ser un total "congelado" de
	// antes de tener presentaciones con stock propio) y se recalcula desde las presentaciones.
	variantIDs := make([]uint, 0)
	for _, p := range products {
		if p.HasVariants {
			variantIDs = append(variantIDs, p.ID)
		}
	}
	if len(variantIDs) > 0 {
		for _, pid := range variantIDs {
			delete(totals, pid)
			delete(stockMap, pid)
		}
		var prows []stockScan
		pq := s.db.Table("tenant_product_presentation_stocks AS ps").
			Select("pr.product_id, ps.branch_id, b.name AS branch_name, SUM(ps.quantity) AS quantity").
			Joins("JOIN tenant_product_presentations pr ON pr.id = ps.presentation_id AND pr.deleted_at IS NULL").
			Joins("JOIN tenant_branches b ON b.id = ps.branch_id").
			Where("pr.product_id IN ?", variantIDs).
			Group("pr.product_id, ps.branch_id, b.name")
		if branchID > 0 {
			pq = pq.Where("ps.branch_id = ?", branchID)
		}
		_ = pq.Scan(&prows).Error
		for _, r := range prows {
			stockMap[r.ProductID] = append(stockMap[r.ProductID], BranchStockRow{
				BranchID: r.BranchID, BranchName: r.BranchName, Quantity: r.Quantity,
			})
			totals[r.ProductID] += r.Quantity
		}
	}

	seriesIDs := make([]uint, 0)
	for _, p := range products {
		if p.ManageSeries {
			seriesIDs = append(seriesIDs, p.ID)
		}
	}
	serialByProduct := map[uint][]string{}
	serialCountByProduct := map[uint]int{}
	if len(seriesIDs) > 0 {
		var serials []database.TenantProductSerial
		qser := s.db.Model(&database.TenantProductSerial{}).Where("product_id IN ?", seriesIDs)
		if branchID > 0 {
			qser = qser.Where("branch_id = ?", branchID)
		}
		_ = qser.Order("serial ASC").Find(&serials).Error
		for _, ser := range serials {
			serialCountByProduct[ser.ProductID]++
			if len(serialByProduct[ser.ProductID]) < maxReportSerialsPerProduct {
				serialByProduct[ser.ProductID] = append(serialByProduct[ser.ProductID], ser.Serial)
			}
		}
	}

	out := make([]ProductReportItem, len(products))
	for i, p := range products {
		cn := ""
		if p.CategoryID != nil {
			cn = catName[*p.CategoryID]
		}
		br := stockMap[p.ID]
		if br == nil {
			br = make([]BranchStockRow, 0)
		}
		ser := serialByProduct[p.ID]
		if ser == nil {
			ser = make([]string, 0)
		}
		sc := 0
		if p.ManageSeries {
			sc = serialCountByProduct[p.ID]
		}
		out[i] = ProductReportItem{
			TenantProduct: p,
			CategoryName:  cn,
			StockTotal:    totals[p.ID],
			StockByBranch: br,
			Serials:       ser,
			SerialCount:   sc,
		}
	}
	return out
}

func (s *ProductService) GetByID(id uint) (*database.TenantProduct, error) {
	var p database.TenantProduct
	err := s.db.First(&p, id).Error
	return &p, err
}

func (s *ProductService) GetByCode(code string) (*database.TenantProduct, error) {
	var p database.TenantProduct
	err := s.db.Where("code = ?", code).First(&p).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &p, err
}

// findProductByCodeUnscoped busca por código incluyendo productos ocultos (soft delete).
func (s *ProductService) findProductByCodeUnscoped(code string, branchID uint, scopeBranch bool) (*database.TenantProduct, error) {
	if strings.TrimSpace(code) == "" {
		return nil, nil
	}
	var p database.TenantProduct
	q := s.db.Unscoped().Where("code = ?", code)
	if scopeBranch && branchID > 0 {
		q = q.Where("branch_id = ?", branchID)
	}
	err := q.First(&p).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &p, err
}

func isProductSoftDeleted(p *database.TenantProduct) bool {
	return p != nil && p.DeletedAt.Valid
}

func (s *ProductService) restoreSoftDeletedProduct(id uint) error {
	return s.db.Unscoped().Model(&database.TenantProduct{}).Where("id = ?", id).Update("deleted_at", nil).Error
}

// GetByCodeInBranch busca por código dentro de la sucursal (catálogo restaurante).
func (s *ProductService) GetByCodeInBranch(code string, branchID uint) (*database.TenantProduct, error) {
	if code == "" {
		return nil, nil
	}
	var p database.TenantProduct
	q := s.db.Where("code = ?", code)
	if branchID > 0 {
		q = q.Where("branch_id = ?", branchID)
	}
	err := q.First(&p).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &p, err
}

// EnsureRestaurantBranchAccess valida que un producto asignado a una sucursal específica
// (branch_id > 0) pertenezca a la sucursal activa. Antes solo se exigía para platos de
// restaurante (IsRestaurant) — igual que el filtro de listado (ver buildListQuery), un
// producto de comercio general asignado a una sucursal también debe quedar exclusivo de
// ella, no solo la carta de Tukichef.
func (s *ProductService) EnsureRestaurantBranchAccess(p *database.TenantProduct, branchID uint) error {
	if p == nil || branchID == 0 {
		return nil
	}
	if p.BranchID == 0 {
		return nil
	}
	if p.BranchID != branchID {
		return errors.New("el producto no pertenece a la sucursal activa")
	}
	return nil
}

type ProductInput struct {
	CategoryID           *uint
	BrandID              *uint
	Code                 string
	Name                 string
	Description          string
	Type                 string
	Unit                 string
	// UnitID: si viene, manda sobre Unit (el catálogo por ID es la fuente de verdad para altas/
	// ediciones desde la UI). Si viene nil, se resuelve/crea a partir de Unit (compatibilidad con
	// importación masiva y clientes de API que todavía mandan solo texto) — ver resolveUnitReference.
	UnitID               *uint
	SalePrice            float64
	PurchasePrice        float64
	TaxRate              float64
	IgvAffectationType   string
	PriceIncludesIgv     bool
	ManageStock          bool
	ManageSeries         bool
	HasVariants          bool
	HasModifiers         bool
	IsRestaurant         bool
	ShowInDigitalCatalog bool
	PreparationAreaID    *uint
	PreparationArea      string // slug legacy; se sincroniza desde preparation_area_id
	MinStock             float64
	HasExpiryDate        bool
	ExpiryDate           *time.Time
	ImageURL             string
	ImageURLSet          bool // si true, Update actualiza image_url; si no, conserva la imagen actual
	Active               bool
	ActiveSet            bool // si true, Update actualiza el campo active
	BranchID             uint // sucursal dueña (platos restaurante)
	// nil = no tocar vínculos (update parcial); no-nil = reemplazar asignación (puede ser slice vacío).
	ModifierGroupIDs *[]uint
	// nil = no tocar presentaciones; no-nil = reemplazar lista del producto.
	Presentations *[]ProductPresentationInput
	// nil = no tocar combo; no-nil = reemplazar grupos (slice vacío = deja de ser combo).
	ComboGroups *[]ComboGroupInput
}

// ProductPresentationInput fila de presentación propia del producto (no es grupo global).
type ProductPresentationInput struct {
	// ID: si viene informado y pertenece al producto, se actualiza esa fila en vez de recrearla
	// (preserva su stock). nil/0 = fila nueva.
	ID        *uint
	Name      string
	SalePrice float64
	SortOrder int
	// InitialStock: solo se aplica cuando la fila es NUEVA (ver PresentationSyncResult.IsNew) y el
	// producto maneja stock. Ediciones de stock posteriores van por ajuste de inventario, no por acá.
	InitialStock float64
}

// PresentationSyncResult resultado de sincronizar una fila de presentación: si es nueva, el
// caller (handler) puede sembrar su stock inicial con InitialStock.
type PresentationSyncResult struct {
	Presentation database.TenantProductPresentation
	IsNew        bool
	InitialStock float64
}

// resolveUnitReference determina el código SUNAT y el ID de catálogo (tenant_units) para un
// producto. Si unitID viene informado, el catálogo manda: se usa su Code tal cual (permite que el
// tenant use unidades propias fuera del catálogo 03 estándar, sin que NormalizeUnit las pise).
// Si no viene, se resuelve a partir del texto libre (alta antigua / importación masiva) pasando
// por sunat.NormalizeUnit como siempre, y se materializa (o reutiliza) la fila de catálogo
// correspondiente — así ningún producto queda con unit_id nulo.
func (s *ProductService) resolveUnitReference(rawUnit, itemType string, unitID *uint) (code string, id uint, err error) {
	if unitID != nil && *unitID > 0 {
		var u database.TenantUnit
		if err := s.db.First(&u, *unitID).Error; err != nil {
			return "", 0, errors.New("unidad de medida no encontrada")
		}
		return u.Code, u.ID, nil
	}
	code = sunat.NormalizeUnit(rawUnit, itemType)
	id, err = database.EnsureUnitByCode(s.db, code)
	if err != nil {
		return "", 0, err
	}
	return code, id, nil
}

func (s *ProductService) Create(input ProductInput) (*database.TenantProduct, []PresentationSyncResult, error) {
	if input.Name == "" {
		return nil, nil, errors.New("nombre es requerido")
	}
	// No se exige sale_price > 0 aquí a propósito: se permite crear un producto "base" sin
	// precio (contenedor pendiente de presentaciones, o combo que se configura después) y
	// terminarlo de armar en una edición posterior. Lo que nunca se permite es VENDERLO en 0 —
	// esa barrera está en SaleService.Create (validateSaleItemPrices), no aquí.
	// Sin código el producto no se puede facturar (SUNAT lo exige por línea) y el error
	// aparecía recién al emitir. Se completa aquí para que nunca nazca uno inutilizable.
	if err := s.ensureProductCode(&input); err != nil {
		return nil, nil, err
	}

	if input.Code != "" {
		scopeBranch := input.IsRestaurant && input.BranchID > 0
		existing, err := s.findProductByCodeUnscoped(input.Code, input.BranchID, scopeBranch)
		if err != nil {
			return nil, nil, err
		}
		if existing != nil {
			if isProductSoftDeleted(existing) {
				return s.reactivateProductFromInput(existing.ID, input)
			}
			return nil, nil, fmt.Errorf("el código '%s' ya está en uso en esta sucursal", input.Code)
		}
	}

	igvType := input.IgvAffectationType
	if igvType == "" {
		igvType = "10"
	}
	// La tasa viene del config de empresa (calculada en el handler). Para tipos no gravados
	// se fuerza a 0 como medida de seguridad.
	taxRate := input.TaxRate
	if !tax.IsGravado(igvType) {
		taxRate = 0
	}

	if err := validateProductExpiry(input.HasExpiryDate, input.ExpiryDate); err != nil {
		return nil, nil, err
	}

	p := &database.TenantProduct{
		CategoryID:           input.CategoryID,
		BrandID:              input.BrandID,
		Code:                 input.Code,
		Name:                 input.Name,
		Description:          input.Description,
		Type:                 input.Type,
		Unit:                 input.Unit,
		SalePrice:            input.SalePrice,
		PurchasePrice:        input.PurchasePrice,
		TaxRate:              taxRate,
		IgvAffectationType:   igvType,
		PriceIncludesIgv:     input.PriceIncludesIgv,
		ManageStock:          input.ManageStock,
		ManageSeries:         input.ManageSeries,
		HasVariants:          input.HasVariants,
		HasModifiers:         input.HasModifiers,
		IsRestaurant:         input.IsRestaurant,
		ShowInDigitalCatalog: input.ShowInDigitalCatalog,
		BranchID:             input.BranchID,
		PreparationAreaID:    input.PreparationAreaID,
		PreparationArea:      input.PreparationArea,
		MinStock:             input.MinStock,
		HasExpiryDate:        input.HasExpiryDate,
		ExpiryDate:           input.ExpiryDate,
		ImageURL:             input.ImageURL,
		Active:               input.Active,
	}
	// Si no viene type pero la unidad es ZZ (SUNAT servicio), tratar como servicio antes del default "product".
	if strings.TrimSpace(p.Type) == "" && strings.EqualFold(strings.TrimSpace(p.Unit), "ZZ") {
		p.Type = "service"
	}
	if p.Type == "" {
		p.Type = "product"
	}
	normalizeProductCatalogFields(p)
	if err := s.resolvePreparationAreaFields(p); err != nil {
		return nil, nil, err
	}
	unitCode, unitID, err := s.resolveUnitReference(p.Unit, p.Type, input.UnitID)
	if err != nil {
		return nil, nil, err
	}
	p.Unit = unitCode
	p.UnitID = &unitID
	if strings.EqualFold(strings.TrimSpace(p.Type), "product") && strings.EqualFold(strings.TrimSpace(p.Unit), "ZZ") {
		return nil, nil, errors.New("la unidad ZZ es solo para servicios: use Inventario → Servicios")
	}

	if err := s.db.Create(p).Error; err != nil {
		return nil, nil, err
	}
	if err := gormutil.PersistBoolWithDefault(s.db, p, "price_includes_igv", input.PriceIncludesIgv); err != nil {
		return nil, nil, err
	}
	p.PriceIncludesIgv = input.PriceIncludesIgv
	// p.ManageStock (no input.ManageStock): para un servicio, normalizeProductServiceFields ya
	// lo forzó a false — reforzar con el input crudo aquí lo desharía en el struct que se
	// devuelve (y lo serializa el handler), aunque la fila en BD sí hubiera quedado bien (este
	// helper no escribe nada cuando value=true, así que dependía en silencio de que el INSERT
	// de arriba ya lo hubiera hecho bien). Con p.ManageStock queda correcto en los dos lados
	// siempre, no solo cuando el caller no manda manage_stock=true para un servicio.
	if err := gormutil.PersistBoolWithDefault(s.db, p, "manage_stock", p.ManageStock); err != nil {
		return nil, nil, err
	}

	if input.ModifierGroupIDs != nil {
		s.syncModifierGroups(p.ID, *input.ModifierGroupIDs)
	}
	var presResults []PresentationSyncResult
	if input.Presentations != nil {
		res, err := s.syncPresentations(p.ID, *input.Presentations)
		if err != nil {
			return nil, nil, err
		}
		presResults = res
		// syncPresentations ya escribió has_variants en la fila (según haya o no presentaciones)
		// con un UPDATE aparte — reflejarlo también en el struct que se devuelve, para que la
		// respuesta del alta no diga has_variants=false justo después de crear un producto o
		// servicio CON presentaciones.
		p.HasVariants = len(presResults) > 0
	}
	if input.ComboGroups != nil {
		if err := s.syncComboGroups(p, *input.ComboGroups); err != nil {
			return nil, nil, err
		}
	}

	return p, presResults, nil
}

// reactivateProductFromInput restaura un producto oculto (soft delete) y aplica los datos del alta.
// No siembra stock inicial por presentación (edge case raro: alta con un código previamente
// eliminado); el tenant puede ajustarlo después con "Ajustar stock".
func (s *ProductService) reactivateProductFromInput(id uint, input ProductInput) (*database.TenantProduct, []PresentationSyncResult, error) {
	igvType := input.IgvAffectationType
	if igvType == "" {
		igvType = "10"
	}
	taxRate := input.TaxRate
	if !tax.IsGravado(igvType) {
		taxRate = 0
	}
	if err := validateProductExpiry(input.HasExpiryDate, input.ExpiryDate); err != nil {
		return nil, nil, err
	}
	effType := strings.TrimSpace(input.Type)
	if effType == "" {
		effType = "product"
	}
	unit := sunat.NormalizeUnit(input.Unit, effType)
	if strings.EqualFold(effType, "product") && strings.EqualFold(unit, "ZZ") {
		return nil, nil, errors.New("la unidad ZZ es solo para servicios: use Inventario → Servicios")
	}

	input.IgvAffectationType = igvType
	input.TaxRate = taxRate
	input.Type = effType
	input.Unit = unit
	input.Active = true
	input.ActiveSet = true
	// Alta que reactiva una fila oculta: la imagen del alta manda sobre la que tuviera antes.
	input.ImageURLSet = true

	if err := s.restoreSoftDeletedProduct(id); err != nil {
		return nil, nil, err
	}
	// Update() ya aplica input.Presentations internamente (syncPresentations) — no repetirlo acá,
	// o cada fila nueva (sin ID) se duplicaría en vez de quedar como una sola.
	if _, err := s.Update(id, input); err != nil {
		return nil, nil, err
	}
	if err := gormutil.PersistBoolWithDefault(s.db, &database.TenantProduct{ID: id}, "price_includes_igv", input.PriceIncludesIgv); err != nil {
		return nil, nil, err
	}
	if err := gormutil.PersistBoolWithDefault(s.db, &database.TenantProduct{ID: id}, "manage_stock", input.ManageStock); err != nil {
		return nil, nil, err
	}
	if input.ModifierGroupIDs != nil {
		s.syncModifierGroups(id, *input.ModifierGroupIDs)
	}
	p, err := s.GetByID(id)
	if err != nil {
		return nil, nil, err
	}
	return p, nil, nil
}

// normalizeProductServiceFields fuerza reglas SUNAT/ERP para filas type=service.
//
// HasVariants NO se fuerza a false: un servicio puede tener presentaciones (ej. "Corte simple"
// / "Corte + barba", "Consulta 30min" / "Consulta 1h"), igual que un producto — syncPresentations
// ya recalcula has_variants según haya o no filas en tenant_product_presentations, sin importar
// el type (ver product_service.go, syncPresentations). Lo que sí sigue sin aplicar a servicios es
// todo lo que modela inventario físico: stock, series, vencimiento, área de preparación.
func normalizeProductServiceFields(p *database.TenantProduct) {
	if !strings.EqualFold(strings.TrimSpace(p.Type), "service") {
		return
	}
	p.Type = "service"
	p.Unit = "ZZ"
	p.ManageStock = false
	p.ManageSeries = false
	p.HasModifiers = false
	p.IsRestaurant = false
	p.MinStock = 0
	p.HasExpiryDate = false
	p.ExpiryDate = nil
	p.PreparationAreaID = nil
	p.PreparationArea = ""
}

// normalizeProductCatalogFields centraliza reglas de catálogo (restaurante, stock).
// preparation_area vacío en restaurante: comandas usan "cocina" por defecto (resolveProductPreparationArea).
func normalizeProductCatalogFields(p *database.TenantProduct) {
	normalizeProductServiceFields(p)
	if !p.IsRestaurant {
		p.PreparationAreaID = nil
		p.PreparationArea = ""
	} else {
		p.PreparationArea = strings.TrimSpace(strings.ToLower(p.PreparationArea))
	}
	if !p.ManageStock {
		p.MinStock = 0
	}
	normalizeProductExpiryFields(p)
}

func (s *ProductService) Update(id uint, input ProductInput) ([]PresentationSyncResult, error) {
	var existing database.TenantProduct
	if err := s.db.First(&existing, id).Error; err != nil {
		return nil, err
	}

	igvType := input.IgvAffectationType
	if igvType == "" {
		igvType = "10"
	}
	// La tasa viene del config de empresa. Para tipos no gravados se fuerza a 0.
	taxRate := input.TaxRate
	if !tax.IsGravado(igvType) {
		taxRate = 0
	}

	effType := strings.TrimSpace(input.Type)
	if effType == "" {
		effType = existing.Type
	}
	if effType == "" {
		effType = "product"
	}

	rawUnit := strings.TrimSpace(input.Unit)
	if rawUnit == "" && input.UnitID == nil {
		rawUnit = existing.Unit
	}
	unit, unitID, err := s.resolveUnitReference(rawUnit, effType, input.UnitID)
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold(effType, "service") && strings.EqualFold(unit, "ZZ") {
		return nil, errors.New("la unidad ZZ es solo para servicios: use Inventario → Servicios")
	}

	if err := validateProductExpiry(input.HasExpiryDate, input.ExpiryDate); err != nil {
		return nil, err
	}

	draft := &database.TenantProduct{
		Type:                 effType,
		Unit:                 unit,
		IsRestaurant:         input.IsRestaurant,
		ShowInDigitalCatalog: input.ShowInDigitalCatalog,
		ManageStock:          input.ManageStock,
		PreparationAreaID:    input.PreparationAreaID,
		PreparationArea:      input.PreparationArea,
		MinStock:             input.MinStock,
		HasExpiryDate:        input.HasExpiryDate,
		ExpiryDate:           input.ExpiryDate,
		ManageSeries:         input.ManageSeries,
		HasVariants:          input.HasVariants,
		HasModifiers:         input.HasModifiers,
	}
	normalizeProductCatalogFields(draft)
	if err := s.resolvePreparationAreaFields(draft); err != nil {
		return nil, err
	}
	if strings.EqualFold(draft.Type, "service") && !strings.EqualFold(unit, draft.Unit) {
		// normalizeProductServiceFields fuerza Unit="ZZ" para servicios — si el catálogo había
		// resuelto otra cosa (p. ej. el tenant no marcó unit_id de servicio), re-resolver contra ZZ.
		unit = draft.Unit
		if zid, zerr := database.EnsureUnitByCode(s.db, "ZZ"); zerr == nil {
			unitID = zid
		}
	}

	upd := map[string]interface{}{
		"category_id":             input.CategoryID,
		"brand_id":                input.BrandID,
		"code":                    input.Code,
		"name":                    input.Name,
		"description":             input.Description,
		"type":                    draft.Type,
		"unit":                    unit,
		"unit_id":                 unitID,
		"sale_price":              input.SalePrice,
		"purchase_price":          input.PurchasePrice,
		"tax_rate":                taxRate,
		"igv_affectation_type":    igvType,
		"price_includes_igv":      input.PriceIncludesIgv,
		"manage_stock":            draft.ManageStock,
		"manage_series":           draft.ManageSeries,
		"has_variants":            draft.HasVariants,
		"has_modifiers":           draft.HasModifiers,
		"is_restaurant":           draft.IsRestaurant,
		"show_in_digital_catalog": draft.ShowInDigitalCatalog,
		"preparation_area_id":     draft.PreparationAreaID,
		"preparation_area":        draft.PreparationArea,
		"min_stock":               draft.MinStock,
		"has_expiry_date":         draft.HasExpiryDate,
		"expiry_date":             draft.ExpiryDate,
	}
	// Omitir image_url en el body significa «no tocar la imagen», no borrarla. Para quitarla
	// hay que enviarla explícitamente vacía.
	if input.ImageURLSet {
		upd["image_url"] = input.ImageURL
	}
	if input.ActiveSet {
		upd["active"] = input.Active
	}
	err = s.db.Model(&database.TenantProduct{}).Where("id = ?", id).Updates(upd).Error
	if err != nil {
		return nil, err
	}

	if input.ModifierGroupIDs != nil {
		modIDs := *input.ModifierGroupIDs
		if strings.EqualFold(effType, "service") {
			modIDs = nil
		}
		s.syncModifierGroups(id, modIDs)
	}
	var presResults []PresentationSyncResult
	if input.Presentations != nil {
		presResults, err = s.syncPresentations(id, *input.Presentations)
		if err != nil {
			return nil, err
		}
	}
	if input.ComboGroups != nil {
		if err := s.syncComboGroups(&existing, *input.ComboGroups); err != nil {
			return nil, err
		}
	}
	return presResults, nil
}

func (s *ProductService) syncModifierGroups(productID uint, groupIDs []uint) {
	filtered := s.filterExtraModifierGroupIDs(groupIDs)
	s.db.Where("product_id = ?", productID).Delete(&database.TenantProductModifierGroup{})
	for _, gid := range filtered {
		s.db.Create(&database.TenantProductModifierGroup{ProductID: productID, GroupID: gid})
	}
}

func (s *ProductService) filterExtraModifierGroupIDs(groupIDs []uint) []uint {
	if len(groupIDs) == 0 {
		return groupIDs
	}
	var groups []database.TenantModifierGroup
	s.db.Where("id IN ? AND active = ?", groupIDs, true).Find(&groups)
	out := make([]uint, 0, len(groups))
	for _, g := range groups {
		if modifierkind.IsExtra(g.Kind, g.Required, g.MultiSelect) {
			out = append(out, g.ID)
		}
	}
	return out
}

// syncPresentations aplica un upsert estable: las filas con ID existente se actualizan in-place
// (preservando su stock en TenantProductPresentationStock), las nuevas se crean, y las que ya no
// vienen en la lista se eliminan (soft-delete, vía DeletedAt del modelo). Antes esto borraba y
// recreaba TODO en cada guardado — inofensivo cuando la presentación solo tenía precio, pero
// destructivo ahora que puede tener stock e historial de movimientos ligados a su ID.
func (s *ProductService) syncPresentations(productID uint, inputs []ProductPresentationInput) ([]PresentationSyncResult, error) {
	// Validar todas antes de escribir nada: la presentación reemplaza el precio base en el POS
	// (ver TenantProductPresentation), así que un precio en 0 dejaría vender esa variante gratis
	// sin ser una bonificación real.
	for _, in := range inputs {
		name := strings.TrimSpace(in.Name)
		if name == "" {
			continue
		}
		if in.SalePrice <= 0 {
			return nil, fmt.Errorf("la presentación '%s' debe tener un precio de venta mayor a S/ 0", name)
		}
	}

	var existing []database.TenantProductPresentation
	if err := s.db.Where("product_id = ?", productID).Find(&existing).Error; err != nil {
		return nil, err
	}
	existingByID := make(map[uint]database.TenantProductPresentation, len(existing))
	for _, e := range existing {
		existingByID[e.ID] = e
	}

	keep := make(map[uint]bool, len(inputs))
	out := make([]PresentationSyncResult, 0, len(inputs))
	sortOrder := 0
	for _, in := range inputs {
		name := strings.TrimSpace(in.Name)
		if name == "" {
			continue
		}
		order := sortOrder
		if in.SortOrder > 0 {
			order = in.SortOrder
		}
		if in.ID != nil && *in.ID > 0 {
			if row, ok := existingByID[*in.ID]; ok {
				row.Name = name
				row.SalePrice = money.RoundDisplay(in.SalePrice)
				row.SortOrder = order
				row.Active = true
				if err := s.db.Save(&row).Error; err != nil {
					return nil, err
				}
				keep[row.ID] = true
				out = append(out, PresentationSyncResult{Presentation: row, IsNew: false})
				sortOrder++
				continue
			}
		}
		row := database.TenantProductPresentation{
			ProductID: productID,
			Name:      name,
			SalePrice: money.RoundDisplay(in.SalePrice),
			SortOrder: order,
			Active:    true,
		}
		if err := s.db.Create(&row).Error; err != nil {
			return nil, err
		}
		out = append(out, PresentationSyncResult{Presentation: row, IsNew: true, InitialStock: in.InitialStock})
		sortOrder++
	}

	var toRemove []uint
	for id := range existingByID {
		if !keep[id] {
			toRemove = append(toRemove, id)
		}
	}
	if len(toRemove) > 0 {
		if err := s.db.Where("id IN ?", toRemove).Delete(&database.TenantProductPresentation{}).Error; err != nil {
			return nil, err
		}
	}

	hasVariants := len(out) > 0
	if err := s.db.Model(&database.TenantProduct{}).Where("id = ?", productID).Update("has_variants", hasVariants).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (s *ProductService) ListProductPresentations(productID uint) ([]database.TenantProductPresentation, error) {
	var rows []database.TenantProductPresentation
	err := s.db.Where("product_id = ? AND active = ?", productID, true).
		Order("sort_order ASC, id ASC").
		Find(&rows).Error
	return rows, err
}

func (s *ProductService) Delete(id uint) error {
	s.db.Where("product_id = ?", id).Delete(&database.TenantProductPresentation{})
	return s.db.Delete(&database.TenantProduct{}, id).Error
}

// GetStock stock total del producto. Si tiene variantes (HasVariants), el stock vive por
// presentación (TenantProductPresentationStock) y no en TenantProductStock — sumar ambas fuentes
// duplicaría el conteo, así que para productos con variantes se usa exclusivamente la primera.
func (s *ProductService) GetStock(productID uint) float64 {
	var hasVariants bool
	s.db.Model(&database.TenantProduct{}).Where("id = ?", productID).Select("has_variants").Scan(&hasVariants)
	var total float64
	if hasVariants {
		s.db.Table("tenant_product_presentation_stocks AS ps").
			Joins("JOIN tenant_product_presentations pr ON pr.id = ps.presentation_id AND pr.deleted_at IS NULL").
			Where("pr.product_id = ?", productID).
			Select("COALESCE(SUM(ps.quantity), 0)").
			Scan(&total)
		return total
	}
	s.db.Model(&database.TenantProductStock{}).
		Where("product_id = ?", productID).
		Select("COALESCE(SUM(quantity), 0)").
		Scan(&total)
	return total
}

func (s *ProductService) GetStockByBranch(productID, branchID uint) float64 {
	var hasVariants bool
	s.db.Model(&database.TenantProduct{}).Where("id = ?", productID).Select("has_variants").Scan(&hasVariants)
	if hasVariants {
		var total float64
		s.db.Table("tenant_product_presentation_stocks AS ps").
			Joins("JOIN tenant_product_presentations pr ON pr.id = ps.presentation_id AND pr.deleted_at IS NULL").
			Where("pr.product_id = ? AND ps.branch_id = ?", productID, branchID).
			Select("COALESCE(SUM(ps.quantity), 0)").
			Scan(&total)
		return total
	}
	var stock database.TenantProductStock
	s.db.Where("product_id = ? AND branch_id = ?", productID, branchID).First(&stock)
	return stock.Quantity
}

// ========= Categorías =========

// CategoryListItem categoría con conteo de productos (panel restaurante).
type CategoryListItem struct {
	database.TenantCategory
	ProductCount int64 `json:"product_count"`
}

func (s *ProductService) nextCategorySortOrder() (int, error) {
	var maxOrder *int
	err := s.db.Model(&database.TenantCategory{}).Select("MAX(sort_order)").Scan(&maxOrder).Error
	if err != nil {
		return 0, err
	}
	if maxOrder == nil {
		return 1, nil
	}
	return *maxOrder + 1, nil
}

func (s *ProductService) ListCategories() ([]database.TenantCategory, error) {
	var cats []database.TenantCategory
	err := s.db.Where("active = ?", true).Order("sort_order ASC, name ASC").Find(&cats).Error
	return cats, err
}

func (s *ProductService) ListCategoriesWithCounts() ([]CategoryListItem, error) {
	var cats []database.TenantCategory
	if err := s.db.Order("sort_order ASC, name ASC").Find(&cats).Error; err != nil {
		return nil, err
	}
	if len(cats) == 0 {
		return nil, nil
	}
	ids := make([]uint, len(cats))
	for i, c := range cats {
		ids[i] = c.ID
	}
	type countRow struct {
		CategoryID uint
		Count      int64
	}
	var counts []countRow
	if err := s.db.Model(&database.TenantProduct{}).
		Select("category_id, COUNT(*) AS count").
		Where("category_id IN ?", ids).
		Group("category_id").
		Scan(&counts).Error; err != nil {
		return nil, err
	}
	countMap := make(map[uint]int64, len(counts))
	for _, r := range counts {
		countMap[r.CategoryID] = r.Count
	}
	out := make([]CategoryListItem, len(cats))
	for i, c := range cats {
		out[i] = CategoryListItem{TenantCategory: c, ProductCount: countMap[c.ID]}
	}
	return out, nil
}

func (s *ProductService) GetCategory(id uint) (*database.TenantCategory, error) {
	var cat database.TenantCategory
	if err := s.db.First(&cat, id).Error; err != nil {
		return nil, err
	}
	return &cat, nil
}

func (s *ProductService) CreateCategory(name, description string, sortOrder *int) (*database.TenantCategory, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("nombre de categoría requerido")
	}
	order := 0
	if sortOrder != nil {
		order = *sortOrder
	} else {
		next, err := s.nextCategorySortOrder()
		if err != nil {
			return nil, err
		}
		order = next
	}
	cat := &database.TenantCategory{Name: name, Description: strings.TrimSpace(description), SortOrder: order, Active: true}
	err := s.db.Create(cat).Error
	return cat, err
}

func (s *ProductService) UpdateCategory(id uint, name, description string, sortOrder int) (*database.TenantCategory, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("nombre de categoría requerido")
	}
	var cat database.TenantCategory
	if err := s.db.First(&cat, id).Error; err != nil {
		return nil, errors.New("categoría no encontrada")
	}
	cat.Name = name
	cat.Description = strings.TrimSpace(description)
	cat.SortOrder = sortOrder
	if err := s.db.Save(&cat).Error; err != nil {
		return nil, err
	}
	return &cat, nil
}

func (s *ProductService) DeleteCategory(id uint) error {
	var cat database.TenantCategory
	if err := s.db.First(&cat, id).Error; err != nil {
		return errors.New("categoría no encontrada")
	}
	var linked int64
	if err := s.db.Model(&database.TenantProduct{}).Where("category_id = ?", id).Count(&linked).Error; err != nil {
		return err
	}
	if linked > 0 {
		return fmt.Errorf("no se puede eliminar: hay %d producto(s) vinculados", linked)
	}
	return s.db.Delete(&cat).Error
}

// ========= Marcas =========

// BrandListItem marca con conteo de productos.
type BrandListItem struct {
	database.TenantBrand
	ProductCount int64 `json:"product_count"`
}

func (s *ProductService) nextBrandSortOrder() (int, error) {
	var maxOrder *int
	err := s.db.Model(&database.TenantBrand{}).Select("MAX(sort_order)").Scan(&maxOrder).Error
	if err != nil {
		return 0, err
	}
	if maxOrder == nil {
		return 1, nil
	}
	return *maxOrder + 1, nil
}

func (s *ProductService) ListBrands() ([]database.TenantBrand, error) {
	var brands []database.TenantBrand
	err := s.db.Where("active = ?", true).Order("sort_order ASC, name ASC").Find(&brands).Error
	return brands, err
}

func (s *ProductService) ListBrandsWithCounts() ([]BrandListItem, error) {
	var brands []database.TenantBrand
	if err := s.db.Order("sort_order ASC, name ASC").Find(&brands).Error; err != nil {
		return nil, err
	}
	if len(brands) == 0 {
		return nil, nil
	}
	ids := make([]uint, len(brands))
	for i, b := range brands {
		ids[i] = b.ID
	}
	type countRow struct {
		BrandID uint
		Count   int64
	}
	var counts []countRow
	if err := s.db.Model(&database.TenantProduct{}).
		Select("brand_id, COUNT(*) AS count").
		Where("brand_id IN ?", ids).
		Group("brand_id").
		Scan(&counts).Error; err != nil {
		return nil, err
	}
	countMap := make(map[uint]int64, len(counts))
	for _, r := range counts {
		countMap[r.BrandID] = r.Count
	}
	out := make([]BrandListItem, len(brands))
	for i, b := range brands {
		out[i] = BrandListItem{TenantBrand: b, ProductCount: countMap[b.ID]}
	}
	return out, nil
}

func (s *ProductService) GetBrand(id uint) (*database.TenantBrand, error) {
	var b database.TenantBrand
	if err := s.db.First(&b, id).Error; err != nil {
		return nil, err
	}
	return &b, nil
}

func (s *ProductService) CreateBrand(name, description string, sortOrder *int) (*database.TenantBrand, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("nombre de marca requerido")
	}
	order := 0
	if sortOrder != nil {
		order = *sortOrder
	} else {
		next, err := s.nextBrandSortOrder()
		if err != nil {
			return nil, err
		}
		order = next
	}
	b := &database.TenantBrand{Name: name, Description: strings.TrimSpace(description), SortOrder: order, Active: true}
	err := s.db.Create(b).Error
	return b, err
}

func (s *ProductService) UpdateBrand(id uint, name, description string, sortOrder int) (*database.TenantBrand, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("nombre de marca requerido")
	}
	var b database.TenantBrand
	if err := s.db.First(&b, id).Error; err != nil {
		return nil, errors.New("marca no encontrada")
	}
	b.Name = name
	b.Description = strings.TrimSpace(description)
	b.SortOrder = sortOrder
	if err := s.db.Save(&b).Error; err != nil {
		return nil, err
	}
	return &b, nil
}

func (s *ProductService) DeleteBrand(id uint) error {
	var b database.TenantBrand
	if err := s.db.First(&b, id).Error; err != nil {
		return errors.New("marca no encontrada")
	}
	var linked int64
	if err := s.db.Model(&database.TenantProduct{}).Where("brand_id = ?", id).Count(&linked).Error; err != nil {
		return err
	}
	if linked > 0 {
		return fmt.Errorf("no se puede eliminar: hay %d producto(s) vinculados", linked)
	}
	return s.db.Delete(&b).Error
}

// ── Unidades de venta (TenantProductSaleUnit) ───────────────────────────────────────────────────
//
// Fase 1: solo administración de la entidad. Todavía no la lee ni la usa ningún flujo de
// ventas/compras/inventario — ver comentario en pkg/database/migrations.go.

// SaleUnitInput datos de entrada para crear/actualizar una unidad de venta.
type SaleUnitInput struct {
	Name             string
	// UnitID: unidad comercial SUNAT (Catálogo N°03) de esta SaleUnit — FK a TenantUnit, mismo
	// campo/patrón que ProductInput usa para la unidad base del producto. Requerido al crear
	// (requireUnit=true en validateSaleUnitInput); opcional al actualizar, para no forzar a
	// completar retroactivamente una SaleUnit existente que nació antes de este campo.
	UnitID           *uint
	ConversionFactor float64
	IsBase           bool
	AllowFraction    bool
	Price1           float64
	Price2           *float64
	Price3           *float64
	SortOrder        int
	Active           bool
}

// validateSaleUnitInput normaliza y valida los campos comunes a Create/Update, y resuelve el
// código SUNAT (unitCode) desde TenantUnit cuando se indica UnitID — nunca se acepta un código de
// unidad como texto libre del caller, igual que ProductService.resolveUnitReference exige que el
// catálogo (TenantUnit) sea la fuente de verdad cuando se referencia por ID. No valida ProductID ni
// pertenencia al tenant: eso lo resuelve el caller con s.db (ya scopeado al tenant).
//
// requireUnit=true (CreateSaleUnit): una SaleUnit nueva debe declarar su unidad comercial SUNAT
// desde que nace. requireUnit=false (UpdateSaleUnit): se permite guardar sin UnitID para no romper
// SaleUnits creadas antes de que este campo existiera — quedan con unitCode="" (el backend usa la
// unidad base del producto como resguardo al vender, ver resolveSaleItemUnitCode).
func validateSaleUnitInput(db *gorm.DB, in SaleUnitInput, requireUnit bool) (name, unitCode string, err error) {
	name = strings.TrimSpace(in.Name)
	if name == "" {
		return "", "", errors.New("nombre de la unidad de venta requerido")
	}
	if in.ConversionFactor <= 0 {
		return "", "", errors.New("el factor de conversión debe ser mayor a cero")
	}
	if in.IsBase && in.ConversionFactor != 1 {
		return "", "", errors.New("la unidad base debe tener factor de conversión igual a 1")
	}
	if in.Price1 <= 0 {
		return "", "", errors.New("price1 debe ser mayor a cero")
	}
	if in.Price2 != nil && *in.Price2 <= 0 {
		return "", "", errors.New("price2 debe ser mayor a cero si se especifica")
	}
	if in.Price3 != nil && *in.Price3 <= 0 {
		return "", "", errors.New("price3 debe ser mayor a cero si se especifica")
	}
	if in.UnitID == nil || *in.UnitID == 0 {
		if requireUnit {
			return "", "", errors.New("la unidad de venta requiere una unidad de medida (Catálogo SUNAT N°03)")
		}
		return name, "", nil
	}
	var u database.TenantUnit
	if err := db.First(&u, *in.UnitID).Error; err != nil {
		return "", "", errors.New("unidad de medida no encontrada")
	}
	return name, u.Code, nil
}

// clearOtherBaseSaleUnitsTx desmarca is_base en las demás unidades de venta del mismo producto,
// para que a lo sumo una quede marcada. Mismo criterio que clearOtherDefaultSeriesTx en
// internal/company/service/company_service.go: se aplica en Create/UpdateSaleUnit dentro de la
// misma transacción, no con un índice único (MySQL no soporta índices únicos parciales sin
// columnas generadas).
func clearOtherBaseSaleUnitsTx(tx *gorm.DB, productID uint, excludeID uint) error {
	q := tx.Model(&database.TenantProductSaleUnit{}).
		Where("product_id = ? AND is_base = ?", productID, true)
	if excludeID > 0 {
		q = q.Where("id != ?", excludeID)
	}
	return q.Update("is_base", false).Error
}

// ListSaleUnits unidades de venta activas de un producto, en orden de exhibición.
func (s *ProductService) ListSaleUnits(productID uint) ([]database.TenantProductSaleUnit, error) {
	var units []database.TenantProductSaleUnit
	err := s.db.Where("product_id = ? AND active = ?", productID, true).
		Order("sort_order ASC, id ASC").Find(&units).Error
	return units, err
}

// ListAllSaleUnits incluye inactivas — para la pantalla de administración.
func (s *ProductService) ListAllSaleUnits(productID uint) ([]database.TenantProductSaleUnit, error) {
	var units []database.TenantProductSaleUnit
	err := s.db.Where("product_id = ?", productID).
		Order("sort_order ASC, id ASC").Find(&units).Error
	return units, err
}

// GetSaleUnit obtiene una unidad de venta verificando que pertenezca al producto indicado (además
// de la aislación de tenant que ya da s.db, scopeado a la base de datos del tenant que llama).
func (s *ProductService) GetSaleUnit(productID, id uint) (*database.TenantProductSaleUnit, error) {
	var u database.TenantProductSaleUnit
	if err := s.db.Where("id = ? AND product_id = ?", id, productID).First(&u).Error; err != nil {
		return nil, errors.New("unidad de venta no encontrada")
	}
	return &u, nil
}

// CreateSaleUnit crea una unidad de venta para un producto existente del tenant.
func (s *ProductService) CreateSaleUnit(productID uint, in SaleUnitInput) (*database.TenantProductSaleUnit, error) {
	var product database.TenantProduct
	if err := s.db.First(&product, productID).Error; err != nil {
		return nil, errors.New("producto no encontrado")
	}
	name, unitCode, err := validateSaleUnitInput(s.db, in, true)
	if err != nil {
		return nil, err
	}
	row := &database.TenantProductSaleUnit{
		ProductID:        productID,
		Name:             name,
		UnitID:           in.UnitID,
		Unit:             unitCode,
		ConversionFactor: in.ConversionFactor,
		IsBase:           in.IsBase,
		AllowFraction:    in.AllowFraction,
		Price1:           in.Price1,
		Price2:           in.Price2,
		Price3:           in.Price3,
		SortOrder:        in.SortOrder,
		Active:           true,
	}
	if !in.IsBase {
		return row, s.db.Create(row).Error
	}
	err = s.db.Transaction(func(tx *gorm.DB) error {
		if err := clearOtherBaseSaleUnitsTx(tx, productID, 0); err != nil {
			return err
		}
		return tx.Create(row).Error
	})
	return row, err
}

// UpdateSaleUnit actualiza nombre, factor, fracción, precios y estado activo. Verifica pertenencia
// al producto indicado antes de modificar.
func (s *ProductService) UpdateSaleUnit(productID, id uint, in SaleUnitInput) (*database.TenantProductSaleUnit, error) {
	var row database.TenantProductSaleUnit
	if err := s.db.Where("id = ? AND product_id = ?", id, productID).First(&row).Error; err != nil {
		return nil, errors.New("unidad de venta no encontrada")
	}
	name, unitCode, err := validateSaleUnitInput(s.db, in, false)
	if err != nil {
		return nil, err
	}
	row.Name = name
	row.UnitID = in.UnitID
	row.Unit = unitCode
	row.ConversionFactor = in.ConversionFactor
	row.AllowFraction = in.AllowFraction
	row.Price1 = in.Price1
	row.Price2 = in.Price2
	row.Price3 = in.Price3
	row.SortOrder = in.SortOrder
	row.Active = in.Active
	if !in.IsBase {
		row.IsBase = false
		return &row, s.db.Save(&row).Error
	}
	row.IsBase = true
	err = s.db.Transaction(func(tx *gorm.DB) error {
		if err := clearOtherBaseSaleUnitsTx(tx, productID, row.ID); err != nil {
			return err
		}
		return tx.Save(&row).Error
	})
	return &row, err
}

// DeleteSaleUnit borra (soft delete) una unidad de venta. En esta fase nada la referencia todavía
// desde ventas/compras/Kardex, así que no hace falta un guard de "vinculados" como en categorías/
// marcas/unidades — se agregará cuando exista esa integración.
func (s *ProductService) DeleteSaleUnit(productID, id uint) error {
	var row database.TenantProductSaleUnit
	if err := s.db.Where("id = ? AND product_id = ?", id, productID).First(&row).Error; err != nil {
		return errors.New("unidad de venta no encontrada")
	}
	return s.db.Delete(&row).Error
}

// ── Precios por sucursal de unidades de venta (TenantProductSaleUnitBranchPrice) ────────────────
//
// Fase 4: override de precio de una SaleUnit para una sucursal puntual. La SaleUnit sigue siendo
// global — esto no le agrega branch_id. Ver pkg/saleunit.ResolvePrice para el algoritmo de
// resolución (override de sucursal activo → precio global de la SaleUnit → Price1).

// SaleUnitBranchPriceInput datos de entrada para crear/actualizar un override de sucursal.
type SaleUnitBranchPriceInput struct {
	Price1 float64
	Price2 *float64
	Price3 *float64
	Active bool
}

// getSaleUnitOwnedByProduct confirma que la SaleUnit existe y pertenece al producto indicado —
// reutilizado por todo el CRUD de precios por sucursal, mismo criterio que GetSaleUnit.
func (s *ProductService) getSaleUnitOwnedByProduct(productID, saleUnitID uint) (*database.TenantProductSaleUnit, error) {
	var su database.TenantProductSaleUnit
	if err := s.db.Where("id = ? AND product_id = ?", saleUnitID, productID).First(&su).Error; err != nil {
		return nil, errors.New("unidad de venta no encontrada")
	}
	return &su, nil
}

func (s *ProductService) getTenantBranch(branchID uint) (*database.TenantBranch, error) {
	var b database.TenantBranch
	if err := s.db.First(&b, branchID).Error; err != nil {
		return nil, errors.New("sucursal no encontrada")
	}
	return &b, nil
}

// ListSaleUnitBranchPrices lista los overrides de sucursal configurados para una unidad de venta.
func (s *ProductService) ListSaleUnitBranchPrices(productID, saleUnitID uint) ([]database.TenantProductSaleUnitBranchPrice, error) {
	if _, err := s.getSaleUnitOwnedByProduct(productID, saleUnitID); err != nil {
		return nil, err
	}
	var rows []database.TenantProductSaleUnitBranchPrice
	err := s.db.Where("sale_unit_id = ?", saleUnitID).Order("branch_id ASC").Find(&rows).Error
	return rows, err
}

// GetSaleUnitBranchPrice obtiene el override de una sucursal puntual.
func (s *ProductService) GetSaleUnitBranchPrice(productID, saleUnitID, branchID uint) (*database.TenantProductSaleUnitBranchPrice, error) {
	if _, err := s.getSaleUnitOwnedByProduct(productID, saleUnitID); err != nil {
		return nil, err
	}
	var row database.TenantProductSaleUnitBranchPrice
	if err := s.db.Where("sale_unit_id = ? AND branch_id = ?", saleUnitID, branchID).First(&row).Error; err != nil {
		return nil, errors.New("precio de sucursal no encontrado")
	}
	return &row, nil
}

// CreateSaleUnitBranchPrice crea el override de precio de una SaleUnit para una sucursal. Rechaza
// si ya existe una fila para ese par — se edita con UpdateSaleUnitBranchPrice en vez de duplicar
// (ver comentario del modelo sobre por qué esta tabla no usa soft delete y sí un índice único
// real). El índice único de BD es el resguardo final contra una creación duplicada por carrera
// entre dos requests concurrentes; este chequeo previo solo da un mensaje legible en el caso común.
func (s *ProductService) CreateSaleUnitBranchPrice(productID, saleUnitID, branchID uint, in SaleUnitBranchPriceInput) (*database.TenantProductSaleUnitBranchPrice, error) {
	if _, err := s.getSaleUnitOwnedByProduct(productID, saleUnitID); err != nil {
		return nil, err
	}
	if _, err := s.getTenantBranch(branchID); err != nil {
		return nil, err
	}
	if err := saleunit.ValidateBranchPriceInput(in.Price1, in.Price2, in.Price3); err != nil {
		return nil, err
	}
	var existing database.TenantProductSaleUnitBranchPrice
	err := s.db.Where("sale_unit_id = ? AND branch_id = ?", saleUnitID, branchID).First(&existing).Error
	if err == nil {
		return nil, errors.New("ya existe un precio configurado para esta sucursal; edítelo o elimínelo primero")
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	row := &database.TenantProductSaleUnitBranchPrice{
		SaleUnitID: saleUnitID, BranchID: branchID,
		Price1: in.Price1, Price2: in.Price2, Price3: in.Price3, Active: true,
	}
	if err := s.db.Create(row).Error; err != nil {
		if isDuplicateBranchPriceError(err) {
			// Carrera entre dos requests concurrentes creando el mismo (sale_unit_id, branch_id):
			// el chequeo previo (líneas arriba) no lo detectó porque ambos leyeron antes de que
			// cualquiera insertara. El índice único de la tabla (idx_sale_unit_branch_price) sí
			// lo impide a nivel de BD — acá solo se traduce a un mensaje de negocio legible en
			// vez del error crudo del driver, mismo criterio que isDuplicateOpenSessionError en
			// internal/restaurant/service/table_session_sync.go.
			return nil, errors.New("ya existe un precio configurado para esta sucursal; edítelo o elimínelo primero")
		}
		return nil, err
	}
	return row, nil
}

// isDuplicateBranchPriceError detecta la violación del índice único
// idx_sale_unit_branch_price (MySQL 1062) — mismo patrón que isDuplicateOpenSessionError en
// internal/restaurant/service/table_session_sync.go, sin introducir infraestructura nueva.
func isDuplicateBranchPriceError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "duplicate") ||
		strings.Contains(msg, "1062") ||
		strings.Contains(msg, "unique") ||
		strings.Contains(msg, "idx_sale_unit_branch_price")
}

// UpdateSaleUnitBranchPrice actualiza precios y estado activo de un override existente.
func (s *ProductService) UpdateSaleUnitBranchPrice(productID, saleUnitID, branchID uint, in SaleUnitBranchPriceInput) (*database.TenantProductSaleUnitBranchPrice, error) {
	if _, err := s.getSaleUnitOwnedByProduct(productID, saleUnitID); err != nil {
		return nil, err
	}
	var row database.TenantProductSaleUnitBranchPrice
	if err := s.db.Where("sale_unit_id = ? AND branch_id = ?", saleUnitID, branchID).First(&row).Error; err != nil {
		return nil, errors.New("precio de sucursal no encontrado")
	}
	if err := saleunit.ValidateBranchPriceInput(in.Price1, in.Price2, in.Price3); err != nil {
		return nil, err
	}
	row.Price1 = in.Price1
	row.Price2 = in.Price2
	row.Price3 = in.Price3
	row.Active = in.Active
	if err := s.db.Save(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

// DeleteSaleUnitBranchPrice elimina físicamente el override (esta tabla no tiene soft delete —
// ver comentario del modelo), liberando el par (sale_unit_id, branch_id) para poder configurarse
// de nuevo más adelante.
func (s *ProductService) DeleteSaleUnitBranchPrice(productID, saleUnitID, branchID uint) error {
	if _, err := s.getSaleUnitOwnedByProduct(productID, saleUnitID); err != nil {
		return err
	}
	res := s.db.Where("sale_unit_id = ? AND branch_id = ?", saleUnitID, branchID).
		Delete(&database.TenantProductSaleUnitBranchPrice{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return errors.New("precio de sucursal no encontrado")
	}
	return nil
}

// ── Atributos descriptivos de producto (TenantProductAttribute) ─────────────────────────────────
//
// Fase 5: puramente descriptivo (ej. "Color: Rojo"). No tiene stock, no genera Kardex, no afecta
// precios/SaleUnit/Presentation, no crea variantes. Ver comentario del modelo en
// pkg/database/migrations.go.

const (
	productAttributeNameMaxLen  = 100
	productAttributeValueMaxLen = 255
)

// ProductAttributeInput datos de entrada para crear/actualizar un atributo.
type ProductAttributeInput struct {
	Name      string
	Value     string
	SortOrder int
	Active    bool
}

// validateProductAttributeInput normaliza (trim) y valida nombre/valor. No valida ProductID ni
// duplicados: eso lo resuelve el caller, que ya tiene el producto cargado.
func validateProductAttributeInput(in ProductAttributeInput) (name, value string, err error) {
	name = strings.TrimSpace(in.Name)
	value = strings.TrimSpace(in.Value)
	if name == "" {
		return "", "", errors.New("el nombre del atributo es requerido")
	}
	if value == "" {
		return "", "", errors.New("el valor del atributo es requerido")
	}
	if len(name) > productAttributeNameMaxLen {
		return "", "", fmt.Errorf("el nombre del atributo no puede superar %d caracteres", productAttributeNameMaxLen)
	}
	if len(value) > productAttributeValueMaxLen {
		return "", "", fmt.Errorf("el valor del atributo no puede superar %d caracteres", productAttributeValueMaxLen)
	}
	return name, value, nil
}

// ListProductAttributes atributos activos de un producto, en orden de exhibición.
func (s *ProductService) ListProductAttributes(productID uint) ([]database.TenantProductAttribute, error) {
	var rows []database.TenantProductAttribute
	err := s.db.Where("product_id = ? AND active = ?", productID, true).
		Order("sort_order ASC, id ASC").Find(&rows).Error
	return rows, err
}

// ListAllProductAttributes incluye inactivos — para la pantalla de administración.
func (s *ProductService) ListAllProductAttributes(productID uint) ([]database.TenantProductAttribute, error) {
	var rows []database.TenantProductAttribute
	err := s.db.Where("product_id = ?", productID).
		Order("sort_order ASC, id ASC").Find(&rows).Error
	return rows, err
}

// GetProductAttribute obtiene un atributo verificando que pertenezca al producto indicado.
func (s *ProductService) GetProductAttribute(productID, id uint) (*database.TenantProductAttribute, error) {
	var row database.TenantProductAttribute
	if err := s.db.Where("id = ? AND product_id = ?", id, productID).First(&row).Error; err != nil {
		return nil, errors.New("atributo no encontrado")
	}
	return &row, nil
}

// productAttributeDuplicateExists busca un atributo con el mismo nombre+valor (insensible a
// mayúsculas/espacios) para el mismo producto, excluyendo opcionalmente una fila (para Update).
func (s *ProductService) productAttributeDuplicateExists(productID uint, name, value string, excludeID uint) (bool, error) {
	q := s.db.Model(&database.TenantProductAttribute{}).
		Where("product_id = ? AND LOWER(name) = LOWER(?) AND LOWER(value) = LOWER(?)", productID, name, value)
	if excludeID > 0 {
		q = q.Where("id != ?", excludeID)
	}
	var count int64
	if err := q.Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

// CreateProductAttribute crea un atributo descriptivo para un producto existente del tenant.
// Rechaza duplicados exactos (mismo nombre+valor) — "Color=Rojo" dos veces no aporta nada nuevo;
// "Color=Rojo" y "Color=Azul" sí se permiten (no hay evidencia de negocio para prohibirlo, y esta
// fase no decide semántica de variantes).
func (s *ProductService) CreateProductAttribute(productID uint, in ProductAttributeInput) (*database.TenantProductAttribute, error) {
	var product database.TenantProduct
	if err := s.db.First(&product, productID).Error; err != nil {
		return nil, errors.New("producto no encontrado")
	}
	name, value, err := validateProductAttributeInput(in)
	if err != nil {
		return nil, err
	}
	dup, err := s.productAttributeDuplicateExists(productID, name, value, 0)
	if err != nil {
		return nil, err
	}
	if dup {
		return nil, fmt.Errorf("'%s = %s' ya existe para este producto", name, value)
	}
	row := &database.TenantProductAttribute{
		ProductID: productID, Name: name, Value: value, SortOrder: in.SortOrder, Active: true,
	}
	if err := s.db.Create(row).Error; err != nil {
		return nil, err
	}
	return row, nil
}

// UpdateProductAttribute actualiza nombre, valor, orden y estado activo de un atributo existente.
func (s *ProductService) UpdateProductAttribute(productID, id uint, in ProductAttributeInput) (*database.TenantProductAttribute, error) {
	var row database.TenantProductAttribute
	if err := s.db.Where("id = ? AND product_id = ?", id, productID).First(&row).Error; err != nil {
		return nil, errors.New("atributo no encontrado")
	}
	name, value, err := validateProductAttributeInput(in)
	if err != nil {
		return nil, err
	}
	dup, err := s.productAttributeDuplicateExists(productID, name, value, row.ID)
	if err != nil {
		return nil, err
	}
	if dup {
		return nil, fmt.Errorf("'%s = %s' ya existe para este producto", name, value)
	}
	row.Name = name
	row.Value = value
	row.SortOrder = in.SortOrder
	row.Active = in.Active
	if err := s.db.Save(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

// DeleteProductAttribute elimina físicamente el atributo (sin soft delete — ver comentario del
// modelo): nada más en el sistema referencia su ID, así que no hace falta preservarlo.
func (s *ProductService) DeleteProductAttribute(productID, id uint) error {
	res := s.db.Where("id = ? AND product_id = ?", id, productID).
		Delete(&database.TenantProductAttribute{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return errors.New("atributo no encontrado")
	}
	return nil
}

// ── Unidades de medida (catálogo SUNAT N°03, gestionable desde Tukifac) ─────────────────────────

func (s *ProductService) ListUnits() ([]database.TenantUnit, error) {
	var units []database.TenantUnit
	err := s.db.Where("active = ?", true).Order("sort_order ASC, name ASC").Find(&units).Error
	return units, err
}

// ListAllUnits incluye inactivas — usado por la pantalla de gestión en Tukifac.
func (s *ProductService) ListAllUnits() ([]database.TenantUnit, error) {
	var units []database.TenantUnit
	err := s.db.Order("sort_order ASC, name ASC").Find(&units).Error
	return units, err
}

func (s *ProductService) GetUnit(id uint) (*database.TenantUnit, error) {
	var u database.TenantUnit
	if err := s.db.First(&u, id).Error; err != nil {
		return nil, err
	}
	return &u, nil
}

// CreateUnit agrega una unidad propia del tenant — libre, no restringida al catálogo SUNAT 03
// (si el código no es válido para SUNAT, NormalizeUnit la reconducirá a NIU solo al facturar,
// nunca al guardar el producto: la unidad elegida siempre se ve tal cual en el ERP).
func (s *ProductService) CreateUnit(code, name, symbol string) (*database.TenantUnit, error) {
	code = strings.ToUpper(strings.TrimSpace(code))
	name = strings.TrimSpace(name)
	if code == "" || name == "" {
		return nil, errors.New("código y nombre de la unidad son requeridos")
	}
	var existing database.TenantUnit
	err := s.db.Where("code = ?", code).First(&existing).Error
	if err == nil {
		return nil, fmt.Errorf("ya existe una unidad con código '%s'", code)
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	next, err := s.nextUnitSortOrder()
	if err != nil {
		return nil, err
	}
	u := &database.TenantUnit{Code: code, Name: name, Symbol: strings.TrimSpace(symbol), SortOrder: next, Active: true}
	return u, s.db.Create(u).Error
}

// UpdateUnit no permite cambiar el código de una fila del catálogo del sistema (IsSystem=true) —
// mismo candado que TenantPaymentMethod, para no desincronizar lo que ya factura como ese código.
func (s *ProductService) UpdateUnit(id uint, code, name, symbol string, active bool) (*database.TenantUnit, error) {
	var u database.TenantUnit
	if err := s.db.First(&u, id).Error; err != nil {
		return nil, errors.New("unidad no encontrada")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("nombre de la unidad requerido")
	}
	code = strings.ToUpper(strings.TrimSpace(code))
	if code != "" && code != u.Code {
		if u.IsSystem {
			return nil, errors.New("no se puede cambiar el código de una unidad del sistema")
		}
		var dup database.TenantUnit
		if err := s.db.Where("code = ? AND id <> ?", code, id).First(&dup).Error; err == nil {
			return nil, fmt.Errorf("ya existe una unidad con código '%s'", code)
		}
		u.Code = code
	}
	u.Name = name
	u.Symbol = strings.TrimSpace(symbol)
	u.Active = active
	if err := s.db.Save(&u).Error; err != nil {
		return nil, err
	}
	return &u, nil
}

// DeleteUnit solo permite borrar unidades propias del tenant (no del sistema) sin productos
// vinculados — igual criterio que categorías/marcas. Una del sistema en desuso se desactiva, no se
// elimina (evita romper ventas/facturas históricas que aún la referencian por código).
func (s *ProductService) DeleteUnit(id uint) error {
	var u database.TenantUnit
	if err := s.db.First(&u, id).Error; err != nil {
		return errors.New("unidad no encontrada")
	}
	if u.IsSystem {
		return errors.New("las unidades del sistema no se eliminan; desactívala en su lugar")
	}
	var linked int64
	if err := s.db.Model(&database.TenantProduct{}).Where("unit_id = ?", id).Count(&linked).Error; err != nil {
		return err
	}
	if linked > 0 {
		return fmt.Errorf("no se puede eliminar: hay %d producto(s) vinculados", linked)
	}
	return s.db.Delete(&u).Error
}

func (s *ProductService) nextUnitSortOrder() (int, error) {
	var max int
	if err := s.db.Model(&database.TenantUnit{}).Select("COALESCE(MAX(sort_order), 0)").Scan(&max).Error; err != nil {
		return 0, err
	}
	return max + 1, nil
}

func (s *ProductService) resolvePreparationAreaFields(p *database.TenantProduct) error {
	if !p.IsRestaurant {
		p.PreparationAreaID = nil
		p.PreparationArea = ""
		return nil
	}
	if p.PreparationAreaID != nil && *p.PreparationAreaID > 0 {
		var area database.TenantPreparationArea
		if err := s.db.First(&area, *p.PreparationAreaID).Error; err != nil {
			return errors.New("área de preparación no encontrada")
		}
		p.PreparationArea = area.Slug
		return nil
	}
	p.PreparationArea = strings.TrimSpace(strings.ToLower(p.PreparationArea))
	if p.PreparationArea == "" {
		p.PreparationAreaID = nil
		return nil
	}
	var area database.TenantPreparationArea
	if err := s.db.Where("slug = ?", p.PreparationArea).First(&area).Error; err != nil {
		return nil
	}
	id := area.ID
	p.PreparationAreaID = &id
	return nil
}

func slugifyPreparationAreaName(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	var b strings.Builder
	lastSep := false
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastSep = false
			continue
		}
		if !lastSep {
			b.WriteRune('_')
			lastSep = true
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		return "area"
	}
	return out
}

func (s *ProductService) uniquePreparationAreaSlug(base string) (string, error) {
	slug := base
	for i := 0; i < 100; i++ {
		var n int64
		if err := s.db.Model(&database.TenantPreparationArea{}).Where("slug = ?", slug).Count(&n).Error; err != nil {
			return "", err
		}
		if n == 0 {
			return slug, nil
		}
		slug = fmt.Sprintf("%s_%d", base, i+2)
	}
	return "", errors.New("no se pudo generar slug único")
}

func (s *ProductService) ResolvePreparationAreaIDBySlug(slug string) (*uint, error) {
	slug = strings.TrimSpace(strings.ToLower(slug))
	if slug == "" {
		return nil, nil
	}
	var area database.TenantPreparationArea
	if err := s.db.Where("slug = ?", slug).First(&area).Error; err != nil {
		return nil, fmt.Errorf("área de preparación %q no encontrada", slug)
	}
	id := area.ID
	return &id, nil
}

// ========= Áreas de preparación =========

type PreparationAreaListItem struct {
	database.TenantPreparationArea
	ProductCount int64 `json:"product_count"`
}

func (s *ProductService) nextPreparationAreaSortOrder() (int, error) {
	var maxOrder *int
	err := s.db.Model(&database.TenantPreparationArea{}).Select("MAX(sort_order)").Scan(&maxOrder).Error
	if err != nil {
		return 0, err
	}
	if maxOrder == nil {
		return 1, nil
	}
	return *maxOrder + 1, nil
}

func (s *ProductService) ListPreparationAreas() ([]database.TenantPreparationArea, error) {
	var areas []database.TenantPreparationArea
	err := s.db.Where("active = ?", true).Order("sort_order ASC, name ASC").Find(&areas).Error
	return areas, err
}

func (s *ProductService) ListPreparationAreasWithCounts() ([]PreparationAreaListItem, error) {
	var areas []database.TenantPreparationArea
	if err := s.db.Order("sort_order ASC, name ASC").Find(&areas).Error; err != nil {
		return nil, err
	}
	if len(areas) == 0 {
		return nil, nil
	}
	ids := make([]uint, len(areas))
	for i, a := range areas {
		ids[i] = a.ID
	}
	type countRow struct {
		PreparationAreaID uint
		Count             int64
	}
	var counts []countRow
	if err := s.db.Model(&database.TenantProduct{}).
		Select("preparation_area_id, COUNT(*) AS count").
		Where("preparation_area_id IN ?", ids).
		Group("preparation_area_id").
		Scan(&counts).Error; err != nil {
		return nil, err
	}
	countMap := make(map[uint]int64, len(counts))
	for _, r := range counts {
		countMap[r.PreparationAreaID] = r.Count
	}
	out := make([]PreparationAreaListItem, len(areas))
	for i, a := range areas {
		out[i] = PreparationAreaListItem{TenantPreparationArea: a, ProductCount: countMap[a.ID]}
	}
	return out, nil
}

func (s *ProductService) CreatePreparationArea(name, slug string, sortOrder *int) (*database.TenantPreparationArea, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("nombre de área requerido")
	}
	baseSlug := strings.TrimSpace(strings.ToLower(slug))
	if baseSlug == "" {
		baseSlug = slugifyPreparationAreaName(name)
	}
	uniqueSlug, err := s.uniquePreparationAreaSlug(baseSlug)
	if err != nil {
		return nil, err
	}
	order := 0
	if sortOrder != nil {
		order = *sortOrder
	} else {
		next, err := s.nextPreparationAreaSortOrder()
		if err != nil {
			return nil, err
		}
		order = next
	}
	area := &database.TenantPreparationArea{Name: name, Slug: uniqueSlug, SortOrder: order, Active: true}
	if err := s.db.Create(area).Error; err != nil {
		return nil, err
	}
	return area, nil
}

func (s *ProductService) UpdatePreparationArea(id uint, name string, sortOrder int) (*database.TenantPreparationArea, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("nombre de área requerido")
	}
	var area database.TenantPreparationArea
	if err := s.db.First(&area, id).Error; err != nil {
		return nil, errors.New("área de preparación no encontrada")
	}
	area.Name = name
	area.SortOrder = sortOrder
	if err := s.db.Save(&area).Error; err != nil {
		return nil, err
	}
	return &area, nil
}

func (s *ProductService) DeletePreparationArea(id uint) error {
	var area database.TenantPreparationArea
	if err := s.db.First(&area, id).Error; err != nil {
		return errors.New("área de preparación no encontrada")
	}
	var linked int64
	if err := s.db.Model(&database.TenantProduct{}).Where("preparation_area_id = ?", id).Count(&linked).Error; err != nil {
		return err
	}
	if linked > 0 {
		return fmt.Errorf("no se puede eliminar: hay %d producto(s) vinculados", linked)
	}
	return s.db.Delete(&area).Error
}

// ========= Grupos de modificadores =========

type ModifierGroupWithOptions struct {
	database.TenantModifierGroup
	Options []database.TenantModifierOption `json:"options"`
}

func (s *ProductService) ListModifierGroups() ([]ModifierGroupWithOptions, error) {
	var groups []database.TenantModifierGroup
	if err := s.db.Where("active = ?", true).Order("name ASC").Find(&groups).Error; err != nil {
		return nil, err
	}
	result := make([]ModifierGroupWithOptions, 0, len(groups))
	for _, g := range groups {
		if !modifierkind.IsExtra(g.Kind, g.Required, g.MultiSelect) {
			continue
		}
		var opts []database.TenantModifierOption
		s.db.Where("group_id = ? AND active = ?", g.ID, true).Order("name ASC").Find(&opts)
		result = append(result, ModifierGroupWithOptions{TenantModifierGroup: g, Options: opts})
	}
	return result, nil
}

func (s *ProductService) GetProductModifierGroupIDs(productID uint) []uint {
	var links []database.TenantProductModifierGroup
	s.db.Where("product_id = ?", productID).Find(&links)
	ids := make([]uint, 0, len(links))
	for _, l := range links {
		ids = append(ids, l.GroupID)
	}
	return ids
}

// ModifierOptionInput opción de un grupo con precio adicional (variante o extra).
type ModifierOptionInput struct {
	Name       string
	ExtraPrice float64
}

func (s *ProductService) CreateModifierGroup(name, kind string, required, multiSelect bool, options []ModifierOptionInput) (*ModifierGroupWithOptions, error) {
	if name == "" {
		return nil, errors.New("nombre del grupo requerido")
	}
	_ = kind
	g := &database.TenantModifierGroup{Name: name, Kind: modifierkind.Extra, Required: required, MultiSelect: multiSelect, Active: true}
	if err := s.db.Create(g).Error; err != nil {
		return nil, err
	}
	opts := s.createModifierOptions(g.ID, options)
	return &ModifierGroupWithOptions{TenantModifierGroup: *g, Options: opts}, nil
}

func (s *ProductService) UpdateModifierGroup(id uint, name, kind string, required, multiSelect bool, options []ModifierOptionInput) (*ModifierGroupWithOptions, error) {
	if name == "" {
		return nil, errors.New("nombre del grupo requerido")
	}
	var g database.TenantModifierGroup
	if err := s.db.First(&g, id).Error; err != nil {
		return nil, errors.New("grupo no encontrado")
	}
	_ = kind
	if err := s.db.Model(&g).Updates(map[string]interface{}{
		"name":         name,
		"kind":         modifierkind.Extra,
		"required":     required,
		"multi_select": multiSelect,
	}).Error; err != nil {
		return nil, err
	}
	if err := s.db.Where("group_id = ?", id).Delete(&database.TenantModifierOption{}).Error; err != nil {
		return nil, err
	}
	opts := s.createModifierOptions(id, options)
	g.Name = name
	g.Kind = modifierkind.Extra
	g.Required = required
	g.MultiSelect = multiSelect
	return &ModifierGroupWithOptions{TenantModifierGroup: g, Options: opts}, nil
}

// DeleteModifierGroup elimina un grupo, sus opciones y vínculos con productos.
// Los pedidos históricos conservan snapshot en modifiers_json.
func (s *ProductService) DeleteModifierGroup(id uint) error {
	var g database.TenantModifierGroup
	if err := s.db.First(&g, id).Error; err != nil {
		return errors.New("grupo no encontrado")
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("group_id = ?", id).Delete(&database.TenantModifierOption{}).Error; err != nil {
			return err
		}
		if err := tx.Where("group_id = ?", id).Delete(&database.TenantProductModifierGroup{}).Error; err != nil {
			return err
		}
		return tx.Delete(&g).Error
	})
}

func (s *ProductService) createModifierOptions(groupID uint, options []ModifierOptionInput) []database.TenantModifierOption {
	opts := make([]database.TenantModifierOption, 0, len(options))
	for _, o := range options {
		optName := strings.TrimSpace(o.Name)
		if optName == "" {
			continue
		}
		price := o.ExtraPrice
		if price < 0 {
			price = 0
		}
		opt := database.TenantModifierOption{
			GroupID:    groupID,
			Name:       optName,
			ExtraPrice: money.RoundDisplay(price),
			Active:     true,
		}
		if err := s.db.Create(&opt).Error; err == nil {
			opts = append(opts, opt)
		}
	}
	return opts
}

// ========= Series =========

func (s *ProductService) AddSerial(productID, branchID uint, serial string, purchaseItemID *uint) (*database.TenantProductSerial, error) {
	if serial == "" {
		return nil, errors.New("número de serie requerido")
	}
	var existing database.TenantProductSerial
	if err := s.db.Where("product_id = ? AND serial = ?", productID, serial).First(&existing).Error; err == nil {
		return nil, fmt.Errorf("el número de serie '%s' ya existe para este producto", serial)
	}
	ps := &database.TenantProductSerial{
		ProductID:      productID,
		BranchID:       branchID,
		Serial:         serial,
		Status:         "available",
		PurchaseItemID: purchaseItemID,
	}
	err := s.db.Create(ps).Error
	return ps, err
}

func (s *ProductService) GetAvailableSerials(productID, branchID uint) ([]database.TenantProductSerial, error) {
	var serials []database.TenantProductSerial
	err := s.db.Where("product_id = ? AND branch_id = ? AND status = ?", productID, branchID, "available").
		Order("serial ASC").Find(&serials).Error
	return serials, err
}

// ListProductSerials returns all serials for a product (all branches), for display in product detail.
func (s *ProductService) ListProductSerials(productID uint) ([]database.TenantProductSerial, error) {
	var serials []database.TenantProductSerial
	err := s.db.Where("product_id = ?", productID).Order("branch_id ASC, serial ASC").Find(&serials).Error
	return serials, err
}

// ========= Bulk Actions =========

type BulkToggleCatalogInput struct {
	ProductIDs []uint
	UserID     uint
	BranchID   uint
}

type BulkUpdateCatalogInput struct {
	ProductIDs           []uint
	Active               *bool
	IsRestaurant         *bool
	ShowInDigitalCatalog *bool
	ManageStock          *bool
	UserID               uint
	BranchID             uint
}

type BulkActionResult struct {
	Success int `json:"success"`
	Updated int `json:"updated"`
}

// BulkToggleCatalog activa/desactiva múltiples productos
func (s *ProductService) BulkToggleCatalog(input BulkToggleCatalogInput) (*BulkActionResult, error) {
	if len(input.ProductIDs) == 0 {
		return nil, errors.New("se requiere al menos un producto")
	}

	res := s.db.Model(&database.TenantProduct{}).
		Where("id IN ?", input.ProductIDs).
		Update("active", gorm.Expr("NOT active"))

	return &BulkActionResult{
		Success: 1,
		Updated: int(res.RowsAffected),
	}, res.Error
}

// BulkUpdateCatalog actualiza múltiples productos con los campos especificados
func (s *ProductService) BulkUpdateCatalog(input BulkUpdateCatalogInput) (*BulkActionResult, error) {
	if len(input.ProductIDs) == 0 {
		return nil, errors.New("se requiere al menos un producto")
	}

	updates := make(map[string]interface{})
	if input.Active != nil {
		updates["active"] = *input.Active
	}
	if input.IsRestaurant != nil {
		updates["is_restaurant"] = *input.IsRestaurant
	}
	if input.ShowInDigitalCatalog != nil {
		updates["show_in_digital_catalog"] = *input.ShowInDigitalCatalog
	}
	if input.ManageStock != nil {
		updates["manage_stock"] = *input.ManageStock
	}

	if len(updates) == 0 {
		return nil, errors.New("se requiere al menos un campo para actualizar")
	}

	res := s.db.Model(&database.TenantProduct{}).
		Where("id IN ?", input.ProductIDs).
		Updates(updates)

	return &BulkActionResult{
		Success: 1,
		Updated: int(res.RowsAffected),
	}, res.Error
}
