package handler

import (
	"strconv"

	"tukifac/internal/superadmin/service"

	"github.com/gofiber/fiber/v3"
)

type MigrationHandler struct {
	svc *service.MigrationFleetService
}

func NewMigrationHandler() *MigrationHandler {
	return &MigrationHandler{svc: service.NewMigrationFleetService()}
}

// GET /api/superadmin/migrations
func (h *MigrationHandler) ListAPI(c fiber.Ctx) error {
	page, _ := strconv.Atoi(c.Query("page", "1"))
	perPage, _ := strconv.Atoi(c.Query("per_page", "25"))
	curV, _ := strconv.Atoi(c.Query("current_version", "0"))
	tgtV, _ := strconv.Atoi(c.Query("target_version", "0"))

	params := service.MigrationListParams{
		Page:           page,
		PerPage:        perPage,
		Status:         c.Query("status"),
		CurrentVersion: curV,
		TargetVersion:  tgtV,
		Outdated:       c.Query("outdated") == "true" || c.Query("outdated") == "1",
		Failed:         c.Query("failed") == "true" || c.Query("failed") == "1",
		Pending:        c.Query("pending") == "true" || c.Query("pending") == "1",
		TenantSlug:     c.Query("tenant_slug"),
		TenantName:     c.Query("tenant_name"),
	}
	items, total, err := h.svc.List(params)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{
		"data":      items,
		"total":     total,
		"page":      page,
		"per_page":  perPage,
	})
}

// GET /api/superadmin/migrations/summary
func (h *MigrationHandler) SummaryAPI(c fiber.Ctx) error {
	sum, err := h.svc.Summary()
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(sum)
}

func (h *MigrationHandler) tenantID(c fiber.Ctx) (uint, error) {
	id, err := strconv.ParseUint(c.Params("tenantId"), 10, 32)
	return uint(id), err
}

func saUserID(c fiber.Ctx) uint {
	v, _ := c.Locals("sa_user_id").(uint)
	return v
}

// POST /api/superadmin/migrations/:tenantId/retry
func (h *MigrationHandler) RetryAPI(c fiber.Ctx) error {
	tid, err := h.tenantID(c)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "ID inválido"})
	}
	if err := h.svc.Retry(tid, saUserID(c), c.IP()); err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"success": true})
}

// POST /api/superadmin/migrations/:tenantId/migrate
func (h *MigrationHandler) MigrateAPI(c fiber.Ctx) error {
	tid, err := h.tenantID(c)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "ID inválido"})
	}
	if err := h.svc.MigrateOne(tid, saUserID(c), c.IP()); err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"success": true})
}

// POST /api/superadmin/migrations/:tenantId/pause
func (h *MigrationHandler) PauseAPI(c fiber.Ctx) error {
	tid, err := h.tenantID(c)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "ID inválido"})
	}
	if err := h.svc.Pause(tid, saUserID(c), c.IP()); err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"success": true})
}

// POST /api/superadmin/migrations/resume-fleet
func (h *MigrationHandler) ResumeFleetAPI(c fiber.Ctx) error {
	if err := h.svc.ResumeFleet(saUserID(c), c.IP()); err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"success": true})
}

// POST /api/superadmin/migrations/:tenantId/resume
func (h *MigrationHandler) ResumeAPI(c fiber.Ctx) error {
	tid, err := h.tenantID(c)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "ID inválido"})
	}
	if err := h.svc.Resume(tid, saUserID(c), c.IP()); err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"success": true})
}
