package auth

import (
	"tukifac/internal/auth/handler"

	"github.com/gofiber/fiber/v3"
)

func RegisterRoutes(app *fiber.App) {
	h := handler.NewAuthHandler()
	// Solo API — el frontend React maneja la UI de login
	app.Post("/api/login", h.LoginAPI)
}
