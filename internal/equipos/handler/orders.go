package handler

import (
	"errors"
	"strconv"

	"tukifac/internal/equipos/service"
	"tukifac/pkg/middleware"

	"github.com/gofiber/fiber/v3"
)

func hasPerm(c fiber.Ctx, perm string) bool {
	claims, _ := c.Locals("sa_claims").(*middleware.SuperAdminClaims)
	return middleware.HasSAPermission(claims, perm)
}

func badBody(c fiber.Ctx) error {
	return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Datos inválidos"})
}

// ── Clientes ───────────────────────────────────────────────────────────────

func (h *Handler) ListCustomers(c fiber.Ctx) error {
	limit, _ := strconv.Atoi(c.Query("limit"))
	rows, err := svc().ListCustomers(c.Query("q"), limit)
	if err != nil {
		return fail(c, err)
	}
	if !hasPerm(c, "equipos.payments_view") { // los saldos son información de cobranza
		for i := range rows {
			rows[i].Balance, rows[i].Credit = 0, 0
		}
	}
	return c.JSON(fiber.Map{"data": rows})
}

func (h *Handler) CreateCustomer(c fiber.Ctx) error {
	var in service.CustomerInput
	if err := c.Bind().JSON(&in); err != nil {
		return badBody(c)
	}
	v, err := svc().CreateCustomer(in)
	if err != nil {
		return fail(c, err)
	}
	audit(c, "equip_customer_created", "equip_customer", v.ID, in)
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"data": v})
}

func (h *Handler) UpdateCustomer(c fiber.Ctx) error {
	id, ok := idParam(c, "id")
	if !ok {
		return nil
	}
	var in service.CustomerInput
	if err := c.Bind().JSON(&in); err != nil {
		return badBody(c)
	}
	v, err := svc().UpdateCustomer(id, in)
	if err != nil {
		return fail(c, err)
	}
	audit(c, "equip_customer_updated", "equip_customer", v.ID, in)
	return c.JSON(fiber.Map{"data": v})
}

func (h *Handler) CustomerAccount(c fiber.Ctx) error {
	id, ok := idParam(c, "id")
	if !ok {
		return nil
	}
	acc, err := svc().CustomerAccount(id)
	if err != nil {
		return fail(c, err)
	}
	return c.JSON(fiber.Map{"data": acc})
}

func (h *Handler) OpenBalances(c fiber.Ctx) error {
	id, ok := idParam(c, "id")
	if !ok {
		return nil
	}
	rows, err := svc().OpenBalances(id)
	if err != nil {
		return fail(c, err)
	}
	return c.JSON(fiber.Map{"data": rows})
}

// ── Pedidos ────────────────────────────────────────────────────────────────

// stockError traduce el aviso de stock negativo a un 409 con el detalle (el usuario puede reintentar confirmando con nota).
func stockError(c fiber.Ctx, err error) (bool, error) {
	var neg *service.NegativeStockError
	if errors.As(err, &neg) {
		return true, c.Status(fiber.StatusConflict).JSON(fiber.Map{"error": err.Error(), "code": "NEGATIVE_STOCK", "items": neg.Items})
	}
	return false, nil
}

// trimOrder oculta los cobros del pedido a quien no puede verlos.
func trimOrder(c fiber.Ctx, v *service.OrderView) {
	if v != nil && !hasPerm(c, "equipos.payments_view") {
		v.Payments = nil
	}
}

func (h *Handler) ListOrders(c fiber.Ctx) error {
	page, _ := strconv.Atoi(c.Query("page"))
	per, _ := strconv.Atoi(c.Query("per_page"))
	carrier, _ := strconv.ParseUint(c.Query("carrier_id"), 10, 32)
	res, err := svc().ListOrders(service.OrderFilter{
		Q: c.Query("q"), From: c.Query("from"), To: c.Query("to"), SaleType: c.Query("sale_type"), PaymentStatus: c.Query("payment_status"),
		ValidationStatus: c.Query("validation_status"), Status: c.Query("status"), ShipmentStatus: c.Query("shipment_status"),
		CarrierID: uint(carrier), Department: c.Query("department"), Page: page, PerPage: per,
	})
	if err != nil {
		return fail(c, err)
	}
	return c.JSON(fiber.Map{"data": res})
}

func (h *Handler) GetOrder(c fiber.Ctx) error {
	id, ok := idParam(c, "id")
	if !ok {
		return nil
	}
	v, err := svc().GetOrder(id)
	if err != nil {
		return fail(c, err)
	}
	trimOrder(c, v)
	return c.JSON(fiber.Map{"data": v})
}

func (h *Handler) CreateOrder(c fiber.Ctx) error {
	var in service.OrderInput
	if err := c.Bind().JSON(&in); err != nil {
		return badBody(c)
	}
	res, err := svc().CreateOrder(in, userID(c))
	if err != nil {
		return fail(c, err)
	}
	audit(c, "equip_order_created", "equip_order", res.Order.ID, in)
	trimOrder(c, res.Order)
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"data": res.Order, "warnings": res.Warnings})
}

func (h *Handler) UpdateOrder(c fiber.Ctx) error {
	id, ok := idParam(c, "id")
	if !ok {
		return nil
	}
	var body struct {
		service.OrderInput
		AllowNegative bool   `json:"allow_negative"`
		NegativeNote  string `json:"negative_note"`
	}
	if err := c.Bind().JSON(&body); err != nil {
		return badBody(c)
	}
	res, err := svc().UpdateOrder(id, body.OrderInput, service.ConfirmInput{AllowNegative: body.AllowNegative, NegativeNote: body.NegativeNote}, userID(c))
	if err != nil {
		if handled, e := stockError(c, err); handled {
			return e
		}
		return fail(c, err)
	}
	audit(c, "equip_order_updated", "equip_order", id, body.OrderInput)
	trimOrder(c, res.Order)
	return c.JSON(fiber.Map{"data": res.Order, "warnings": res.Warnings})
}

func (h *Handler) ConfirmOrder(c fiber.Ctx) error {
	id, ok := idParam(c, "id")
	if !ok {
		return nil
	}
	var in service.ConfirmInput
	_ = c.Bind().JSON(&in)
	res, err := svc().ConfirmOrder(id, in, userID(c))
	if err != nil {
		if handled, e := stockError(c, err); handled {
			return e
		}
		return fail(c, err)
	}
	audit(c, "equip_order_confirmed", "equip_order", id, in)
	trimOrder(c, res.Order)
	return c.JSON(fiber.Map{"data": res.Order, "warnings": res.Warnings})
}

type notesBody struct {
	Notes  string `json:"notes"`
	Reason string `json:"reason"`
}

func (h *Handler) CancelOrder(c fiber.Ctx) error {
	id, ok := idParam(c, "id")
	if !ok {
		return nil
	}
	var b notesBody
	_ = c.Bind().JSON(&b)
	v, err := svc().CancelOrder(id, b.Reason, userID(c))
	if err != nil {
		return fail(c, err)
	}
	audit(c, "equip_order_cancelled", "equip_order", id, b)
	trimOrder(c, v)
	return c.JSON(fiber.Map{"data": v})
}

func (h *Handler) ValidateOrder(c fiber.Ctx) error {
	id, ok := idParam(c, "id")
	if !ok {
		return nil
	}
	var b notesBody
	_ = c.Bind().JSON(&b)
	v, err := svc().ValidateOrder(id, b.Notes, userID(c))
	if err != nil {
		return fail(c, err)
	}
	audit(c, "equip_order_validated", "equip_order", id, b)
	trimOrder(c, v)
	return c.JSON(fiber.Map{"data": v})
}

func (h *Handler) ObserveOrder(c fiber.Ctx) error {
	id, ok := idParam(c, "id")
	if !ok {
		return nil
	}
	var b notesBody
	_ = c.Bind().JSON(&b)
	v, err := svc().ObserveOrder(id, b.Notes, userID(c))
	if err != nil {
		return fail(c, err)
	}
	audit(c, "equip_order_observed", "equip_order", id, b)
	trimOrder(c, v)
	return c.JSON(fiber.Map{"data": v})
}

func (h *Handler) SetNoPayment(c fiber.Ctx) error {
	id, ok := idParam(c, "id")
	if !ok {
		return nil
	}
	var b struct {
		On bool `json:"on"`
	}
	_ = c.Bind().JSON(&b)
	v, err := svc().SetNoPayment(id, b.On)
	if err != nil {
		return fail(c, err)
	}
	audit(c, "equip_order_no_payment", "equip_order", id, b)
	trimOrder(c, v)
	return c.JSON(fiber.Map{"data": v})
}

// ── Envíos ─────────────────────────────────────────────────────────────────

func (h *Handler) ListShipments(c fiber.Ctx) error {
	carrier, _ := strconv.ParseUint(c.Query("carrier_id"), 10, 32)
	rows, err := svc().ListShipments(service.ShipmentFilter{Status: c.Query("status"), CarrierID: uint(carrier), Q: c.Query("q")})
	if err != nil {
		return fail(c, err)
	}
	if !hasPerm(c, "equipos.payments_view") {
		for i := range rows {
			rows[i].BalanceAmount, rows[i].TotalAmount = 0, 0
		}
	}
	return c.JSON(fiber.Map{"data": rows})
}

func (h *Handler) UpdateShipment(c fiber.Ctx) error {
	id, ok := idParam(c, "id")
	if !ok {
		return nil
	}
	var in service.ShipmentInput
	if err := c.Bind().JSON(&in); err != nil {
		return badBody(c)
	}
	res, err := svc().UpdateShipment(id, in)
	if err != nil {
		return fail(c, err)
	}
	audit(c, "equip_shipment_updated", "equip_order", id, in)
	trimOrder(c, res.Order)
	return c.JSON(fiber.Map{"data": res.Order, "warnings": res.Warnings})
}

func shipmentAction(name string, fn func(*service.Service, uint, string) (*service.OrderResult, error)) fiber.Handler {
	return func(c fiber.Ctx) error {
		id, ok := idParam(c, "id")
		if !ok {
			return nil
		}
		var body struct {
			Date string `json:"date"`
		}
		_ = c.Bind().JSON(&body)
		res, err := fn(svc(), id, body.Date)
		if err != nil {
			return fail(c, err)
		}
		audit(c, name, "equip_order", id, body)
		trimOrder(c, res.Order)
		return c.JSON(fiber.Map{"data": res.Order, "warnings": res.Warnings})
	}
}

var (
	DispatchOrder = shipmentAction("equip_shipment_dispatched", (*service.Service).DispatchOrder)
	ArrivedOrder  = shipmentAction("equip_shipment_arrived", (*service.Service).MarkArrived)
	PickedUpOrder = shipmentAction("equip_shipment_picked_up", (*service.Service).MarkPickedUp)
)

func (h *Handler) LabelPrinted(c fiber.Ctx) error {
	id, ok := idParam(c, "id")
	if !ok {
		return nil
	}
	if err := svc().MarkLabelPrinted(id); err != nil {
		return fail(c, err)
	}
	return c.JSON(fiber.Map{"success": true})
}

// ── Cobros ─────────────────────────────────────────────────────────────────

func (h *Handler) ListPayments(c fiber.Ctx) error {
	page, _ := strconv.Atoi(c.Query("page"))
	per, _ := strconv.Atoi(c.Query("per_page"))
	cust, _ := strconv.ParseUint(c.Query("customer_id"), 10, 32)
	res, err := svc().ListPayments(service.PaymentFilter{CustomerID: uint(cust), From: c.Query("from"), To: c.Query("to"), Method: c.Query("method"), Status: c.Query("status"), Q: c.Query("q"), Page: page, PerPage: per})
	if err != nil {
		return fail(c, err)
	}
	return c.JSON(fiber.Map{"data": res})
}

func (h *Handler) GetPayment(c fiber.Ctx) error {
	id, ok := idParam(c, "id")
	if !ok {
		return nil
	}
	v, err := svc().GetPayment(id)
	if err != nil {
		return fail(c, err)
	}
	return c.JSON(fiber.Map{"data": v})
}

func (h *Handler) CreatePayment(c fiber.Ctx) error {
	var in service.PaymentInput
	if err := c.Bind().JSON(&in); err != nil {
		return badBody(c)
	}
	v, err := svc().CreatePayment(in, userID(c))
	if err != nil {
		return fail(c, err)
	}
	audit(c, "equip_payment_created", "equip_payment", v.ID, in)
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"data": v})
}

func (h *Handler) VoidPayment(c fiber.Ctx) error {
	id, ok := idParam(c, "id")
	if !ok {
		return nil
	}
	var b notesBody
	_ = c.Bind().JSON(&b)
	v, err := svc().VoidPayment(id, b.Reason)
	if err != nil {
		return fail(c, err)
	}
	audit(c, "equip_payment_voided", "equip_payment", id, b)
	return c.JSON(fiber.Map{"data": v})
}

func (h *Handler) AllocatePayment(c fiber.Ctx) error {
	id, ok := idParam(c, "id")
	if !ok {
		return nil
	}
	var b struct {
		Allocations []service.PaymentAllocationInput `json:"allocations"`
	}
	if err := c.Bind().JSON(&b); err != nil {
		return badBody(c)
	}
	v, err := svc().AllocateCredit(id, b.Allocations)
	if err != nil {
		return fail(c, err)
	}
	audit(c, "equip_payment_allocated", "equip_payment", id, b)
	return c.JSON(fiber.Map{"data": v})
}
