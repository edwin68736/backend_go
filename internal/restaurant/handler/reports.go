package handler

// F6a: reportes de insumos y platos (solo lectura). Siempre de una sucursal; los costos solo viajan con permiso de verlos.

import (
	"errors"
	"strconv"
	"time"

	invsvc "tukifac/internal/inventory/service"
	"tukifac/internal/restaurant/service"
	"tukifac/pkg/middleware"

	"github.com/gofiber/fiber/v3"
)

const (
	reportMaxDays    = 366
	reportPageSize   = 50
	reportMaxPerPage = 1000
)

// RequireReportData: la BD del tenant ya guarda costos de insumos.
func (h *RestaurantHandler) RequireReportData(c fiber.Ctx) error {
	if gdb := db(c); gdb != nil && invsvc.SupplyCostsReady(gdb) {
		return c.Next()
	}
	return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Los reportes aún no están disponibles", "code": "reports_unavailable"})
}

// reportPeriod lee from y to (AAAA-MM-DD), ambos obligatorios y de no más de un año.
func reportPeriod(c fiber.Ctx) (service.ReportPeriod, error) {
	from, errFrom := time.ParseInLocation("2006-01-02", c.Query("from"), time.Local)
	to, errTo := time.ParseInLocation("2006-01-02", c.Query("to"), time.Local)
	if errFrom != nil || errTo != nil {
		return service.ReportPeriod{}, errors.New("indica las fechas desde y hasta (AAAA-MM-DD)")
	}
	if to.Before(from) {
		return service.ReportPeriod{}, errors.New("la fecha final es anterior a la inicial")
	}
	if to.Sub(from) > reportMaxDays*24*time.Hour {
		return service.ReportPeriod{}, errors.New("el período no puede pasar de un año")
	}
	return service.ReportPeriod{From: from, To: to.Add(24*time.Hour - time.Nanosecond)}, nil
}

func reportPage(c fiber.Ctx) (page, perPage int) {
	page, _ = strconv.Atoi(c.Query("page"))
	perPage, _ = strconv.Atoi(c.Query("per_page"))
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = reportPageSize
	}
	if perPage > reportMaxPerPage {
		perPage = reportMaxPerPage
	}
	return page, perPage
}

func reportSupply(c fiber.Ctx) uint {
	id, _ := strconv.ParseUint(c.Query("supply_id"), 10, 32)
	return uint(id)
}

func reportError(c fiber.Ctx, err error) error {
	return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
}

func reportBadRequest(c fiber.Ctx, err error) error {
	return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
}

func (h *RestaurantHandler) SupplyStockReport(c fiber.Ctx) error {
	branchID, withCost := recipeBranch(c), middleware.CanViewSupplyCost(c)
	rows, summary, err := svc(c).SupplyStockReport(branchID, withCost)
	if err != nil {
		return reportError(c, err)
	}
	return c.JSON(fiber.Map{"data": rows, "summary": summary, "branch_id": branchID, "can_view_cost": withCost})
}

func (h *RestaurantHandler) SupplyPurchasesReport(c fiber.Ctx) error {
	period, err := reportPeriod(c)
	if err != nil {
		return reportBadRequest(c, err)
	}
	branchID, withCost := recipeBranch(c), middleware.CanViewSupplyCost(c)
	page, perPage := reportPage(c)
	rows, total, summary, err := svc(c).SupplyPurchasesReport(branchID, reportSupply(c), period, page, perPage, withCost)
	if err != nil {
		return reportError(c, err)
	}
	return c.JSON(fiber.Map{"data": rows, "total": total, "summary": summary, "branch_id": branchID, "can_view_cost": withCost})
}

func (h *RestaurantHandler) SupplyConsumptionReport(c fiber.Ctx) error {
	period, err := reportPeriod(c)
	if err != nil {
		return reportBadRequest(c, err)
	}
	branchID, withCost := recipeBranch(c), middleware.CanViewSupplyCost(c)
	rows, summary, err := svc(c).SupplyConsumptionReport(branchID, period, withCost)
	if err != nil {
		return reportError(c, err)
	}
	return c.JSON(fiber.Map{"data": rows, "summary": summary, "branch_id": branchID, "can_view_cost": withCost})
}

func (h *RestaurantHandler) SupplyWasteReport(c fiber.Ctx) error {
	period, err := reportPeriod(c)
	if err != nil {
		return reportBadRequest(c, err)
	}
	branchID, withCost := recipeBranch(c), middleware.CanViewSupplyCost(c)
	page, perPage := reportPage(c)
	rows, total, summary, err := svc(c).SupplyWasteReport(branchID, reportSupply(c), period, page, perPage, withCost)
	if err != nil {
		return reportError(c, err)
	}
	return c.JSON(fiber.Map{"data": rows, "total": total, "summary": summary, "branch_id": branchID, "can_view_cost": withCost})
}

func (h *RestaurantHandler) DishProfitReport(c fiber.Ctx) error {
	period, err := reportPeriod(c)
	if err != nil {
		return reportBadRequest(c, err)
	}
	branchID := recipeBranch(c)
	rows, summary, err := svc(c).DishProfitReport(branchID, period)
	if err != nil {
		return reportError(c, err)
	}
	return c.JSON(fiber.Map{"data": rows, "summary": summary, "branch_id": branchID})
}
