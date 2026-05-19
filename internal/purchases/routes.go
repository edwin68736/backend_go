package purchases

import (
	"tukifac/internal/purchases/handler"
	"tukifac/pkg/middleware"

	"github.com/gofiber/fiber/v3"
)

func RegisterRoutes(api fiber.Router) {
	h := handler.NewPurchaseHandler()
	api.Get("/purchases", middleware.RequireModule("purchases"), middleware.RequirePermission("purchases.view"), h.ListAPI)
	api.Get("/purchases/:id", middleware.RequireModule("purchases"), middleware.RequirePermission("purchases.view"), h.GetAPI)
	api.Post("/purchases", middleware.RequireModule("purchases"), middleware.RequirePermission("purchases.create"), h.CreateAPI)
	api.Post("/purchases/:id/void", middleware.RequireModule("purchases"), middleware.RequirePermission("purchases.delete"), h.VoidAPI)
}
