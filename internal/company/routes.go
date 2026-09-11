package company

import (
	"tukifac/internal/company/handler"
	"tukifac/pkg/middleware"

	"github.com/gofiber/fiber/v3"
)

// RegisterRoutes registra las rutas de configuración de empresa. Antes no tenían NINGÚN chequeo de
// rol (ni siquiera de módulo) — cualquier usuario autenticado del tenant podía leer/editar
// credenciales SUNAT, sucursales y series de comprobantes. El catálogo ya define
// company.{view,edit} (ver internal/users/service/role_service.go); se agrega RequirePermission
// para que sí se exija.
func RegisterRoutes(api fiber.Router) {
	h := handler.NewCompanyHandler()
	view := middleware.RequirePermission("company.view")
	edit := middleware.RequirePermission("company.edit")

	api.Get("/company/config", view, h.GetConfigAPI)
	api.Put("/company/config", edit, h.UpdateConfigAPI)
	api.Put("/company/receipt-wallet", edit, h.UpdateReceiptWalletAPI)
	api.Post("/company/receipt-wallet/qr", edit, h.UploadReceiptWalletQRAPI)
	api.Post("/company/logo", edit, h.UploadCompanyLogoAPI)
	api.Delete("/company/logo", edit, h.DeleteCompanyLogoAPI)
	api.Get("/company/sunat", view, h.GetSunatAPI)
	api.Get("/company/invoicing", view, h.GetInvoicingAPI)
	api.Put("/company/sunat", edit, h.UpdateSunatAPI)
	// POST /company/sync-facturador eliminado: la sincronización con Lycet se hace solo desde el panel central.
	api.Get("/company/branches", view, h.ListBranchesAPI)
	api.Post("/company/branches", edit, h.CreateBranchAPI)
	api.Put("/company/branches/:id", edit, h.UpdateBranchAPI)
	api.Delete("/company/branches/:id", edit, h.DeleteBranchAPI)
	api.Get("/company/series/document-types", view, h.ListSeriesDocumentTypesAPI)
	api.Get("/company/series", view, h.ListSeriesAPI)
	api.Post("/company/series", edit, h.CreateSeriesAPI)
	api.Put("/company/series/:id", edit, h.UpdateSeriesAPI)
	api.Put("/company/series/:id/default", edit, h.SetDefaultSeriesAPI)
	api.Delete("/company/series/:id", edit, h.DeleteSeriesAPI)
}
