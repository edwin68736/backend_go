package tenantportal

import (
	"tukifac/internal/tenantportal/handler"

	"github.com/gofiber/fiber/v3"
)

// RegisterRoutes rutas de suscripción/pagos del tenant (BD central, desacoplado del ERP).
func RegisterRoutes(api fiber.Router) {
	h := handler.New()
	g := api.Group("/subscription")
	g.Get("/summary", h.Summary)
	g.Get("/invoices", h.ListInvoices)
	g.Get("/payments", h.ListPayments)
	g.Get("/events", h.ListEvents)
	g.Post("/payments", h.SubmitPayment)
	g.Get("/document-packages", h.ListDocumentPackages)
	g.Post("/document-packages/purchase", h.PurchaseDocumentPackage)
}
