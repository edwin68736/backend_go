package handler

import (
	"tukifac/internal/ajustes/service"

	"github.com/gofiber/fiber/v3"
)

type AjusteHandler struct {
	svc *service.AjusteService
}

func NewAjusteHandler() *AjusteHandler {
	return &AjusteHandler{svc: service.NewAjusteService()}
}

// GET /api/superadmin/ajustes — devuelve la configuración central (token_consulta no se expone en JSON).
func (h *AjusteHandler) GetAPI(c fiber.Ctx) error {
	a, err := h.svc.Get()
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"data": a})
}

// PUT /api/superadmin/ajustes — actualiza la configuración central.
func (h *AjusteHandler) UpdateAPI(c fiber.Ctx) error {
	var input service.UpdateAjusteInput
	if err := c.Bind().JSON(&input); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "JSON inválido"})
	}
	if err := h.svc.Update(input); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"success": true})
}
