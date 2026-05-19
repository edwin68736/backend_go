package handler

import (
	"encoding/json"
	"strconv"
	"time"

	"tukifac/internal/purchases/service"
	"tukifac/pkg/database"
	"tukifac/pkg/tax"

	"github.com/gofiber/fiber/v3"
	"gorm.io/gorm"
)

type PurchaseHandler struct{}

func NewPurchaseHandler() *PurchaseHandler { return &PurchaseHandler{} }

// PurchaseRow aplana los datos de una compra para la vista de lista
type PurchaseRow struct {
	ID           uint
	DocType      string
	Series       string
	Number       string
	IssueDate    time.Time
	SupplierName string
	Currency     string
	Total        float64
	Status       string
}

func db(c fiber.Ctx) *gorm.DB {
	v, _ := c.Locals("tenantDB").(*gorm.DB)
	return v
}
func email(c fiber.Ctx) string {
	v, _ := c.Locals("user_email").(string)
	return v
}
func userID(c fiber.Ctx) uint {
	v, _ := c.Locals("user_id").(uint)
	return v
}

func (h *PurchaseHandler) ListPage(c fiber.Ctx) error {
	tdb := db(c)
	svc := service.NewPurchaseService(tdb)
	purchases, _, _ := svc.List(service.PurchaseListParams{
		Query: c.Query("q"),
	})

	// Cargar contactos para obtener nombres de proveedores
	var contacts []database.TenantContact
	tdb.Find(&contacts)
	contactMap := make(map[uint]string)
	for _, ct := range contacts {
		contactMap[ct.ID] = ct.BusinessName
	}

	rows := make([]PurchaseRow, 0, len(purchases))
	for _, p := range purchases {
		supplierName := ""
		if p.ContactID != nil {
			supplierName = contactMap[*p.ContactID]
		}
		rows = append(rows, PurchaseRow{
			ID:           p.ID,
			DocType:      p.DocType,
			Series:       p.Series,
			Number:       p.Number,
			IssueDate:    p.IssueDate,
			SupplierName: supplierName,
			Currency:     p.Currency,
			Total:        p.Total,
			Status:       p.Status,
		})
	}

	return c.Render("purchases/index", fiber.Map{
		"Title":     "Compras",
		"UserEmail": email(c),
		"Purchases": rows,
		"Success":   c.Query("success"),
	}, "layouts/base")
}

func (h *PurchaseHandler) NewPage(c fiber.Ctx) error {
	tdb := db(c)
	var branches []database.TenantBranch
	tdb.Where("active = ?", true).Find(&branches)
	var suppliers []database.TenantContact
	tdb.Where("active = ? AND (type = 'supplier' OR type = 'both')", true).Order("business_name").Find(&suppliers)
	var products []database.TenantProduct
	tdb.Where("active = ?", true).Order("name").Find(&products)

	return c.Render("purchases/form", fiber.Map{
		"Title":     "Nueva Compra",
		"UserEmail": email(c),
		"Branches":  branches,
		"Suppliers": suppliers,
		"Products":  products,
		"Today":     time.Now().Format("2006-01-02"),
	}, "layouts/base")
}

func (h *PurchaseHandler) CreateForm(c fiber.Ctx) error {
	tdb := db(c)
	branchID, _ := strconv.ParseUint(c.FormValue("branch_id"), 10, 32)

	var supplierID *uint
	if sid, err := strconv.ParseUint(c.FormValue("supplier_id"), 10, 32); err == nil && sid > 0 {
		v := uint(sid)
		supplierID = &v
	}

	issueDate := time.Now()
	if d := c.FormValue("purchase_date"); d != "" {
		if t, err := time.Parse("2006-01-02", d); err == nil {
			issueDate = t
		}
	}

	// Parsear items del JSON serializado por el frontend
	var items []service.PurchaseItemInput
	if itemsJSON := c.FormValue("items_json"); itemsJSON != "" {
		json.Unmarshal([]byte(itemsJSON), &items)
	}

	if len(items) == 0 {
		var branches []database.TenantBranch
		tdb.Where("active = ?", true).Find(&branches)
		var suppliers []database.TenantContact
		tdb.Where("active = ? AND (type = 'supplier' OR type = 'both')", true).Order("business_name").Find(&suppliers)
		var products []database.TenantProduct
		tdb.Where("active = ?", true).Order("name").Find(&products)
		return c.Render("purchases/form", fiber.Map{
			"Title":     "Nueva Compra",
			"UserEmail": email(c),
			"Branches":  branches,
			"Suppliers": suppliers,
			"Products":  products,
			"Today":     c.FormValue("purchase_date"),
			"Error":     "Debes agregar al menos un ítem",
		}, "layouts/base")
	}

	taxCfg := tax.LoadFromDB(tdb)
	svc := service.NewPurchaseService(tdb)
	purchase, err := svc.Create(service.CreatePurchaseInput{
		BranchID:  uint(branchID),
		ContactID: supplierID,
		UserID:    userID(c),
		DocType:   c.FormValue("doc_type"),
		Series:    c.FormValue("series"),
		Number:    c.FormValue("number"),
		IssueDate: issueDate,
		Currency:  c.FormValue("currency"),
		Status:    c.FormValue("status"),
		Notes:     c.FormValue("notes"),
		Items:     items,
		TaxConfig: taxCfg,
	})
	if err != nil {
		var branches []database.TenantBranch
		tdb.Where("active = ?", true).Find(&branches)
		var suppliers []database.TenantContact
		tdb.Where("active = ? AND (type = 'supplier' OR type = 'both')", true).Order("business_name").Find(&suppliers)
		var products []database.TenantProduct
		tdb.Where("active = ?", true).Order("name").Find(&products)
		return c.Render("purchases/form", fiber.Map{
			"Title":     "Nueva Compra",
			"UserEmail": email(c),
			"Branches":  branches,
			"Suppliers": suppliers,
			"Products":  products,
			"Today":     c.FormValue("purchase_date"),
			"Error":     err.Error(),
		}, "layouts/base")
	}
	return c.Redirect().To("/purchases/" + strconv.FormatUint(uint64(purchase.ID), 10))
}

func (h *PurchaseHandler) DetailPage(c fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 32)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).SendString("ID inválido")
	}
	svc := service.NewPurchaseService(db(c))
	purchase, err := svc.GetByID(uint(id))
	if err != nil {
		return c.Status(fiber.StatusNotFound).SendString("Compra no encontrada")
	}
	items, _ := svc.GetItems(purchase.ID)

	var contact *database.TenantContact
	if purchase.ContactID != nil {
		var ct database.TenantContact
		if db(c).First(&ct, *purchase.ContactID).Error == nil {
			contact = &ct
		}
	}

	return c.Render("purchases/detail", fiber.Map{
		"Title":    "Detalle de Compra — " + purchase.Series + "-" + purchase.Number,
		"UserEmail": email(c),
		"Purchase": purchase,
		"Items":    items,
		"Contact":  contact,
	}, "layouts/base")
}
