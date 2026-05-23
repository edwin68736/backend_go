package handler

import (
	"strconv"

	"tukifac/internal/subscriptions/service"
	"tukifac/pkg/saas"

	"github.com/gofiber/fiber/v3"
)

type SubscriptionHandler struct {
	svc *service.SubscriptionService
}

func NewSubscriptionHandler() *SubscriptionHandler {
	return &SubscriptionHandler{svc: service.NewSubscriptionService()}
}

// GET /api/superadmin/subscriptions?status=
func (h *SubscriptionHandler) ListAPI(c fiber.Ctx) error {
	subs, err := h.svc.List(c.Query("status"))
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"data": subs})
}

// GET /api/superadmin/tenants/:id/subscription
func (h *SubscriptionHandler) GetByTenantAPI(c fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 32)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "ID inválido"})
	}
	sub, err := h.svc.GetByTenant(uint(id))
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(sub)
}

// POST /api/superadmin/subscriptions
func (h *SubscriptionHandler) CreateAPI(c fiber.Ctx) error {
	var input service.CreateSubscriptionInput
	if err := c.Bind().JSON(&input); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "JSON inválido"})
	}
	sub, err := h.svc.Create(input)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"success": true, "data": sub})
}

// PATCH /api/superadmin/subscriptions/:id/suspend
func (h *SubscriptionHandler) SuspendAPI(c fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 32)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "ID inválido"})
	}
	var body struct {
		Reason string `json:"reason"`
	}
	c.Bind().JSON(&body)
	if err := h.svc.Suspend(uint(id), body.Reason); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"success": true})
}

// PATCH /api/superadmin/subscriptions/:id/reactivate
func (h *SubscriptionHandler) ReactivateAPI(c fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 32)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "ID inválido"})
	}
	var body struct {
		ExtraMonths int `json:"extra_months"`
	}
	c.Bind().JSON(&body)
	if err := h.svc.Reactivate(uint(id), body.ExtraMonths); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"success": true})
}

// POST /api/superadmin/cron/check-expirations
func (h *SubscriptionHandler) CheckExpirationsAPI(c fiber.Ctx) error {
	r, u, s := saas.RunDailyJobs()
	return c.JSON(fiber.Map{
		"reminders": r, "status_updates": u, "suspended": s,
		"message": "verificación completada",
	})
}
