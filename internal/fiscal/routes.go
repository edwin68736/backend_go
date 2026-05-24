package fiscal

import (
	"tukifac/internal/fiscal/handler"

	"github.com/gofiber/fiber/v3"
)

func RegisterInternalRoutes(app *fiber.App) {
	h := handler.NewStatusHandler()
	app.Post("/api/internal/fiscal/status", h.PostStatus)
}

// RegisterTenantRoutes expone BFF fiscal al panel tenant (scoped por JWT).
func RegisterTenantRoutes(api fiber.Router) {
	h := handler.NewTenantFiscalHandler()
	f := api.Group("/fiscal")
	f.Get("/stats", h.StatsAPI)
	f.Get("/documents", h.ListDocumentsAPI)
	f.Get("/documents/:uuid/download/:type", h.DownloadAPI)
	f.Post("/documents/bulk/:action", h.BulkActionAPI)
	f.Get("/documents/:uuid", h.DocumentDetailAPI)
	f.Post("/documents/:uuid/:action", h.DocumentActionAPI)
}
