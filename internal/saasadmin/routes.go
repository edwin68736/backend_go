package saasadmin

import (
	"tukifac/internal/saasadmin/handler"

	"github.com/gofiber/fiber/v3"
)

func RegisterRoutes(saAPI fiber.Router) {
	h := handler.NewSettingsHandler()
	saAPI.Get("/saas-settings", h.GetAPI)
	saAPI.Put("/saas-settings", h.PutAPI)
	saAPI.Put("/saas-settings/operations-key", h.SetOperationsKeyAPI)
	saAPI.Post("/saas-settings/upload-qr", h.UploadQR)
	saAPI.Post("/cron/saas-jobs", h.RunJobsAPI)
	saAPI.Post("/tenants/:id/unblock", h.UnblockTenantAPI)
}
