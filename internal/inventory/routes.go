package inventory

import (
	"tukifac/internal/inventory/handler"
	"tukifac/pkg/middleware"

	"github.com/gofiber/fiber/v3"
)

// RegisterRoutes registra las rutas de inventario. Antes solo exigían el módulo del plan
// ("inventory"), nunca el rol del usuario — el catálogo ya define inventory.{view,manage} pero no
// se validaba en ningún endpoint. Se agrega RequirePermission: lectura con inventory.view,
// operaciones que mueven stock con inventory.manage (que ya implica inventory.view, ver
// pkg/middleware/tenant_permissions.go).
func RegisterRoutes(api fiber.Router) {
	h := handler.NewInventoryHandler()
	mod := middleware.RequireModule("inventory")
	view := middleware.RequirePermission("inventory.view")
	manage := middleware.RequirePermission("inventory.manage")

	api.Get("/inventory/operation-types", mod, view, h.OperationTypesAPI)
	api.Get("/inventory/documents", mod, view, h.DocumentsListAPI)
	api.Get("/inventory/documents/:id", mod, view, h.DocumentGetAPI)
	api.Post("/inventory/documents", mod, manage, h.DocumentCreateAPI)
	api.Put("/inventory/documents/:id", mod, manage, h.DocumentUpdateAPI)
	api.Post("/inventory/documents/:id/confirm", mod, manage, h.DocumentConfirmAPI)
	api.Post("/inventory/documents/:id/void", mod, manage, h.DocumentVoidAPI)
	api.Get("/inventory/stock-summary", mod, view, h.StockSummaryAPI)
	api.Get("/inventory/stock/:productId", mod, view, h.StockAPI)
	api.Get("/inventory/movements", mod, view, h.MovementsAPI)
	api.Get("/inventory/transfers", mod, view, h.TransfersListAPI)
	api.Post("/inventory/transfer", mod, manage, h.TransferAPI)
	api.Post("/inventory/adjustment", mod, manage, h.AdjustmentAPI)
	api.Post("/inventory/import-adjustment/preview", mod, manage, h.ImportAdjustmentPreviewAPI)
	api.Post("/inventory/import-adjustment/confirm", mod, manage, h.ImportAdjustmentConfirmAPI)
	api.Post("/inventory/transfers/:id/reverse", mod, manage, h.TransferReverseAPI)
	api.Post("/inventory/transfers/:id/confirm", mod, manage, h.TransferConfirmAPI)
	api.Post("/inventory/transfers/:id/cancel", mod, manage, h.TransferCancelAPI)
}
