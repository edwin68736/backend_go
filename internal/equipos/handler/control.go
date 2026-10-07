package handler

import (
	"strconv"

	"tukifac/internal/equipos/service"

	"github.com/gofiber/fiber/v3"
)

// ── Retornos ───────────────────────────────────────────────────────────────

func (h *Handler) ListReturns(c fiber.Ctx) error {
	rows, err := svc().ListReturns(c.Query("status"))
	if err != nil {
		return fail(c, err)
	}
	return c.JSON(fiber.Map{"data": rows})
}

func (h *Handler) CreateReturn(c fiber.Ctx) error {
	var in service.ReturnInput
	if err := c.Bind().JSON(&in); err != nil {
		return badBody(c)
	}
	v, err := svc().CreateReturn(in, userID(c))
	if err != nil {
		return fail(c, err)
	}
	audit(c, "equip_return_created", "equip_return", v.ID, in)
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"data": v})
}

func (h *Handler) UpdateReturn(c fiber.Ctx) error {
	id, ok := idParam(c, "id")
	if !ok {
		return nil
	}
	var in service.ReturnUpdate
	if err := c.Bind().JSON(&in); err != nil {
		return badBody(c)
	}
	v, err := svc().UpdateReturn(id, in, userID(c))
	if err != nil {
		return fail(c, err)
	}
	audit(c, "equip_return_updated", "equip_return", id, in)
	return c.JSON(fiber.Map{"data": v})
}

func (h *Handler) ReshipReturn(c fiber.Ctx) error {
	id, ok := idParam(c, "id")
	if !ok {
		return nil
	}
	res, err := svc().Reship(id, userID(c))
	if err != nil {
		return fail(c, err)
	}
	audit(c, "equip_return_reshipped", "equip_return", id, nil)
	trimOrder(c, res.Order)
	return c.JSON(fiber.Map{"data": res.Order})
}

// ── Panel y alertas ────────────────────────────────────────────────────────

func (h *Handler) Dashboard(c fiber.Ctx) error {
	d, err := svc().Dashboard(c.Query("period"), canViewStock(c))
	if err != nil {
		return fail(c, err)
	}
	if !hasPerm(c, "equipos.payments_view") {
		d.SalesTotal, d.Collected, d.Receivable, d.ReturnUnpaid = 0, 0, 0, 0
	}
	return c.JSON(fiber.Map{"data": d, "money_visible": hasPerm(c, "equipos.payments_view")})
}

func (h *Handler) Alerts(c fiber.Ctx) error {
	rows, err := svc().Alerts()
	if err != nil {
		return fail(c, err)
	}
	if !hasPerm(c, "equipos.payments_view") {
		for i := range rows {
			rows[i].Balance = 0
		}
	}
	return c.JSON(fiber.Map{"data": rows})
}

// ── Reportes y cierre ──────────────────────────────────────────────────────

func (h *Handler) ReportSummary(c fiber.Ctx) error {
	v, err := svc().Summary(c.Query("period"))
	if err != nil {
		return fail(c, err)
	}
	return c.JSON(fiber.Map{"data": v})
}

func (h *Handler) ReportSales(c fiber.Ctx) error {
	v, err := svc().SalesByProduct(c.Query("period"))
	if err != nil {
		return fail(c, err)
	}
	return c.JSON(fiber.Map{"data": v})
}

func (h *Handler) ReportCollections(c fiber.Ctx) error {
	v, err := svc().Collections()
	if err != nil {
		return fail(c, err)
	}
	return c.JSON(fiber.Map{"data": v})
}

func (h *Handler) ReportReplenishment(c fiber.Ctx) error {
	v, err := svc().Replenishment()
	if err != nil {
		return fail(c, err)
	}
	return c.JSON(fiber.Map{"data": v})
}

func (h *Handler) ReportProfit(c fiber.Ctx) error {
	v, err := svc().Profitability(c.Query("period"))
	if err != nil {
		return fail(c, err)
	}
	return c.JSON(fiber.Map{"data": v})
}

func (h *Handler) ClosedPeriods(c fiber.Ctx) error {
	v, err := svc().ClosedPeriods()
	if err != nil {
		return fail(c, err)
	}
	return c.JSON(fiber.Map{"data": v})
}

func (h *Handler) ClosePeriod(c fiber.Ctx) error {
	if !requirePin(c) {
		return nil
	}
	period := c.Params("period")
	v, err := svc().ClosePeriod(period, userID(c))
	if err != nil {
		return fail(c, err)
	}
	audit(c, "equip_period_closed", "equip_period", 0, fiber.Map{"period": period})
	return c.JSON(fiber.Map{"data": v})
}

func (h *Handler) ReopenPeriod(c fiber.Ctx) error {
	if !requirePin(c) {
		return nil
	}
	period := c.Params("period")
	if err := svc().ReopenPeriod(period); err != nil {
		return fail(c, err)
	}
	audit(c, "equip_period_reopened", "equip_period", 0, fiber.Map{"period": period})
	return c.JSON(fiber.Map{"success": true})
}

var _ = strconv.Itoa
