package modules

import (
	"tukifac/internal/modules/handler"
	"tukifac/pkg/middleware"

	"github.com/gofiber/fiber/v3"
)

func RegisterRoutes(api fiber.Router) {
	h := handler.NewModuleHandler()
	admin := middleware.RequireRole("Administrador")
	api.Post("/modules/:key/toggle", admin, h.ToggleAPI)
	api.Get("/modules/:key/ping", admin, h.PingAPI)
	api.All("/modules/:key/forward/*", h.ForwardAPI)
}
