package handler

// F3a: compras de insumos, proveedores, carga inicial e ingreso/merma para Tukichef. Rutas propias que SOLO tocan insumos: el
// personal con PIN no puede comprar ni ver otra cosa. Reutilizan los servicios de compras, contactos e inventario.

import (
	"errors"
	"strconv"
	"strings"
	"time"

	contactsvc "tukifac/internal/contacts/service"
	invsvc "tukifac/internal/inventory/service"
	purchasesvc "tukifac/internal/purchases/service"
	"tukifac/pkg/branch"
	"tukifac/pkg/database"
	"tukifac/pkg/tax"

	"github.com/gofiber/fiber/v3"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const supplyPurchasePage = 30

// onlySupplyPurchases: compras con al menos una línea y TODAS de insumos.
func onlySupplyPurchases(q *gorm.DB) *gorm.DB {
	return q.
		Where("EXISTS (SELECT 1 FROM tenant_purchase_items i WHERE i.purchase_id = tenant_purchases.id)").
		Where(`NOT EXISTS (SELECT 1 FROM tenant_purchase_items i LEFT JOIN tenant_products p ON p.id = i.product_id
			WHERE i.purchase_id = tenant_purchases.id AND COALESCE(p.is_supply, 0) = 0)`)
}

func writeBranch(c fiber.Ctx) (uint, error) {
	id, err := branch.ResolveWriteBranchID(c, 0)
	if err != nil || id == 0 {
		return 0, errors.New("selecciona una sucursal")
	}
	return id, nil
}

// ── Proveedores ──

type supplierView struct {
	ID           uint   `json:"id"`
	DocType      string `json:"doc_type"`
	DocNumber    string `json:"doc_number"`
	BusinessName string `json:"business_name"`
	Phone        string `json:"phone"`
}

func toSupplierView(c database.TenantContact) supplierView {
	return supplierView{ID: c.ID, DocType: c.DocType, DocNumber: c.DocNumber, BusinessName: c.BusinessName, Phone: c.Phone}
}

func (h *RestaurantHandler) ListSuppliers(c fiber.Ctx) error {
	rows, _, err := contactsvc.NewContactService(db(c)).ListPaged(contactsvc.ContactListParams{
		Query: strings.TrimSpace(c.Query("q")), TypesIn: []string{"supplier", "both"},
	}, 1, supplyPurchasePage)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	out := make([]supplierView, 0, len(rows))
	for _, r := range rows {
		out = append(out, toSupplierView(r))
	}
	return c.JSON(fiber.Map{"data": out})
}

func (h *RestaurantHandler) CreateSupplier(c fiber.Ctx) error {
	var body struct {
		DocType      string `json:"doc_type"`
		DocNumber    string `json:"doc_number"`
		BusinessName string `json:"business_name"`
		Phone        string `json:"phone"`
	}
	if err := c.Bind().JSON(&body); err != nil {
		return supplyBadRequest(c, "datos inválidos")
	}
	contact, err := contactsvc.NewContactService(db(c)).Create(contactsvc.ContactInput{
		Type: "supplier", DocType: strings.TrimSpace(body.DocType), DocNumber: strings.TrimSpace(body.DocNumber),
		BusinessName: strings.TrimSpace(body.BusinessName), Phone: strings.TrimSpace(body.Phone),
	})
	if err != nil {
		return supplyBadRequest(c, err.Error())
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"data": toSupplierView(*contact)})
}

// ── Compras ──

type supplyPurchaseLineBody struct {
	SupplyID   uint    `json:"supply_id"`
	SaleUnitID *uint   `json:"sale_unit_id"`
	Quantity   float64 `json:"quantity"`
	// LineTotal: importe pagado por la línea (con o sin IGV según price_includes_igv). Se pide el total, no el costo unitario.
	LineTotal float64 `json:"line_total"`
}

type supplyPurchaseBody struct {
	SupplierID       uint                     `json:"supplier_id"`
	DocType          string                   `json:"doc_type"`
	Series           string                   `json:"series"`
	Number           string                   `json:"number"`
	IssueDate        string                   `json:"issue_date"`
	DueDate          string                   `json:"due_date"`
	PaymentMethod    string                   `json:"payment_method"` // vacío o "credit" = por pagar
	Notes            string                   `json:"notes"`
	PriceIncludesIgv bool                     `json:"price_includes_igv"`
	IdempotencyKey   string                   `json:"idempotency_key"`
	Lines            []supplyPurchaseLineBody `json:"lines"`
}

type supplyPurchaseLineView struct {
	ProductID uint    `json:"product_id"`
	Name      string  `json:"name"`
	Unit      string  `json:"unit"`
	Quantity  float64 `json:"quantity"`
	UnitCost  float64 `json:"unit_cost"`
	LineTotal float64 `json:"line_total"`
}

type supplyPurchaseView struct {
	ID            uint                     `json:"id"`
	Supplier      string                   `json:"supplier"`
	DocType       string                   `json:"doc_type"`
	Series        string                   `json:"series"`
	Number        string                   `json:"number"`
	IssueDate     string                   `json:"issue_date"`
	Total         float64                  `json:"total"`
	PaymentMethod string                   `json:"payment_method"`
	Status        string                   `json:"status"`
	Lines         []supplyPurchaseLineView `json:"lines,omitempty"`
}

func supplyPurchaseViews(gdb *gorm.DB, purchases []database.TenantPurchase, withLines bool) []supplyPurchaseView {
	out := make([]supplyPurchaseView, 0, len(purchases))
	contactIDs := make([]uint, 0, len(purchases))
	for _, p := range purchases {
		if p.ContactID != nil {
			contactIDs = append(contactIDs, *p.ContactID)
		}
	}
	names := map[uint]string{}
	if len(contactIDs) > 0 {
		var contacts []database.TenantContact
		gdb.Unscoped().Select("id", "business_name").Where("id IN ?", contactIDs).Find(&contacts)
		for _, ct := range contacts {
			names[ct.ID] = ct.BusinessName
		}
	}
	for _, p := range purchases {
		v := supplyPurchaseView{ID: p.ID, DocType: p.DocType, Series: p.Series, Number: p.Number, IssueDate: p.IssueDate.Format("2006-01-02"),
			Total: p.Total, PaymentMethod: p.PaymentMethod, Status: p.Status}
		if p.ContactID != nil {
			v.Supplier = names[*p.ContactID]
		}
		if withLines {
			var items []database.TenantPurchaseItem
			gdb.Where("purchase_id = ?", p.ID).Order("id").Find(&items)
			for _, it := range items {
				unit := it.Unit
				if it.SaleUnitID != nil {
					// La cantidad es de unidades de compra («1 Saco 50 kg»), no de la unidad base.
					var su database.TenantProductSaleUnit
					if gdb.Unscoped().Select("id", "name").Limit(1).Find(&su, *it.SaleUnitID).Error == nil && su.Name != "" {
						unit = su.Name
					}
				}
				line := supplyPurchaseLineView{Name: it.Description, Unit: unit, Quantity: it.Quantity, UnitCost: it.UnitCost, LineTotal: it.Total}
				if it.ProductID != nil {
					line.ProductID = *it.ProductID
				}
				v.Lines = append(v.Lines, line)
			}
		}
		out = append(out, v)
	}
	return out
}

func parseDay(raw string, fallback time.Time) (time.Time, error) {
	if strings.TrimSpace(raw) == "" {
		return fallback, nil
	}
	return time.ParseInLocation("2006-01-02", strings.TrimSpace(raw), fallback.Location())
}

// supplyPurchaseItems arma las líneas del servicio de compras a partir de insumos y unidades de compra.
func supplyPurchaseItems(gdb *gorm.DB, body supplyPurchaseBody) ([]purchasesvc.PurchaseItemInput, error) {
	if len(body.Lines) == 0 {
		return nil, errors.New("agrega al menos un insumo")
	}
	items := make([]purchasesvc.PurchaseItemInput, 0, len(body.Lines))
	for _, l := range body.Lines {
		var p database.TenantProduct
		if err := gdb.Where("id = ? AND is_supply = ? AND active = ?", l.SupplyID, true, true).First(&p).Error; err != nil {
			return nil, errors.New("solo se pueden comprar insumos activos")
		}
		if l.Quantity <= 0 || l.LineTotal <= 0 {
			return nil, errors.New("indica cantidad e importe de " + p.Name)
		}
		unit := p.Unit
		if l.SaleUnitID != nil && *l.SaleUnitID > 0 {
			var su database.TenantProductSaleUnit
			if err := gdb.Where("id = ? AND product_id = ? AND active = ?", *l.SaleUnitID, p.ID, true).First(&su).Error; err != nil {
				return nil, errors.New("la unidad de compra no corresponde a " + p.Name)
			}
			if su.Unit != "" {
				unit = su.Unit
			}
		}
		id := p.ID
		items = append(items, purchasesvc.PurchaseItemInput{
			ProductID: &id, Code: p.Code, Description: p.Name, Unit: unit, SaleUnitID: l.SaleUnitID, Quantity: l.Quantity,
			UnitCost: l.LineTotal / l.Quantity, IgvAffectationType: p.IgvAffectationType, PriceIncludesIgv: body.PriceIncludesIgv,
		})
	}
	return items, nil
}

func (h *RestaurantHandler) CreateSupplyPurchase(c fiber.Ctx) error {
	var body supplyPurchaseBody
	if err := c.Bind().JSON(&body); err != nil {
		return supplyBadRequest(c, "datos inválidos")
	}
	if strings.TrimSpace(body.IdempotencyKey) == "" || len(body.IdempotencyKey) > 64 {
		return supplyBadRequest(c, "falta la clave del intento")
	}
	gdb := db(c)
	if !purchasesvc.IdempotencyReady(gdb) {
		return supplyBadRequest(c, "las compras de insumos aún no están disponibles para esta empresa")
	}
	branchID, err := writeBranch(c)
	if err != nil {
		return supplyBadRequest(c, err.Error())
	}
	var supplier database.TenantContact
	if err := gdb.Where("id = ? AND type IN ?", body.SupplierID, []string{"supplier", "both"}).First(&supplier).Error; err != nil {
		return supplyBadRequest(c, "elige un proveedor")
	}
	items, err := supplyPurchaseItems(gdb, body)
	if err != nil {
		return supplyBadRequest(c, err.Error())
	}
	now := time.Now()
	issue, err := parseDay(body.IssueDate, now)
	if err != nil {
		return supplyBadRequest(c, "fecha de emisión inválida")
	}
	var due *time.Time
	if strings.TrimSpace(body.DueDate) != "" {
		d, err := parseDay(body.DueDate, now)
		if err != nil {
			return supplyBadRequest(c, "fecha de vencimiento inválida")
		}
		due = &d
	}
	method := strings.TrimSpace(body.PaymentMethod)
	if strings.EqualFold(method, "credit") {
		method = ""
	}
	docType := strings.ToUpper(strings.TrimSpace(body.DocType))
	if docType == "" {
		docType = "FACTURA"
	}
	contactID := supplier.ID
	purchase, err := purchasesvc.NewPurchaseService(gdb).Create(purchasesvc.CreatePurchaseInput{
		BranchID: branchID, ContactID: &contactID, UserID: uid(c), DocType: docType,
		Series: strings.ToUpper(strings.TrimSpace(body.Series)), Number: strings.TrimSpace(body.Number),
		IssueDate: issue, DueDate: due, Currency: "PEN", PaymentMethod: method, Notes: strings.TrimSpace(body.Notes),
		PriceIncludesIgv: body.PriceIncludesIgv, Items: items, TaxConfig: tax.LoadFromDB(gdb),
		IdempotencyKey: body.IdempotencyKey, RejectDuplicateDocument: true,
	})
	status := fiber.StatusCreated
	switch {
	case errors.Is(err, purchasesvc.ErrIdempotentReplay):
		status = fiber.StatusOK
	case errors.Is(err, purchasesvc.ErrDuplicateDocument):
		return c.Status(fiber.StatusConflict).JSON(fiber.Map{"error": err.Error()})
	case err != nil:
		return supplyBadRequest(c, err.Error())
	}
	return c.Status(status).JSON(fiber.Map{"data": supplyPurchaseViews(gdb, []database.TenantPurchase{*purchase}, true)[0]})
}

func (h *RestaurantHandler) ListSupplyPurchases(c fiber.Ctx) error {
	branchID, err := writeBranch(c)
	if err != nil {
		return supplyBadRequest(c, err.Error())
	}
	page, _ := strconv.Atoi(c.Query("page"))
	if page < 1 {
		page = 1
	}
	q := onlySupplyPurchases(db(c).Model(&database.TenantPurchase{}).Where("tenant_purchases.branch_id = ?", branchID))
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	var rows []database.TenantPurchase
	if err := q.Order("tenant_purchases.id DESC").Limit(supplyPurchasePage).Offset((page - 1) * supplyPurchasePage).Find(&rows).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"data": supplyPurchaseViews(db(c), rows, false), "total": total})
}

// supplyPurchaseByID: solo compras de insumos de la sucursal activa.
func supplyPurchaseByID(c fiber.Ctx, gdb *gorm.DB) (*database.TenantPurchase, error) {
	branchID, err := writeBranch(c)
	if err != nil {
		return nil, err
	}
	id, err := strconv.ParseUint(c.Params("id"), 10, 32)
	if err != nil || id == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	q := onlySupplyPurchases(gdb.Where("tenant_purchases.id = ? AND tenant_purchases.branch_id = ?", id, branchID))
	var p database.TenantPurchase
	if err := q.First(&p).Error; err != nil {
		return nil, err
	}
	return &p, nil
}

func supplyPurchaseNotFound(c fiber.Ctx, err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "compra no encontrada"})
	}
	return supplyBadRequest(c, err.Error())
}

func (h *RestaurantHandler) GetSupplyPurchase(c fiber.Ctx) error {
	p, err := supplyPurchaseByID(c, db(c))
	if err != nil {
		return supplyPurchaseNotFound(c, err)
	}
	return c.JSON(fiber.Map{"data": supplyPurchaseViews(db(c), []database.TenantPurchase{*p}, true)[0]})
}

func (h *RestaurantHandler) VoidSupplyPurchase(c fiber.Ctx) error {
	// La fila queda bloqueada hasta terminar: un doble toque no revierte dos veces el stock ni la caja.
	err := db(c).Transaction(func(tx *gorm.DB) error {
		// Primera consulta, y simple: bloquea antes de que la transacción tome su foto de datos.
		if tx.Dialector.Name() == "mysql" {
			var ids []uint
			if err := tx.Model(&database.TenantPurchase{}).Select("id").Where("id = ?", c.Params("id")).
				Clauses(clause.Locking{Strength: "UPDATE"}).Find(&ids).Error; err != nil {
				return err
			}
		}
		p, err := supplyPurchaseByID(c, tx)
		if err != nil {
			return err
		}
		return purchasesvc.NewPurchaseService(tx).Void(p.ID, uid(c))
	})
	if err != nil {
		return supplyPurchaseNotFound(c, err)
	}
	return c.JSON(fiber.Map{"success": true})
}

// ── Carga inicial, ingreso y merma ──

func (h *RestaurantHandler) SupplyInitialStock(c fiber.Ctx) error {
	p, err := supplyByID(c)
	if err != nil {
		return supplyNotFound(c, err)
	}
	var body struct {
		Quantity float64 `json:"quantity"`
		UnitCost float64 `json:"unit_cost"`
	}
	if err := c.Bind().JSON(&body); err != nil {
		return supplyBadRequest(c, "datos inválidos")
	}
	branchID, err := writeBranch(c)
	if err != nil {
		return supplyBadRequest(c, err.Error())
	}
	cost := body.UnitCost
	if !canViewSupplyCost(c) {
		cost = 0 // sin permiso de costos no se fija ninguno
	}
	if err := invsvc.NewInventoryService(db(c)).RecordSupplyInitialStock(p.ID, branchID, body.Quantity, cost, uid(c)); err != nil {
		if errors.Is(err, invsvc.ErrSupplyAlreadyLoaded) {
			return c.Status(fiber.StatusConflict).JSON(fiber.Map{"error": err.Error()})
		}
		return supplyBadRequest(c, err.Error())
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"success": true})
}

var supplyEntryKinds = map[string]struct{ direction, operation string }{
	"ingreso":     {"IN", "OTHER_IN"},
	"merma":       {"OUT", "SHRINKAGE"},
	"desperdicio": {"OUT", "WASTE"},
}

func (h *RestaurantHandler) CreateSupplyEntry(c fiber.Ctx) error {
	var body struct {
		SupplyID       uint    `json:"supply_id"`
		Kind           string  `json:"kind"` // ingreso | merma | desperdicio
		Quantity       float64 `json:"quantity"`
		TotalCost      float64 `json:"total_cost"` // solo ingreso: lo pagado por toda la cantidad
		Notes          string  `json:"notes"`
		IdempotencyKey string  `json:"idempotency_key"`
	}
	if err := c.Bind().JSON(&body); err != nil {
		return supplyBadRequest(c, "datos inválidos")
	}
	kind, ok := supplyEntryKinds[strings.ToLower(strings.TrimSpace(body.Kind))]
	if !ok {
		return supplyBadRequest(c, "tipo de movimiento no válido")
	}
	var p database.TenantProduct
	if err := db(c).Where("id = ? AND is_supply = ?", body.SupplyID, true).First(&p).Error; err != nil {
		return supplyNotFound(c, err)
	}
	if body.Quantity <= 0 || body.TotalCost < 0 {
		return supplyBadRequest(c, "indica una cantidad válida")
	}
	branchID, err := writeBranch(c)
	if err != nil {
		return supplyBadRequest(c, err.Error())
	}
	var unitCost float64
	if kind.direction == "IN" && canViewSupplyCost(c) {
		unitCost = body.TotalCost / body.Quantity
	}
	docID, replay, err := invsvc.NewInventoryDocumentService(db(c)).RecordSupplyEntry(invsvc.SupplyEntryInput{
		ProductID: p.ID, BranchID: branchID, UserID: uid(c), Direction: kind.direction, Operation: kind.operation,
		Quantity: body.Quantity, UnitCost: unitCost, Notes: body.Notes, Key: body.IdempotencyKey,
	})
	if err != nil {
		return supplyBadRequest(c, err.Error())
	}
	status := fiber.StatusCreated
	if replay {
		status = fiber.StatusOK
	}
	return c.Status(status).JSON(fiber.Map{"data": fiber.Map{"document_id": docID}})
}

// ── Historial de compras de un insumo ──

type supplyHistoryRow struct {
	PurchaseID uint     `json:"purchase_id"`
	IssueDate  string   `json:"issue_date"`
	Supplier   string   `json:"supplier"`
	Document   string   `json:"document"`
	Quantity   float64  `json:"quantity"`
	Unit       string   `json:"unit"`
	Status     string   `json:"status"`
	LineTotal  *float64 `json:"line_total,omitempty"`
}

func (h *RestaurantHandler) SupplyPurchaseHistory(c fiber.Ctx) error {
	p, err := supplyByID(c)
	if err != nil {
		return supplyNotFound(c, err)
	}
	q := db(c).Table("tenant_purchase_items AS i").
		Select("tp.id AS purchase_id, tp.issue_date, ct.business_name AS supplier, tp.series, tp.number, i.quantity, COALESCE(su.name, i.unit) AS unit, tp.status, i.total").
		Joins("LEFT JOIN tenant_product_sale_units su ON su.id = i.sale_unit_id").
		Joins("JOIN tenant_purchases tp ON tp.id = i.purchase_id").
		Joins("LEFT JOIN tenant_contacts ct ON ct.id = tp.contact_id").
		Where("i.product_id = ?", p.ID)
	if branchID := supplyBranch(c); branchID > 0 {
		q = q.Where("tp.branch_id = ?", branchID)
	}
	var rows []struct {
		PurchaseID uint
		IssueDate  time.Time
		Supplier   string
		Series     string
		Number     string
		Quantity   float64
		Unit       string
		Status     string
		Total      float64
	}
	if err := q.Order("tp.issue_date DESC, i.id DESC").Limit(supplyPurchasePage).Scan(&rows).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	withCost := canViewSupplyCost(c)
	out := make([]supplyHistoryRow, 0, len(rows))
	for _, r := range rows {
		row := supplyHistoryRow{PurchaseID: r.PurchaseID, IssueDate: r.IssueDate.Format("2006-01-02"), Supplier: r.Supplier,
			Document: r.Series + "-" + r.Number, Quantity: r.Quantity, Unit: r.Unit, Status: r.Status}
		if withCost {
			total := r.Total
			row.LineTotal = &total
		}
		out = append(out, row)
	}
	return c.JSON(fiber.Map{"data": out})
}
