package dashboard

import (
	"tukifac/internal/dashboard/handler"

	"github.com/gofiber/fiber/v3"
)

func RegisterRoutes(api fiber.Router) {
	h := handler.NewDashboardHandler()
	api.Get("/dashboard/stats", h.StatsAPI)
	api.Get("/dashboard/analytics", h.AnalyticsAPI)
}
