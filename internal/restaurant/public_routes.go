package restaurant

import (
	"tukifac/internal/restaurant/handler"
	"tukifac/pkg/middleware"

	"github.com/gofiber/fiber/v3"
)

// RegisterPublicRoutes rutas restaurante sin JWT (requieren tenant por slug).
func RegisterPublicRoutes(api fiber.Router) {
	h := handler.New()
	g := api.Group("/restaurant/auth", middleware.RequireTenant())
	g.Get("/config", h.AuthConfig)
	g.Post("/pin", h.PinLogin)
}
