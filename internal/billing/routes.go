package billing

import (
	"tukifac/internal/billing/handler"
	"tukifac/pkg/middleware"

	"github.com/gofiber/fiber/v3"
)

func RegisterRoutes(api fiber.Router) {
	h := handler.NewBillingHandler()
	api.Post("/billing/send/:saleId", middleware.RequireModule("billing"), h.SendToSUNAT)
	api.Post("/billing/resend/:saleId", middleware.RequireModule("billing"), h.ResendToSUNAT)
	api.Post("/billing/void-with-credit-note/:saleId", middleware.RequireModule("billing"), h.VoidWithCreditNoteAPI)
	api.Get("/billing/invoice/:saleId", middleware.RequireModule("billing"), h.GetInvoiceAPI)
	api.Get("/billing/invoice/:saleId/document/:kind", middleware.RequireModule("billing"), h.GetInvoiceDocumentAPI)
	// Resúmenes diarios y comunicaciones de baja
	api.Get("/billing/summaries", middleware.RequireModule("billing"), h.ListSummariesAPI)
	api.Post("/billing/summaries", middleware.RequireModule("billing"), h.CreateSummaryAPI)
	api.Get("/billing/summaries/:id/status", middleware.RequireModule("billing"), h.GetSummaryStatusAPI)
	api.Get("/billing/voided", middleware.RequireModule("billing"), h.ListVoidedAPI)
	api.Post("/billing/voided", middleware.RequireModule("billing"), h.CreateVoidedAPI)
	api.Get("/billing/voided/:id/status", middleware.RequireModule("billing"), h.GetVoidedStatusAPI)
	api.Get("/billing/notification-counts", middleware.RequireModule("billing"), h.NotificationCountsAPI)
	api.Get("/billing/invoice-status", middleware.RequireModule("billing"), h.ConsultInvoiceStatusAPI)
	// Guías de remisión, retención, percepción, reversión
	api.Get("/billing/despatches", middleware.RequireModule("billing"), h.ListDespatchesAPI)
	api.Post("/billing/despatches", middleware.RequireModule("billing"), h.CreateDespatchAPI)
	api.Get("/billing/despatches/:id/status", middleware.RequireModule("billing"), h.GetDespatchStatusAPI)
	api.Get("/billing/retentions", middleware.RequireModule("billing"), h.ListRetentionsAPI)
	api.Post("/billing/retentions", middleware.RequireModule("billing"), h.CreateRetentionAPI)
	api.Get("/billing/perceptions", middleware.RequireModule("billing"), h.ListPerceptionsAPI)
	api.Post("/billing/perceptions", middleware.RequireModule("billing"), h.CreatePerceptionAPI)
	api.Get("/billing/reversions", middleware.RequireModule("billing"), h.ListReversionsAPI)
	api.Post("/billing/reversions", middleware.RequireModule("billing"), h.CreateReversionAPI)
	api.Get("/billing/reversions/:id/status", middleware.RequireModule("billing"), h.GetReversionStatusAPI)
}
