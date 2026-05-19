package memberships

import (
	"tukifac/internal/memberships/handler"
	"tukifac/pkg/middleware"

	"github.com/gofiber/fiber/v3"
)

func RegisterRoutes(api fiber.Router) {
	h := handler.NewMembershipHandler()
	api.Get("/memberships/reminder-counts", middleware.RequireModule("memberships"), middleware.RequirePermission("memberships.view"), h.ReminderCountsAPI)
	api.Get("/memberships", middleware.RequireModule("memberships"), middleware.RequirePermission("memberships.view"), h.ListAPI)
	api.Post("/memberships", middleware.RequireModule("memberships"), middleware.RequirePermission("memberships.create"), h.CreateAPI)
	api.Get("/memberships/:id", middleware.RequireModule("memberships"), middleware.RequirePermission("memberships.view"), h.GetAPI)
	api.Get("/memberships/:id/billing-history", middleware.RequireModule("memberships"), middleware.RequirePermission("memberships.view"), h.BillingHistoryAPI)
	api.Put("/memberships/:id", middleware.RequireModule("memberships"), middleware.RequirePermission("memberships.edit"), h.UpdateAPI)
	api.Patch("/memberships/:id/status", middleware.RequireModule("memberships"), middleware.RequirePermission("memberships.edit"), h.SetStatusAPI)
	api.Delete("/memberships/:id", middleware.RequireModule("memberships"), middleware.RequirePermission("memberships.delete"), h.DeleteAPI)
	api.Post("/memberships/:id/generate-sale", middleware.RequireModule("memberships"), middleware.RequirePermission("memberships.generate_sale"), h.GenerateSaleAPI)
}
