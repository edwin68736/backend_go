package ajustes

import (
	"tukifac/internal/ajustes/handler"

	"github.com/gofiber/fiber/v3"
)

func RegisterRoutes(saAPI fiber.Router) {
	h := handler.NewAjusteHandler()
	saAPI.Get("/ajustes", h.GetAPI)
	saAPI.Put("/ajustes", h.UpdateAPI)
}
