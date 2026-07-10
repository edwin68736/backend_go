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
	Query            string
	CategoryID       uint
	Type             string
	ActiveOnly       bool
	InactiveOnly     bool // solo productos inactivos (panel restaurante)
	ManageStockOnly    bool // solo productos con manage_stock (para transferencias/inventario)
	NoManageStockOnly  bool // solo productos sin control de stock (reporte restaurante)
	RestaurantOnly       bool // solo productos con is_restaurant (para panel restaurante)
	PreparationArea      string // filtrar por slug (legacy)
	PreparationAreaID    uint   // filtrar por FK
	StockLessThan    *float64
	BranchID         uint // >0: restaurante → tenant_products.branch_id; inventario ERP → stock en sucursal
	Limit            int // 0 = sin límite (comportamiento anterior)
	Offset           int
	SortBy           string // id, code, name, category, price, stock
	SortDir          string // asc, desc
}

const maxReportSerialsPerProduct = 120

// BranchStockRow es una fila de stock por sucursal (reportes).
type BranchStockRow struct {
	BranchID   uint    `json:"branch_id"`
	BranchName string  `json:"branch_name"`
	Quantity   float64 `json:"quantity"`
}

// ProductListItem producto en listados API con nombre de categoría.
type ProductListItem struct {
	database.TenantProduct
	CategoryName string `json:"category_name,omitempty"`
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
	if params.PreparationAreaID > 0 {
		q = q.Where(p+"preparation_area_id = ?", params.PreparationAreaID)
	} else if params.PreparationArea != "" {
		q = q.Where(p+"preparation_area = ?", params.PreparationArea)
	}
	if params.BranchID > 0 {
		bid := params.BranchID
		if params.RestaurantOnly {
			// Carta Tukichef: catálogo exclusivo por sucursal (branch_id en el producto).
			q = q.Where(p+"branch_id = ?", bid)
		} else {
			q = q.Where(`(`+p+`manage_stock = ? OR EXISTS (
				SELECT 1 FROM tenant_product_stocks s WHERE s.product_id = tenant_products.id AND s.branch_id = ?
			))`, false, bid)
		}
	}
	if params.StockLessThan != nil {
		thr := *params.StockLessThan
		if params.BranchID > 0 {
			bid := params.BranchID
			q = q.Where(p+"manage_stock = ?", true).
				Where(`COALESCE((
					SELECT s.quantity FROM tenant_product_stocks s
					WHERE s.product_id = tenant_products.id AND s.branch_id = ?
					LIMIT 1
				), 0) < ?`, bid, thr)
		} else {
			q = q.Where(p+"manage_stock = ?", true).
				Where(`COALESCE((
					SELECT SUM(s.quantity) FROM tenant_product_stocks s WHERE s.product_id = tenant_products.id
				), 0) < ?`, thr)
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
	out := make([]ProductListItem, len(products))
	for i, p := range products {
		item := ProductListItem{TenantProduct: p}
		if p.CategoryID != nil {
			item.CategoryName = catName[*p.CategoryID]
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

// EnsureRestaurantBranchAccess valida que un plato pertenezca a la sucursal activa.
func (s *ProductService) EnsureRestaurantBranchAccess(p *database.TenantProduct, branchID uint) error {
	if p == nil || !p.IsRestaurant || branchID == 0 {
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
	CategoryID         *uint
	Code               string
	Name               string
	Description        string
	Type               string
	Unit               string
	SalePrice          float64
	PurchasePrice      float64
	TaxRate            float64
	IgvAffectationType string
	PriceIncludesIgv   bool
	ManageStock        bool
	ManageSeries       bool
	HasVariants        bool
	HasModifiers       bool
	IsRestaurant       bool
	PreparationAreaID  *uint
	PreparationArea    string // slug legacy; se sincroniza desde preparation_area_id
	MinStock           float64
	HasExpiryDate      bool
	ExpiryDate         *time.Time
	ImageURL           string
	Active             bool
	ActiveSet          bool // si true, Update actualiza el campo active
	BranchID           uint // sucursal dueña (platos restaurante)
	// nil = no tocar vínculos (update parcial); no-nil = reemplazar asignación (puede ser slice vacío).
	ModifierGroupIDs *[]uint
	// nil = no tocar presentaciones; no-nil = reemplazar lista del producto.
	Presentations *[]ProductPresentationInput
}

// ProductPresentationInput fila de presentación propia del producto (no es grupo global).
type ProductPresentationInput struct {
	Name      string
	SalePrice float64
	SortOrder int
}

func (s *ProductService) Create(input ProductInput) (*database.TenantProduct, error) {
	if input.Name == "" {
		return nil, errors.New("nombre es requerido")
	}

	if input.Code != "" {
		scopeBranch := input.IsRestaurant && input.BranchID > 0
		existing, err := s.findProductByCodeUnscoped(input.Code, input.BranchID, scopeBranch)
		if err != nil {
			return nil, err
		}
		if existing != nil {
			if isProductSoftDeleted(existing) {
				return s.reactivateProductFromInput(existing.ID, input)
			}
			return nil, fmt.Errorf("el código '%s' ya está en uso en esta sucursal", input.Code)
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
		return nil, err
	}

	p := &database.TenantProduct{
		CategoryID:         input.CategoryID,
		Code:               input.Code,
		Name:               input.Name,
		Description:        input.Description,
		Type:               input.Type,
		Unit:               input.Unit,
		SalePrice:          input.SalePrice,
		PurchasePrice:      input.PurchasePrice,
		TaxRate:            taxRate,
		IgvAffectationType: igvType,
		PriceIncludesIgv:   input.PriceIncludesIgv,
		ManageStock:        input.ManageStock,
		ManageSeries:       input.ManageSeries,
		HasVariants:        input.HasVariants,
		HasModifiers:       input.HasModifiers,
		IsRestaurant:       input.IsRestaurant,
		BranchID:           input.BranchID,
		PreparationAreaID:  input.PreparationAreaID,
		PreparationArea:    input.PreparationArea,
		MinStock:           input.MinStock,
		HasExpiryDate:      input.HasExpiryDate,
		ExpiryDate:         input.ExpiryDate,
		ImageURL:           input.ImageURL,
		Active:             input.Active,
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
		return nil, err
	}
	p.Unit = sunat.NormalizeUnit(p.Unit, p.Type)
	if strings.EqualFold(strings.TrimSpace(p.Type), "product") && strings.EqualFold(strings.TrimSpace(p.Unit), "ZZ") {
		return nil, errors.New("la unidad ZZ es solo para servicios: use Inventario → Servicios")
	}

	if err := s.db.Create(p).Error; err != nil {
		return nil, err
	}
	if err := gormutil.PersistBoolWithDefault(s.db, p, "price_includes_igv", input.PriceIncludesIgv); err != nil {
		return nil, err
	}
	p.PriceIncludesIgv = input.PriceIncludesIgv
	if err := gormutil.PersistBoolWithDefault(s.db, p, "manage_stock", input.ManageStock); err != nil {
		return nil, err
	}
	p.ManageStock = input.ManageStock

	if input.ModifierGroupIDs != nil {
		s.syncModifierGroups(p.ID, *input.ModifierGroupIDs)
	}
	if input.Presentations != nil {
		if err := s.syncPresentations(p.ID, *input.Presentations); err != nil {
			return nil, err
		}
	}

	return p, nil
}

// reactivateProductFromInput restaura un producto oculto (soft delete) y aplica los datos del alta.
func (s *ProductService) reactivateProductFromInput(id uint, input ProductInput) (*database.TenantProduct, error) {
	igvType := input.IgvAffectationType
	if igvType == "" {
		igvType = "10"
	}
	taxRate := input.TaxRate
	if !tax.IsGravado(igvType) {
		taxRate = 0
	}
	if err := validateProductExpiry(input.HasExpiryDate, input.ExpiryDate); err != nil {
		return nil, err
	}
	effType := strings.TrimSpace(input.Type)
	if effType == "" {
		effType = "product"
	}
	unit := sunat.NormalizeUnit(input.Unit, effType)
	if strings.EqualFold(effType, "product") && strings.EqualFold(unit, "ZZ") {
		return nil, errors.New("la unidad ZZ es solo para servicios: use Inventario → Servicios")
	}

	input.IgvAffectationType = igvType
	input.TaxRate = taxRate
	input.Type = effType
	input.Unit = unit
	input.Active = true
	input.ActiveSet = true

	if err := s.restoreSoftDeletedProduct(id); err != nil {
		return nil, err
	}
	if err := s.Update(id, input); err != nil {
		return nil, err
	}
	if err := gormutil.PersistBoolWithDefault(s.db, &database.TenantProduct{ID: id}, "price_includes_igv", input.PriceIncludesIgv); err != nil {
		return nil, err
	}
	if err := gormutil.PersistBoolWithDefault(s.db, &database.TenantProduct{ID: id}, "manage_stock", input.ManageStock); err != nil {
		return nil, err
	}
	if input.ModifierGroupIDs != nil {
		s.syncModifierGroups(id, *input.ModifierGroupIDs)
	}
	if input.Presentations != nil {
		if err := s.syncPresentations(id, *input.Presentations); err != nil {
			return nil, err
		}
	}
	return s.GetByID(id)
}

// normalizeProductServiceFields fuerza reglas SUNAT/ERP para filas type=service.
func normalizeProductServiceFields(p *database.TenantProduct) {
	if !strings.EqualFold(strings.TrimSpace(p.Type), "service") {
		return
	}
	p.Type = "service"
	p.Unit = "ZZ"
	p.ManageStock = false
	p.ManageSeries = false
	p.HasVariants = false
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

func (s *ProductService) Update(id uint, input ProductInput) error {
	var existing database.TenantProduct
	if err := s.db.First(&existing, id).Error; err != nil {
		return err
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

	unit := strings.TrimSpace(input.Unit)
	if unit == "" {
		unit = existing.Unit
	}
	unit = sunat.NormalizeUnit(unit, effType)
	if !strings.EqualFold(effType, "service") && strings.EqualFold(unit, "ZZ") {
		return errors.New("la unidad ZZ es solo para servicios: use Inventario → Servicios")
	}

	if err := validateProductExpiry(input.HasExpiryDate, input.ExpiryDate); err != nil {
		return err
	}

	draft := &database.TenantProduct{
		Type:              effType,
		Unit:              unit,
		IsRestaurant:      input.IsRestaurant,
		ManageStock:         input.ManageStock,
		PreparationAreaID:   input.PreparationAreaID,
		PreparationArea:     input.PreparationArea,
		MinStock:            input.MinStock,
		HasExpiryDate:       input.HasExpiryDate,
		ExpiryDate:          input.ExpiryDate,
		ManageSeries:        input.ManageSeries,
		HasVariants:         input.HasVariants,
		HasModifiers:        input.HasModifiers,
	}
	normalizeProductCatalogFields(draft)
	if err := s.resolvePreparationAreaFields(draft); err != nil {
		return err
	}
	if strings.EqualFold(draft.Type, "service") {
		unit = draft.Unit
	}

	upd := map[string]interface{}{
		"category_id":          input.CategoryID,
		"code":                 input.Code,
		"name":                 input.Name,
		"description":          input.Description,
		"type":                 draft.Type,
		"unit":                 unit,
		"sale_price":           input.SalePrice,
		"purchase_price":       input.PurchasePrice,
		"tax_rate":             taxRate,
		"igv_affectation_type": igvType,
		"price_includes_igv":   input.PriceIncludesIgv,
		"manage_stock":         draft.ManageStock,
		"manage_series":        draft.ManageSeries,
		"has_variants":         draft.HasVariants,
		"has_modifiers":        draft.HasModifiers,
		"is_restaurant":        draft.IsRestaurant,
		"preparation_area_id":  draft.PreparationAreaID,
		"preparation_area":     draft.PreparationArea,
		"min_stock":            draft.MinStock,
		"has_expiry_date":      draft.HasExpiryDate,
		"expiry_date":          draft.ExpiryDate,
		"image_url":            input.ImageURL,
	}
	if input.ActiveSet {
		upd["active"] = input.Active
	}
	err := s.db.Model(&database.TenantProduct{}).Where("id = ?", id).Updates(upd).Error
	if err != nil {
		return err
	}

	if input.ModifierGroupIDs != nil {
		modIDs := *input.ModifierGroupIDs
		if strings.EqualFold(effType, "service") {
			modIDs = nil
		}
		s.syncModifierGroups(id, modIDs)
	}
	if input.Presentations != nil {
		if err := s.syncPresentations(id, *input.Presentations); err != nil {
			return err
		}
	}
	return nil
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

func (s *ProductService) syncPresentations(productID uint, inputs []ProductPresentationInput) error {
	if err := s.db.Where("product_id = ?", productID).Delete(&database.TenantProductPresentation{}).Error; err != nil {
		return err
	}
	sortOrder := 0
	for _, in := range inputs {
		name := strings.TrimSpace(in.Name)
		if name == "" {
			continue
		}
		row := database.TenantProductPresentation{
			ProductID: productID,
			Name:      name,
			SalePrice: money.RoundDisplay(in.SalePrice),
			SortOrder: sortOrder,
			Active:    true,
		}
		if in.SortOrder > 0 {
			row.SortOrder = in.SortOrder
		}
		if err := s.db.Create(&row).Error; err != nil {
			return err
		}
		sortOrder++
	}
	hasVariants := sortOrder > 0
	return s.db.Model(&database.TenantProduct{}).Where("id = ?", productID).Update("has_variants", hasVariants).Error
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

func (s *ProductService) GetStock(productID uint) float64 {
	var total float64
	s.db.Model(&database.TenantProductStock{}).
		Where("product_id = ?", productID).
		Select("COALESCE(SUM(quantity), 0)").
		Scan(&total)
	return total
}

func (s *ProductService) GetStockByBranch(productID, branchID uint) float64 {
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
