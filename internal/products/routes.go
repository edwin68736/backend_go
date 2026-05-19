package products

import (
	"tukifac/internal/products/handler"
	"tukifac/pkg/middleware"

	"github.com/gofiber/fiber/v3"
)

func RegisterRoutes(api fiber.Router) {
	h := handler.NewProductHandler()
	api.Get("/products", middleware.RequireModule("products"), middleware.RequirePermission("products.view"), h.SearchAPI)
	api.Get("/products/:id/serials", middleware.RequireModule("products"), middleware.RequirePermission("products.view"), h.ProductSerialsAPI)
	api.Get("/products/:id", middleware.RequireModule("products"), middleware.RequirePermission("products.view"), h.GetAPI)
	api.Post("/products", middleware.RequireModule("products"), middleware.RequirePermission("products.create"), h.CreateAPI)
	api.Put("/products/:id", middleware.RequireModule("products"), middleware.RequirePermission("products.edit"), h.UpdateAPI)
	api.Patch("/products/:id/toggle", middleware.RequireModule("products"), middleware.RequirePermission("products.edit"), h.ToggleAPI)
	api.Delete("/products/:id", middleware.RequireModule("products"), middleware.RequirePermission("products.delete"), h.DeleteAPI)
	api.Post("/products/:id/image", middleware.RequireModule("products"), middleware.RequirePermission("products.edit"), h.UploadImageAPI)
	api.Get("/categories", middleware.RequireModule("products"), middleware.RequirePermission("products.view"), h.CategoryListAPI)
	api.Post("/categories", middleware.RequireModule("products"), middleware.RequirePermission("products.create"), h.CategoryCreateAPI)
	api.Get("/modifier-groups", middleware.RequireModule("products"), middleware.RequirePermission("products.view"), h.ModifierGroupsAPI)
	api.Post("/modifier-groups", middleware.RequireModule("products"), middleware.RequirePermission("products.create"), h.ModifierGroupCreateAPI)
}
