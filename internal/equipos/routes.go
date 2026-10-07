// Package equipos registra las rutas del módulo «Gestión de Equipos» del panel central.
package equipos

import (
	"tukifac/internal/equipos/handler"
	"tukifac/pkg/middleware"

	"github.com/gofiber/fiber/v3"
)

// RegisterRoutes monta /api/superadmin/equipos/*. saAPI ya viene con SuperAdminAuthAPI(); cada ruta exige su permiso.
func RegisterRoutes(saAPI fiber.Router) {
	h := handler.New()
	p := middleware.RequireSAPermission

	saAPI.Get("/equipos/products", p("equipos.view"), h.ListProducts)
	saAPI.Post("/equipos/products", p("equipos.catalog"), h.CreateProduct)
	saAPI.Put("/equipos/products/:id", p("equipos.catalog"), h.UpdateProduct)

	saAPI.Get("/equipos/combos", p("equipos.view"), h.ListCombos)
	saAPI.Post("/equipos/combos", p("equipos.catalog"), h.CreateCombo)
	saAPI.Put("/equipos/combos/:id", p("equipos.catalog"), h.UpdateCombo)

	saAPI.Get("/equipos/carriers", p("equipos.view"), h.ListCarriers)
	saAPI.Post("/equipos/carriers", p("equipos.carriers"), h.CreateCarrier)
	saAPI.Put("/equipos/carriers/:id", p("equipos.carriers"), h.UpdateCarrier)

	saAPI.Get("/equipos/settings", p("equipos.view"), h.GetSettings)
	saAPI.Put("/equipos/settings", p("equipos.settings"), h.UpdateSettings)
	saAPI.Post("/equipos/settings/pin", p("equipos.settings"), h.SetSecurityPin)
	saAPI.Get("/equipos/lookup/:type", p("equipos.create"), h.Lookup)

	saAPI.Get("/equipos/stock", p("equipos.stock_view"), h.StockReport)
	saAPI.Get("/equipos/stock/:productId/movements", p("equipos.stock_view"), h.ProductMovements)
	saAPI.Post("/equipos/stock/movements", p("equipos.stock_adjust"), h.AddMovement)

	saAPI.Post("/equipos/import/preview", p("equipos.import"), h.ImportPreview)
	saAPI.Post("/equipos/import/commit", p("equipos.import"), h.ImportCommit)
	saAPI.Get("/equipos/import/batches", p("equipos.import"), h.ImportBatches)

	saAPI.Get("/equipos/customers", p("equipos.view"), h.ListCustomers)
	saAPI.Post("/equipos/customers", p("equipos.create"), h.CreateCustomer)
	saAPI.Put("/equipos/customers/:id", p("equipos.create"), h.UpdateCustomer)
	saAPI.Get("/equipos/customers/:id/account", p("equipos.payments_view"), h.CustomerAccount)
	saAPI.Get("/equipos/customers/:id/open-balances", p("equipos.payments_view"), h.OpenBalances)

	saAPI.Get("/equipos/orders", p("equipos.view"), h.ListOrders)
	saAPI.Post("/equipos/orders", p("equipos.create"), h.CreateOrder)
	saAPI.Get("/equipos/orders/:id", p("equipos.view"), h.GetOrder)
	saAPI.Put("/equipos/orders/:id", p("equipos.update"), h.UpdateOrder)
	saAPI.Post("/equipos/orders/:id/confirm", p("equipos.create"), h.ConfirmOrder)
	saAPI.Post("/equipos/orders/:id/cancel", p("equipos.cancel"), h.CancelOrder)
	saAPI.Post("/equipos/orders/:id/validate", p("equipos.validate"), h.ValidateOrder)
	saAPI.Post("/equipos/orders/:id/observe", p("equipos.validate"), h.ObserveOrder)
	saAPI.Post("/equipos/orders/:id/no-payment", p("equipos.payments"), h.SetNoPayment)

	saAPI.Get("/equipos/shipments", p("equipos.view"), h.ListShipments)
	saAPI.Put("/equipos/orders/:id/shipment", p("equipos.shipments"), h.UpdateShipment)
	saAPI.Post("/equipos/orders/:id/dispatch", p("equipos.shipments"), handler.DispatchOrder)
	saAPI.Post("/equipos/orders/:id/arrived", p("equipos.shipments"), handler.ArrivedOrder)
	saAPI.Post("/equipos/orders/:id/picked-up", p("equipos.shipments"), handler.PickedUpOrder)
	saAPI.Post("/equipos/orders/:id/label-printed", p("equipos.shipments"), h.LabelPrinted)

	saAPI.Get("/equipos/payments", p("equipos.payments_view"), h.ListPayments)
	saAPI.Get("/equipos/payments/:id", p("equipos.payments_view"), h.GetPayment)
	saAPI.Post("/equipos/payments", p("equipos.payments"), h.CreatePayment)
	saAPI.Post("/equipos/payments/:id/void", p("equipos.payments"), h.VoidPayment)
	saAPI.Post("/equipos/payments/:id/allocate", p("equipos.payments"), h.AllocatePayment)

	saAPI.Get("/equipos/returns", p("equipos.view"), h.ListReturns)
	saAPI.Post("/equipos/returns", p("equipos.returns"), h.CreateReturn)
	saAPI.Put("/equipos/returns/:id", p("equipos.returns"), h.UpdateReturn)
	saAPI.Post("/equipos/returns/:id/reship", p("equipos.returns"), h.ReshipReturn)

	saAPI.Get("/equipos/dashboard", p("equipos.view"), h.Dashboard)
	saAPI.Get("/equipos/alerts", p("equipos.view"), h.Alerts)

	saAPI.Get("/equipos/reports/summary", p("equipos.reports"), h.ReportSummary)
	saAPI.Get("/equipos/reports/sales", p("equipos.reports"), h.ReportSales)
	saAPI.Get("/equipos/reports/collections", p("equipos.reports"), h.ReportCollections)
	saAPI.Get("/equipos/reports/replenishment", p("equipos.reports"), h.ReportReplenishment)
	saAPI.Get("/equipos/reports/profit", p("equipos.reports"), h.ReportProfit)
	saAPI.Get("/equipos/periods", p("equipos.reports"), h.ClosedPeriods)
	saAPI.Post("/equipos/periods/:period/close", p("equipos.settings"), h.ClosePeriod)
	saAPI.Post("/equipos/periods/:period/reopen", p("equipos.settings"), h.ReopenPeriod)
}
