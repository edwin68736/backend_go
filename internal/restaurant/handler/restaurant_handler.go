package handler

import (
	"strconv"
	"time"

	"tukifac/internal/restaurant/service"
	salesvc "tukifac/internal/sales/service"
	"tukifac/pkg/tax"

	"github.com/gofiber/fiber/v3"
	"gorm.io/gorm"
)

type RestaurantHandler struct{}

func New() *RestaurantHandler { return &RestaurantHandler{} }

func db(c fiber.Ctx) *gorm.DB  { v, _ := c.Locals("tenantDB").(*gorm.DB); return v }
func uid(c fiber.Ctx) uint      { v, _ := c.Locals("user_id").(uint); return v }
func bid(c fiber.Ctx) uint {
	v, _ := c.Locals("branch_id").(uint)
	if v == 0 {
		v = 1
	}
	return v
}
func svc(c fiber.Ctx) *service.RestaurantService { return service.New(db(c)) }

// GET /api/restaurant/me — rol restaurante del usuario actual (desde JWT)
func (h *RestaurantHandler) GetMyRestaurantRole(c fiber.Ctx) error {
	role, _ := c.Locals("restaurant_role").(string)
	return c.JSON(fiber.Map{"restaurant_role": role})
}

// GET /api/restaurant/roles/assignments — lista user_id -> role (solo admin)
func (h *RestaurantHandler) ListRestaurantRoleAssignments(c fiber.Ctx) error {
	m, err := svc(c).ListUserRestaurantRoles()
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"data": m})
}

// PUT /api/restaurant/users/:id/restaurant-role — asigna rol (solo admin)
func (h *RestaurantHandler) SetUserRestaurantRole(c fiber.Ctx) error {
	id, err := parseID(c)
	if err != nil {
		return err
	}
	var body struct {
		Role string `json:"role"` // admin, vendedor, mozo, cocinero, o "" para quitar
	}
	if err := c.Bind().JSON(&body); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "JSON inválido"})
	}
	if err := svc(c).SetUserRestaurantRole(id, body.Role); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"success": true})
}

// GET /api/restaurant/settings  — retorna si hay PIN configurado (no expone el PIN)
func (h *RestaurantHandler) GetSettings(c fiber.Ctx) error {
	cfg, err := svc(c).GetRestaurantSettings()
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"has_deletion_pin": cfg.DeletionPin != ""})
}

// PUT /api/restaurant/settings  — guarda el PIN de anulación (desde panel tenant)
func (h *RestaurantHandler) UpdateSettings(c fiber.Ctx) error {
	var body struct {
		DeletionPin string `json:"deletion_pin"`
	}
	if err := c.Bind().JSON(&body); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "JSON inválido"})
	}
	if err := svc(c).SaveRestaurantSettings(body.DeletionPin); err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"success": true})
}

// ================================================================
// PISOS
// ================================================================

// GET /api/restaurant/floors
func (h *RestaurantHandler) ListFloors(c fiber.Ctx) error {
	floors, err := svc(c).ListFloors()
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"data": floors})
}

// POST /api/restaurant/floors
func (h *RestaurantHandler) CreateFloor(c fiber.Ctx) error {
	var body struct {
		Name  string `json:"name"`
		Order int    `json:"sort_order"`
	}
	if err := c.Bind().JSON(&body); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "datos inválidos"})
	}
	f, err := svc(c).CreateFloor(body.Name, body.Order)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}
	return c.Status(201).JSON(fiber.Map{"success": true, "data": f})
}

// PUT /api/restaurant/floors/:id
func (h *RestaurantHandler) UpdateFloor(c fiber.Ctx) error {
	id, err := parseID(c)
	if err != nil {
		return err
	}
	var body struct {
		Name   string `json:"name"`
		Order  int    `json:"sort_order"`
		Active bool   `json:"active"`
	}
	body.Active = true
	if err := c.Bind().JSON(&body); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "datos inválidos"})
	}
	if err := svc(c).UpdateFloor(id, body.Name, body.Order, body.Active); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"success": true})
}

// DELETE /api/restaurant/floors/:id
func (h *RestaurantHandler) DeleteFloor(c fiber.Ctx) error {
	id, err := parseID(c)
	if err != nil {
		return err
	}
	if err := svc(c).DeleteFloor(id); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"success": true})
}

// ================================================================
// MESAS
// ================================================================

// GET /api/restaurant/tables?floor_id=
func (h *RestaurantHandler) ListTables(c fiber.Ctx) error {
	floorID, _ := strconv.ParseUint(c.Query("floor_id"), 10, 32)
	tables, err := svc(c).ListTables(uint(floorID))
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"data": tables})
}

// POST /api/restaurant/tables
func (h *RestaurantHandler) CreateTable(c fiber.Ctx) error {
	var body struct {
		FloorID  uint   `json:"floor_id"`
		Name     string `json:"name"`
		Capacity int    `json:"capacity"`
	}
	if err := c.Bind().JSON(&body); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "datos inválidos"})
	}
	if body.Capacity == 0 {
		body.Capacity = 4
	}
	t, err := svc(c).CreateTable(body.FloorID, body.Name, body.Capacity)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}
	return c.Status(201).JSON(fiber.Map{"success": true, "data": t})
}

// PUT /api/restaurant/tables/:id
func (h *RestaurantHandler) UpdateTable(c fiber.Ctx) error {
	id, err := parseID(c)
	if err != nil {
		return err
	}
	var body struct {
		Name     string `json:"name"`
		Capacity int    `json:"capacity"`
		Active   *bool  `json:"active"`
	}
	if err := c.Bind().JSON(&body); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "datos inválidos"})
	}
	active := true
	if body.Active != nil {
		active = *body.Active
	}
	if err := svc(c).UpdateTable(id, body.Name, body.Capacity, active); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"success": true})
}

// DELETE /api/restaurant/tables/:id
func (h *RestaurantHandler) DeleteTable(c fiber.Ctx) error {
	id, err := parseID(c)
	if err != nil {
		return err
	}
	if err := svc(c).DeleteTable(id); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"success": true})
}

// ================================================================
// MOZOS
// ================================================================

// GET /api/restaurant/waiters
func (h *RestaurantHandler) ListWaiters(c fiber.Ctx) error {
	w, err := svc(c).ListWaiters()
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"data": w})
}

// POST /api/restaurant/waiters
func (h *RestaurantHandler) CreateWaiter(c fiber.Ctx) error {
	var body struct {
		Name   string `json:"name"`
		Code   string `json:"code"`
		UserID *uint  `json:"user_id"`
	}
	if err := c.Bind().JSON(&body); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "datos inválidos"})
	}
	w, err := svc(c).CreateWaiter(body.Name, body.Code, body.UserID)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}
	return c.Status(201).JSON(fiber.Map{"success": true, "data": w})
}

// PUT /api/restaurant/waiters/:id
func (h *RestaurantHandler) UpdateWaiter(c fiber.Ctx) error {
	id, err := parseID(c)
	if err != nil {
		return err
	}
	var body struct {
		Name   string `json:"name"`
		Code   string `json:"code"`
		Active *bool  `json:"active"`
	}
	if err := c.Bind().JSON(&body); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "datos inválidos"})
	}
	active := true
	if body.Active != nil {
		active = *body.Active
	}
	if err := svc(c).UpdateWaiter(id, body.Name, body.Code, active); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"success": true})
}

// DELETE /api/restaurant/waiters/:id
func (h *RestaurantHandler) DeleteWaiter(c fiber.Ctx) error {
	id, err := parseID(c)
	if err != nil {
		return err
	}
	if err := svc(c).DeleteWaiter(id); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"success": true})
}

// ================================================================
// SESIONES (APERTURA / CIERRE DE MESA)
// ================================================================

// POST /api/restaurant/sessions  — abre una mesa o inicia pedido rápido
func (h *RestaurantHandler) OpenSession(c fiber.Ctx) error {
	var body struct {
		TableID  *uint  `json:"table_id"`  // null = pedido rápido sin mesa
		WaiterID *uint  `json:"waiter_id"`
		Guests   int    `json:"guests"`
		Notes    string `json:"notes"`
	}
	if err := c.Bind().JSON(&body); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "datos inválidos"})
	}
	if body.Guests == 0 {
		body.Guests = 1
	}
	sess, err := svc(c).OpenTable(body.TableID, body.WaiterID, bid(c), uid(c), body.Guests, body.Notes)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}
	return c.Status(201).JSON(fiber.Map{"success": true, "data": sess})
}

// GET /api/restaurant/sessions/:id
func (h *RestaurantHandler) GetSession(c fiber.Ctx) error {
	id, err := parseID(c)
	if err != nil {
		return err
	}
	detail, err := svc(c).GetSessionDetail(id)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"data": detail})
}

// GET /api/restaurant/tables/:id/session
func (h *RestaurantHandler) GetTableSession(c fiber.Ctx) error {
	tableID, err := parseID(c)
	if err != nil {
		return err
	}
	sess, err := svc(c).GetActiveSessionByTable(tableID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	if sess == nil {
		return c.Status(404).JSON(fiber.Map{"error": "no hay sesión activa en esta mesa"})
	}
	detail, _ := svc(c).GetSessionDetail(sess.ID)
	return c.JSON(fiber.Map{"data": detail})
}

// POST /api/restaurant/sessions/:id/cancel
func (h *RestaurantHandler) CancelSession(c fiber.Ctx) error {
	id, err := parseID(c)
	if err != nil {
		return err
	}
	var body struct {
		Reason string `json:"reason"`
	}
	c.Bind().JSON(&body)
	if err := svc(c).CancelSession(id, body.Reason, uid(c)); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"success": true})
}

// ================================================================
// PEDIDOS
// ================================================================

// POST /api/restaurant/sessions/:id/orders
func (h *RestaurantHandler) AddOrder(c fiber.Ctx) error {
	sessionID, err := parseID(c)
	if err != nil {
		return err
	}
	var body struct {
		WaiterID *uint                     `json:"waiter_id"`
		Notes    string                    `json:"notes"`
		Items    []service.NewOrderItem    `json:"items"`
	}
	if err := c.Bind().JSON(&body); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "datos inválidos"})
	}
	order, err := svc(c).AddOrder(sessionID, body.WaiterID, uid(c), body.Items, body.Notes)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}
	return c.Status(201).JSON(fiber.Map{"success": true, "data": order})
}

// ================================================================
// COMANDAS
// ================================================================

// PUT /api/restaurant/comandas/:id/status
func (h *RestaurantHandler) UpdateComandaStatus(c fiber.Ctx) error {
	id, err := parseID(c)
	if err != nil {
		return err
	}
	var body struct {
		Status string `json:"status"`
	}
	if err := c.Bind().JSON(&body); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "datos inválidos"})
	}
	if err := svc(c).UpdateComandaStatus(id, body.Status, uid(c)); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"success": true})
}

// DELETE /api/restaurant/comandas/:id   (anulación; requiere PIN de ajustes)
func (h *RestaurantHandler) CancelComanda(c fiber.Ctx) error {
	id, err := parseID(c)
	if err != nil {
		return err
	}
	var body struct {
		Reason string `json:"reason"`
		Pin    string `json:"pin"`
	}
	if err := c.Bind().JSON(&body); err != nil || body.Reason == "" {
		return c.Status(400).JSON(fiber.Map{"error": "se requiere motivo de anulación y PIN"})
	}
	if err := svc(c).VerifyDeletionPin(body.Pin); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}
	if err := svc(c).CancelComanda(id, body.Reason, uid(c)); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"success": true})
}

// POST /api/restaurant/comandas/:id/print
func (h *RestaurantHandler) PrintComanda(c fiber.Ctx) error {
	id, err := parseID(c)
	if err != nil {
		return err
	}
	if err := svc(c).MarkComandaPrinted(id); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"success": true})
}

// GET /api/restaurant/kitchen   — vista de cocina: comandas activas
func (h *RestaurantHandler) KitchenView(c fiber.Ctx) error {
	comandas, err := svc(c).GetKitchenComandas(bid(c))
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"data": comandas})
}

// ================================================================
// COBRO DE MESA
// ================================================================

// POST /api/restaurant/sessions/:id/bill
func (h *RestaurantHandler) BillSession(c fiber.Ctx) error {
	sessionID, err := parseID(c)
	if err != nil {
		return err
	}
	var body struct {
		SeriesID      uint                     `json:"series_id"`
		DocType       string                   `json:"doc_type"`
		Currency      string                   `json:"currency"`
		ContactID     *uint                    `json:"contact_id"`
		CashSessionID *uint                    `json:"cash_session_id"`
		IssueDate     string                   `json:"issue_date"`
		CloseSession  *bool                    `json:"close_session"` // true = cerrar mesa tras cobrar; false = solo generar venta, mesa sigue abierta
		Payments      []service.PaymentInput   `json:"payments"`
	}
	if err := c.Bind().JSON(&body); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "datos inválidos"})
	}
	if len(body.Payments) == 0 {
		return c.Status(400).JSON(fiber.Map{"error": "se requiere al menos un método de pago"})
	}
	if body.SeriesID == 0 {
		return c.Status(400).JSON(fiber.Map{"error": "se requiere una serie de documento"})
	}
	closeSession := true
	if body.CloseSession != nil {
		closeSession = *body.CloseSession
	}

	issueDate := time.Now()
	if body.IssueDate != "" {
		if t, parseErr := time.Parse("2006-01-02", body.IssueDate); parseErr == nil {
			issueDate = t
		}
	}

	taxCfg := tax.LoadFromDB(db(c))
	sale, err := svc(c).BillTable(service.BillInput{
		SessionID:     sessionID,
		UserID:        uid(c),
		SeriesID:      body.SeriesID,
		DocType:       body.DocType,
		IssueDate:     issueDate,
		Currency:      body.Currency,
		ContactID:     body.ContactID,
		Payments:      body.Payments,
		CashSessionID: body.CashSessionID,
		CloseSession:  closeSession,
	}, taxCfg)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}
	printData, _ := salesvc.BuildPrintDataForSale(db(c), sale.ID)
	return c.Status(201).JSON(fiber.Map{"success": true, "data": sale, "print_data": printData})
}

// POST /api/restaurant/sessions/:id/close — cierra la mesa sin generar venta (mesa ya pagada).
func (h *RestaurantHandler) CloseSession(c fiber.Ctx) error {
	sessionID, err := parseID(c)
	if err != nil {
		return err
	}
	if err := svc(c).CloseSessionOnly(sessionID); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"success": true})
}

// ================================================================
// PAGOS MÚLTIPLES (ventas generales)
// ================================================================

// POST /api/sales/:id/payments
func (h *RestaurantHandler) RegisterPayments(c fiber.Ctx) error {
	saleID, err := parseID(c)
	if err != nil {
		return err
	}
	var body struct {
		Payments []service.PaymentInput `json:"payments"`
	}
	if err := c.Bind().JSON(&body); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "datos inválidos"})
	}
	if len(body.Payments) == 0 {
		return c.Status(400).JSON(fiber.Map{"error": "se requiere al menos un pago"})
	}
	if err := svc(c).RegisterPayments(saleID, body.Payments, uid(c)); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}
	printData, _ := salesvc.BuildPrintDataForSale(db(c), saleID)
	return c.Status(201).JSON(fiber.Map{"success": true, "print_data": printData})
}

// GET /api/sales/:id/payments
func (h *RestaurantHandler) GetSalePayments(c fiber.Ctx) error {
	saleID, err := parseID(c)
	if err != nil {
		return err
	}
	payments, err := svc(c).GetSalePayments(saleID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"data": payments})
}

// ================================================================
// HELPERS
// ================================================================

func parseID(c fiber.Ctx) (uint, error) {
	id, err := strconv.ParseUint(c.Params("id"), 10, 32)
	if err != nil {
		_ = c.Status(400).JSON(fiber.Map{"error": "ID inválido"})
		return 0, err
	}
	return uint(id), nil
}
