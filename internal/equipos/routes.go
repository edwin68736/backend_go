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

	saAPI.Get("/equipos/stock", p("equipos.stock_view"), h.StockReport)
	saAPI.Get("/equipos/stock/:productId/movements", p("equipos.stock_view"), h.ProductMovements)
	saAPI.Post("/equipos/stock/movements", p("equipos.stock_adjust"), h.AddMovement)

	saAPI.Post("/equipos/import/preview", p("equipos.import"), h.ImportPreview)
	saAPI.Post("/equipos/import/commit", p("equipos.import"), h.ImportCommit)
	saAPI.Get("/equipos/import/batches", p("equipos.import"), h.ImportBatches)
}
