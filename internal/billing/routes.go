package billing

import (
	"tukifac/internal/billing/handler"
	"tukifac/pkg/middleware"

	"github.com/gofiber/fiber/v3"
)

// RegisterRoutes registra las rutas de facturación electrónica. Antes solo exigían el módulo del
// plan ("billing"), nunca el rol del usuario: cualquier usuario autenticado del tenant podía
// enviar a SUNAT, anular con nota de crédito, emitir notas/guías/retenciones/percepciones, sin
// que el permiso "billing.send" (el único que existe hoy en el catálogo para este módulo,
// ver internal/users/service/role_service.go) tuviera ningún efecto. Se agrega RequirePermission
// a todas las rutas salvo /reissue, que sigue reservada a soporte (RequireMasterAccess).
func RegisterRoutes(api fiber.Router) {
	h := handler.NewBillingHandler()
	mod := middleware.RequireModule("billing")
	perm := middleware.RequirePermission("billing.send")

	api.Post("/billing/send/:saleId", mod, perm, h.SendToSUNAT)
	api.Get("/billing/status/:saleId", mod, perm, h.GetBillingStatus)
	api.Get("/billing/events", mod, perm, handler.SSEAccessTokenMiddleware, h.BillingEventsSSE)
	api.Get("/billing/job/:saleId", mod, perm, h.GetBillingJobStatus)
	api.Post("/billing/resend/:saleId", mod, perm, h.ResendToSUNAT)
	// Corrección fiscal: reenvía con otra fecha de emisión aunque el comprobante
	// ya tenga aceptación. Reservada a soporte por acceso maestro (auditable).
	api.Post("/billing/reissue/:saleId",
		mod,
		middleware.RequireMasterAccess(),
		h.ReissueToSUNAT,
	)
	api.Post("/billing/void-with-credit-note/:saleId", mod, perm, h.VoidWithCreditNoteAPI)
	api.Post("/billing/debit-notes/:saleId", mod, perm, h.CreateDebitNoteAPI)
	// Nota de crédito/débito independiente (Fase 3): sin venta local, documento afectado a mano.
	api.Post("/billing/notes/independent", mod, perm, h.CreateIndependentNoteAPI)
	api.Get("/billing/invoice/:saleId", mod, perm, h.GetInvoiceAPI)
	api.Get("/billing/invoice/:saleId/document/:kind", mod, perm, h.GetInvoiceDocumentAPI)
	// Resúmenes diarios y comunicaciones de baja
	api.Get("/billing/summaries", mod, perm, h.ListSummariesAPI)
	api.Post("/billing/summaries", mod, perm, h.CreateSummaryAPI)
	api.Get("/billing/summaries/:id/status", mod, perm, h.GetSummaryStatusAPI)
	api.Get("/billing/voided", mod, perm, h.ListVoidedAPI)
	api.Post("/billing/voided", mod, perm, h.CreateVoidedAPI)
	api.Get("/billing/voided/:id/status", mod, perm, h.GetVoidedStatusAPI)
	api.Get("/billing/notification-counts", mod, perm, h.NotificationCountsAPI)
	api.Get("/billing/invoice-status", mod, perm, h.ConsultInvoiceStatusAPI)
	// Guías de remisión, retención, percepción, reversión
	api.Get("/billing/despatches", mod, perm, h.ListDespatchesAPI)
	api.Post("/billing/despatches", mod, perm, h.CreateDespatchAPI)
	api.Get("/billing/despatches/:id/status", mod, perm, h.GetDespatchStatusAPI)
	api.Get("/billing/retentions", mod, perm, h.ListRetentionsAPI)
	api.Post("/billing/retentions", mod, perm, h.CreateRetentionAPI)
	api.Get("/billing/retentions/:id/status", mod, perm, h.GetRetentionStatusAPI)
	api.Get("/billing/perceptions", mod, perm, h.ListPerceptionsAPI)
	api.Post("/billing/perceptions", mod, perm, h.CreatePerceptionAPI)
	api.Get("/billing/perceptions/:id/status", mod, perm, h.GetPerceptionStatusAPI)
	api.Get("/billing/reversions", mod, perm, h.ListReversionsAPI)
	api.Post("/billing/reversions", mod, perm, h.CreateReversionAPI)
	api.Get("/billing/reversions/:id/status", mod, perm, h.GetReversionStatusAPI)
}
