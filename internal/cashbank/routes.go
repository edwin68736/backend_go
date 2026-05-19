package cashbank

import (
	"tukifac/internal/cashbank/handler"
	"tukifac/pkg/middleware"

	"github.com/gofiber/fiber/v3"
)

func RegisterRoutes(api fiber.Router) {
	h := handler.NewCashBankHandler()
	api.Get("/cashbank/sessions", middleware.RequireModule("cashbank"), middleware.RequirePermission("cashbank.view"), h.ListSessionsAPI)
	api.Get("/cashbank/sessions/open", middleware.RequireModule("cashbank"), middleware.RequirePermission("cashbank.view"), h.GetOpenSessionAPI)
	api.Get("/cashbank/sessions/:id", middleware.RequireModule("cashbank"), middleware.RequirePermission("cashbank.view"), h.GetSessionAPI)
	api.Post("/cashbank/sessions", middleware.RequireModule("cashbank"), middleware.RequirePermission("cashbank.open"), h.OpenSessionAPI)
	api.Post("/cashbank/sessions/:id/close", middleware.RequireModule("cashbank"), middleware.RequirePermission("cashbank.close"), h.CloseSessionAPI)
	api.Post("/cashbank/sessions/:id/arqueo", middleware.RequireModule("cashbank"), middleware.RequirePermission("cashbank.view"), h.SaveArqueoAPI)
	api.Get("/cashbank/sessions/:id/movements", middleware.RequireModule("cashbank"), middleware.RequirePermission("cashbank.view"), h.GetMovementsAPI)
	api.Post("/cashbank/sessions/:id/movements", middleware.RequireModule("cashbank"), middleware.RequirePermission("cashbank.movements"), h.AddMovementAPI)
	api.Get("/cashbank/sessions/:id/report", middleware.RequireModule("cashbank"), middleware.RequirePermission("reports.view"), h.GetSessionReportAPI)
	api.Get("/cashbank/reports/movements", middleware.RequireModule("cashbank"), middleware.RequirePermission("reports.view"), h.ListMovementsReportAPI)
	// Lectura del catálogo (filtros en reportes, POS, etc.): alinear con ver ventas, no con crear.
	api.Get("/cashbank/payment-methods", middleware.RequireModule("sales"), middleware.RequirePermission("sales.view"), h.ListPaymentMethodsAPI)
	api.Get("/cashbank/payment-methods/:id", middleware.RequireModule("cashbank"), middleware.RequirePermission("cashbank.manage"), h.GetPaymentMethodAPI)
	api.Post("/cashbank/payment-methods", middleware.RequireModule("cashbank"), middleware.RequirePermission("cashbank.manage"), h.CreatePaymentMethodAPI)
	api.Put("/cashbank/payment-methods/:id", middleware.RequireModule("cashbank"), middleware.RequirePermission("cashbank.manage"), h.UpdatePaymentMethodAPI)
	api.Delete("/cashbank/payment-methods/:id", middleware.RequireModule("cashbank"), middleware.RequirePermission("cashbank.manage"), h.DeletePaymentMethodAPI)
	api.Get("/cashbank/bank-accounts", middleware.RequireModule("cashbank"), middleware.RequirePermission("cashbank.view"), h.ListBankAccountsAPI)
	api.Get("/cashbank/bank-accounts/:id", middleware.RequireModule("cashbank"), middleware.RequirePermission("cashbank.view"), h.GetBankAccountAPI)
	api.Post("/cashbank/bank-accounts", middleware.RequireModule("cashbank"), middleware.RequirePermission("cashbank.manage"), h.CreateBankAccountAPI)
	api.Put("/cashbank/bank-accounts/:id", middleware.RequireModule("cashbank"), middleware.RequirePermission("cashbank.manage"), h.UpdateBankAccountAPI)
	api.Get("/cashbank/bank-accounts/:id/movements", middleware.RequireModule("cashbank"), middleware.RequirePermission("cashbank.view"), h.GetBankMovementsAPI)
	api.Post("/cashbank/bank-accounts/:id/movements", middleware.RequireModule("cashbank"), middleware.RequirePermission("cashbank.manage"), h.AddBankMovementAPI)
}
