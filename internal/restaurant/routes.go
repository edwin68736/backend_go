package restaurant

import (
	"tukifac/internal/restaurant/handler"
	"tukifac/pkg/middleware"

	"github.com/gofiber/fiber/v3"
)

func RegisterRoutes(api fiber.Router) {
	h := handler.New()

	r := api.Group("/restaurant", middleware.RequireModule("restaurant"))

	// Roles: admin, vendedor, mozo, cocinero
	adminOnly := middleware.RequireRestaurantRole("admin")
	adminVendedor := middleware.RequireRestaurantRole("admin", "vendedor")
	adminVendedorMozo := middleware.RequireRestaurantRole("admin", "vendedor", "mozo")
	allRoles := middleware.RequireRestaurantRole("admin", "vendedor", "mozo", "cocinero")

	// Gestión de roles (admin restaurante O administrador tenant)
	roleMgmt := middleware.RequireRestaurantAdminOrTenantAdmin()
	r.Get("/me", allRoles, h.GetMyRestaurantRole)
	r.Get("/roles/assignments", roleMgmt, h.ListRestaurantRoleAssignments)
	r.Put("/users/:id/restaurant-role", roleMgmt, h.SetUserRestaurantRole)

	// Config: solo admin
	r.Get("/settings", adminOnly, h.GetSettings)
	r.Put("/settings", adminOnly, h.UpdateSettings)

	r.Get("/floors", adminOnly, h.ListFloors)
	r.Post("/floors", adminOnly, h.CreateFloor)
	r.Put("/floors/:id", adminOnly, h.UpdateFloor)
	r.Delete("/floors/:id", adminOnly, h.DeleteFloor)

	r.Get("/tables", adminVendedorMozo, h.ListTables)
	r.Post("/tables", adminOnly, h.CreateTable)
	r.Put("/tables/:id", adminOnly, h.UpdateTable)
	r.Delete("/tables/:id", adminOnly, h.DeleteTable)
	r.Get("/tables/:id/session", adminVendedorMozo, h.GetTableSession)

	r.Get("/waiters", adminVendedorMozo, h.ListWaiters)
	r.Post("/waiters", adminOnly, h.CreateWaiter)
	r.Put("/waiters/:id", adminOnly, h.UpdateWaiter)
	r.Delete("/waiters/:id", adminOnly, h.DeleteWaiter)

	// Sesiones: admin, vendedor, mozo
	r.Post("/sessions", adminVendedorMozo, h.OpenSession)
	r.Get("/sessions/:id", adminVendedorMozo, h.GetSession)
	r.Post("/sessions/:id/orders", adminVendedorMozo, h.AddOrder)
	r.Post("/sessions/:id/bill", adminVendedor, h.BillSession)
	r.Post("/sessions/:id/close", adminVendedor, h.CloseSession)
	r.Post("/sessions/:id/cancel", adminVendedor, h.CancelSession)

	// Comandas: todos los roles (cocinero solo tiene esto)
	r.Put("/comandas/:id/status", allRoles, h.UpdateComandaStatus)
	r.Post("/comandas/:id/print", allRoles, h.PrintComanda)
	r.Delete("/comandas/:id", adminOnly, h.CancelComanda)

	r.Get("/kitchen", allRoles, h.KitchenView)
}

// RegisterSalePaymentRoutes registra los endpoints de pagos bajo /api/sales
func RegisterSalePaymentRoutes(api fiber.Router) {
	h := handler.New()
	api.Post("/sales/:id/payments", middleware.RequireModule("sales"), h.RegisterPayments)
	api.Get("/sales/:id/payments", middleware.RequireModule("sales"), h.GetSalePayments)
}
