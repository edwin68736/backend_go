package sales

import (
	"tukifac/internal/sales/handler"
	"tukifac/pkg/middleware"

	"github.com/gofiber/fiber/v3"
)

func RegisterRoutes(api fiber.Router) {
	h := handler.NewSaleHandler()
	api.Get("/sales", middleware.RequireModule("sales"), middleware.RequirePermission("sales.view"), h.ListAPI)
	api.Get("/sales/by-product", middleware.RequireModule("sales"), middleware.RequirePermission("sales.view"), h.ListByProductAPI)
	api.Post("/sales", middleware.RequireModule("sales"), middleware.RequirePermission("sales.create"), h.CreateAPI)
	api.Post("/sales/:id/issue-electronic", middleware.RequireModule("sales"), middleware.RequireModule("billing"), middleware.RequirePermission("sales.create"), h.IssueElectronicFromNotaAPI)
	api.Get("/sales/:id", middleware.RequireModule("sales"), middleware.RequirePermission("sales.view"), h.GetAPI)
}
