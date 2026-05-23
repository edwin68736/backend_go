package handler

import (
	"strconv"

	"tukifac/pkg/saas"

	"github.com/gofiber/fiber/v3"
)

type SettingsHandler struct{}

func NewSettingsHandler() *SettingsHandler { return &SettingsHandler{} }

func (h *SettingsHandler) GetAPI(c fiber.Ctx) error {
	cfg, err := saas.LoadSettings()
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(cfg)
}

// PUT /api/superadmin/saas-settings/operations-key
func (h *SettingsHandler) SetOperationsKeyAPI(c fiber.Ctx) error {
	var body struct {
		NewOperationsKey     string `json:"new_operations_key"`
		CurrentOperationsKey string `json:"current_operations_key"`
	}
	if err := c.Bind().JSON(&body); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "JSON inválido"})
	}
	if err := saas.SetOperationsKey(body.NewOperationsKey, body.CurrentOperationsKey); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{
		"success":                   true,
		"operations_key_configured": true,
	})
}

func (h *SettingsHandler) PutAPI(c fiber.Ctx) error {
	var body saas.PlatformSettings
	if err := c.Bind().JSON(&body); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "JSON inválido"})
	}
	if err := saas.SaveSettings(body); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"success": true})
}

func (h *SettingsHandler) RunJobsAPI(c fiber.Ctx) error {
	r, n := saas.RunHourlyJobs()
	su, s, oc := saas.RunLimaDailyEvaluation()
	return c.JSON(fiber.Map{
		"success":        true,
		"reminders":      r,
		"notifications":  n,
		"status_updates": su,
		"suspended":      s,
		"overdue_cycles": oc,
	})
}

// POST /api/superadmin/tenants/:id/unblock
func (h *SettingsHandler) UnblockTenantAPI(c fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 32)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "ID inválido"})
	}
	var body struct {
		Reason string `json:"reason"`
	}
	_ = c.Bind().JSON(&body)
	adminID, _ := c.Locals("sa_user_id").(uint)
	if err := saas.UnblockTenant(uint(id), adminID, body.Reason); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"success": true, "message": "Tenant desbloqueado; sigue suspendido hasta pago válido"})
}
