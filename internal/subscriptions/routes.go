package subscriptions

import (
	"tukifac/internal/subscriptions/handler"

	"github.com/gofiber/fiber/v3"
)

func RegisterRoutes(saAPI fiber.Router) {
	h := handler.NewSubscriptionHandler()

	saAPI.Get("/subscriptions", h.ListAPI)
	saAPI.Post("/subscriptions", h.CreateAPI)
	saAPI.Patch("/subscriptions/:id/suspend", h.SuspendAPI)
	saAPI.Patch("/subscriptions/:id/reactivate", h.ReactivateAPI)
	saAPI.Get("/tenants/:id/subscription", h.GetByTenantAPI)
	saAPI.Post("/cron/check-expirations", h.CheckExpirationsAPI)
}
