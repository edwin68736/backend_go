package ecommerce

import (
	"tukifac/internal/ecommerce/handler"
	"tukifac/pkg/middleware"

	"github.com/gofiber/fiber/v3"
)

// RegisterRoutes rutas de administración (autenticadas, dentro de Tukifac). Cada una exige el
// módulo "ecommerce" habilitado en el plan del tenant — mismo mecanismo que usa "billing" — y,
// ahora, el permiso del rol (ecommerce.{view,manage}, ver
// internal/users/service/role_service.go); antes ninguna ruta lo exigía.
func RegisterRoutes(api fiber.Router) {
	h := handler.NewEcommerceHandler()
	mod := middleware.RequireModule("ecommerce")
	view := middleware.RequirePermission("ecommerce.view")
	manage := middleware.RequirePermission("ecommerce.manage")

	api.Get("/ecommerce/settings", mod, view, h.GetSettingsAPI)
	api.Put("/ecommerce/settings", mod, manage, h.UpdateSettingsAPI)
	api.Post("/ecommerce/settings/logo", mod, manage, h.UploadLogoAPI)
	api.Post("/ecommerce/settings/background", mod, manage, h.UploadBackgroundAPI)

	api.Get("/ecommerce/sliders", mod, view, h.ListSlidersAPI)
	api.Post("/ecommerce/sliders", mod, manage, h.CreateSliderAPI)
	api.Put("/ecommerce/sliders/:id", mod, manage, h.UpdateSliderAPI)
	api.Delete("/ecommerce/sliders/:id", mod, manage, h.DeleteSliderAPI)
	api.Post("/ecommerce/sliders/reorder", mod, manage, h.ReorderSlidersAPI)

	api.Get("/ecommerce/orders", mod, view, h.ListOrdersAPI)
	api.Get("/ecommerce/orders/:id/print-data", mod, view, h.OrderPrintDataAPI)
	api.Put("/ecommerce/orders/:id/status", mod, manage, h.UpdateOrderStatusAPI)
	api.Post("/ecommerce/orders/:id/convert", mod, manage, h.ConvertOrderAPI)
}

// RegisterPublicRoutes rutas de la tienda pública (sin JWT), resueltas por tenant vía
// TenantResolver (subdominio) + RequireEcommerceAvailable (módulo + ajustes + suscripción).
func RegisterPublicRoutes(app fiber.Router) {
	h := handler.NewEcommerceHandler()
	g := app.Group("/public/ecommerce", middleware.RequireTenant(), middleware.RequireEcommerceAvailable())
	g.Get("/settings", h.PublicSettingsAPI)
	g.Get("/categories", h.PublicCategoriesAPI)
	g.Get("/price-bounds", h.PublicPriceBoundsAPI)
	g.Get("/products", h.PublicProductsAPI)
	g.Post("/orders", h.CreatePublicOrderAPI)
}
