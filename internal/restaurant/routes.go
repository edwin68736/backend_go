package restaurant

import (
	"tukifac/internal/restaurant/handler"
	"tukifac/pkg/middleware"
	"tukifac/pkg/restaurantperm"

	"github.com/gofiber/fiber/v3"
)

func RegisterRoutes(api fiber.Router) {
	h := handler.New()

	r := api.Group("/restaurant",
		middleware.RequireModule("restaurant"),
		middleware.LoadRestaurantPermissions(),
		middleware.RequireRestaurantStaff(),
	)

	manage := middleware.RequireRestaurantAdminOrTenantAdmin()

	r.Get("/session/permissions", h.SessionPermissions)

	RegisterSupplyRoutes(r, h)
	r.Get("/staff", manage, h.ListStaff)
	r.Get("/staff/management", manage, h.ListStaffManagement)
	r.Post("/staff/users", manage, h.CreateStaffUser)
	r.Post("/staff/users/:id/restore", manage, h.RestoreStaffUser)
	r.Put("/users/:id/staff", manage, h.UpsertUserStaff)

	r.Get("/settings", middleware.RequireAnyRestaurantPerm(restaurantperm.OrdersCharge, restaurantperm.TablesOpen, restaurantperm.KitchenView, restaurantperm.SettingsManage), h.GetSettings)
	r.Put("/settings", manage, h.UpdateSettings)

	r.Get("/floors", middleware.RequireRestaurantPerm(restaurantperm.TablesView), h.ListFloors)
	r.Post("/floors", middleware.RequireRestaurantPerm(restaurantperm.ProductsManage), h.CreateFloor)
	r.Put("/floors/:id", middleware.RequireRestaurantPerm(restaurantperm.ProductsManage), h.UpdateFloor)
	r.Delete("/floors/:id", middleware.RequireRestaurantPerm(restaurantperm.ProductsManage), h.DeleteFloor)

	r.Get("/tables", middleware.RequireRestaurantPerm(restaurantperm.TablesView), h.ListTables)
	r.Post("/tables", middleware.RequireRestaurantPerm(restaurantperm.ProductsManage), h.CreateTable)
	r.Put("/tables/:id", middleware.RequireRestaurantPerm(restaurantperm.ProductsManage), h.UpdateTable)
	r.Delete("/tables/:id", middleware.RequireRestaurantPerm(restaurantperm.ProductsManage), h.DeleteTable)
	r.Get("/tables/:id/session", middleware.RequireRestaurantPerm(restaurantperm.TablesView), h.GetTableSession)

	r.Get("/orders", middleware.RequireAnyRestaurantPerm(restaurantperm.TablesView, restaurantperm.KitchenView, restaurantperm.POSUse), h.ListOpenOrders)
	r.Get("/delivery-companies", middleware.RequireRestaurantPerm(restaurantperm.DeliveryView), h.ListDeliveryCompanies)
	r.Post("/delivery-companies", middleware.RequireRestaurantPerm(restaurantperm.ProductsManage), h.CreateDeliveryCompany)
	r.Put("/delivery-companies/:id", middleware.RequireRestaurantPerm(restaurantperm.ProductsManage), h.UpdateDeliveryCompany)
	r.Delete("/delivery-companies/:id", middleware.RequireRestaurantPerm(restaurantperm.ProductsManage), h.DeleteDeliveryCompany)
	r.Get("/delivery-drivers", middleware.RequireRestaurantPerm(restaurantperm.DeliveryView), h.ListDeliveryDrivers)
	r.Post("/delivery-drivers", middleware.RequireRestaurantPerm(restaurantperm.ProductsManage), h.CreateDeliveryDriver)
	r.Put("/delivery-drivers/:id", middleware.RequireRestaurantPerm(restaurantperm.ProductsManage), h.UpdateDeliveryDriver)
	r.Delete("/delivery-drivers/:id", middleware.RequireRestaurantPerm(restaurantperm.ProductsManage), h.DeleteDeliveryDriver)

	r.Post("/sessions", middleware.RequireRestaurantPerm(restaurantperm.TablesOpen), h.OpenSession)
	r.Get("/sessions/:id", middleware.RequireRestaurantPerm(restaurantperm.TablesView), h.GetSession)
	r.Patch("/sessions/:id", middleware.RequireRestaurantPerm(restaurantperm.TablesView), h.UpdateSession)
	r.Patch("/sessions/:id/table", middleware.RequireRestaurantPerm(restaurantperm.TablesOpen), h.MoveSessionTable)
	r.Put("/sessions/:id/order-status", middleware.RequireAnyRestaurantPerm(restaurantperm.TablesView, restaurantperm.KitchenView), h.UpdateOrderStatus)
	r.Get("/sessions/:id/precuenta", middleware.RequireRestaurantPerm(restaurantperm.TablesView), h.GetPrecuenta)
	r.Post("/sessions/:id/orders", middleware.RequireRestaurantPerm(restaurantperm.TablesOpen), h.AddOrder)
	r.Post("/table-orders/:id/printed", middleware.RequireRestaurantPerm(restaurantperm.TablesOpen), h.MarkTableOrderPrinted)
	r.Post("/sessions/:id/bill", middleware.RequireRestaurantPerm(restaurantperm.OrdersCharge), h.BillSession)
	// Checkout compuesto del POS de venta rápida (Open+AddOrder+Bill en 1 request).
	// Exige los mismos permisos que las operaciones que compone: TablesOpen + OrdersCharge.
	r.Post("/pos/checkout",
		middleware.RequireRestaurantPerm(restaurantperm.TablesOpen),
		middleware.RequireRestaurantPerm(restaurantperm.OrdersCharge),
		h.POSCheckout)
	r.Post("/sessions/:id/close", middleware.RequireRestaurantPerm(restaurantperm.OrdersCharge), h.CloseSession)
	r.Post("/sessions/:id/cancel", middleware.RequireAnyRestaurantPerm(restaurantperm.OrdersCharge, restaurantperm.OrdersCancel, restaurantperm.SettingsManage), h.CancelSession)
	r.Post("/sessions/:id/cancel-comandas", middleware.RequireAnyRestaurantPerm(restaurantperm.SettingsManage, restaurantperm.OrdersCancel), h.CancelAllComandas)

	r.Patch("/comandas/:id/notes", middleware.RequireAnyRestaurantPerm(restaurantperm.TablesOpen, restaurantperm.OrdersCreate, restaurantperm.POSUse), h.UpdateComandaNotes)
	r.Put("/comandas/:id/status", middleware.RequireRestaurantPerm(restaurantperm.KitchenUpdate), h.UpdateComandaStatus)
	r.Post("/comandas/:id/print", middleware.RequireRestaurantPerm(restaurantperm.KitchenView), h.PrintComanda)
	r.Delete("/comandas/:id", middleware.RequireAnyRestaurantPerm(restaurantperm.SettingsManage, restaurantperm.OrdersCancel), h.CancelComanda)

	r.Get("/comandas/cancelled", middleware.RequireAnyRestaurantPerm(restaurantperm.KitchenView, restaurantperm.OrdersCancel, restaurantperm.SettingsManage), h.ListCancelledComandas)
	r.Get("/kitchen", middleware.RequireRestaurantPerm(restaurantperm.KitchenView), h.KitchenView)

	r.Get("/dashboard",
		middleware.RequireAnyRestaurantPerm(
			restaurantperm.OrdersCharge,
			restaurantperm.CashView,
			restaurantperm.SettingsManage,
			restaurantperm.TablesView,
		),
		h.Dashboard,
	)
}

// RegisterSalePaymentRoutes registra los endpoints de pagos bajo /api/sales
func RegisterSalePaymentRoutes(api fiber.Router) {
	h := handler.New()
	mod := middleware.RequireModule("sales")
	loadRest := middleware.LoadRestaurantPermissions()
	api.Post("/sales/:id/payments", mod, loadRest, middleware.RequireSalesAccess("create"), h.RegisterPayments)
	api.Get("/sales/:id/payments", mod, loadRest, middleware.RequireSalesAccess("view"), h.GetSalePayments)
}

// RegisterSupplyRoutes monta las rutas de insumos sobre un grupo que ya cargó los permisos de restaurante.
func RegisterSupplyRoutes(r fiber.Router, h *handler.RestaurantHandler) {
	// Insumos (F1): solo con el módulo activado.
	viewSupplies := middleware.RequireAnyRestaurantPerm(restaurantperm.SuppliesView, restaurantperm.SuppliesManage)
	manageSupplies := middleware.RequireRestaurantPerm(restaurantperm.SuppliesManage)
	r.Get("/supplies", h.RequireRecipesEnabled, viewSupplies, h.ListSupplies)
	r.Post("/supplies", h.RequireRecipesEnabled, manageSupplies, h.CreateSupply)
	r.Get("/supplies/unit-catalog", h.RequireRecipesEnabled, viewSupplies, h.SupplyUnitCatalog)
	r.Get("/supplies/:id", h.RequireRecipesEnabled, viewSupplies, h.GetSupply)
	r.Put("/supplies/:id", h.RequireRecipesEnabled, manageSupplies, h.UpdateSupply)
	r.Get("/supplies/:id/stock", h.RequireRecipesEnabled, viewSupplies, h.SupplyStock)
	r.Get("/supplies/:id/movements", h.RequireRecipesEnabled, viewSupplies, h.SupplyMovements)
	r.Get("/supplies/:id/units", h.RequireRecipesEnabled, viewSupplies, h.ListSupplyUnits)
	r.Post("/supplies/:id/units", h.RequireRecipesEnabled, manageSupplies, h.CreateSupplyUnit)
	r.Put("/supplies/:id/units/:unitId", h.RequireRecipesEnabled, manageSupplies, h.UpdateSupplyUnit)
	r.Delete("/supplies/:id/units/:unitId", h.RequireRecipesEnabled, manageSupplies, h.DeleteSupplyUnit)

	// Compras, proveedores, ingresos y carga inicial (F3a). Las compras exigen además los módulos de compras y caja.
	buy := middleware.RequireRestaurantPerm(restaurantperm.SuppliesPurchase)
	purchases, cash := middleware.RequireModule("purchases"), middleware.RequireModule("cashbank")
	r.Get("/suppliers", h.RequireRecipesEnabled, buy, h.ListSuppliers)
	r.Post("/suppliers", h.RequireRecipesEnabled, buy, h.CreateSupplier)
	r.Get("/supply-purchases", h.RequireRecipesEnabled, buy, purchases, h.ListSupplyPurchases)
	r.Post("/supply-purchases", h.RequireRecipesEnabled, buy, purchases, cash, h.CreateSupplyPurchase)
	r.Get("/supply-purchases/:id", h.RequireRecipesEnabled, buy, purchases, h.GetSupplyPurchase)
	r.Post("/supply-purchases/:id/void", h.RequireRecipesEnabled, buy, purchases, cash, h.VoidSupplyPurchase)
	r.Post("/supply-entries", h.RequireRecipesEnabled, buy, h.CreateSupplyEntry)
	r.Post("/supplies/:id/initial-stock", h.RequireRecipesEnabled, manageSupplies, h.SupplyInitialStock)
	r.Get("/supplies/:id/purchases", h.RequireRecipesEnabled, viewSupplies, h.SupplyPurchaseHistory)

	// Recetas (F4a). Los costos y el precio sugerido exigen además recipes.view_cost.
	viewRecipes := middleware.RequireAnyRestaurantPerm(restaurantperm.RecipesView, restaurantperm.RecipesManage)
	manageRecipes := middleware.RequireRestaurantPerm(restaurantperm.RecipesManage)
	viewCost := middleware.RequireRestaurantPerm(restaurantperm.RecipesViewCost)
	manageDishes := middleware.RequireRestaurantPerm(restaurantperm.ProductsManage)
	// Descuento de insumos al vender (F5a): lo enciende el administrador del restaurante.
	restaurantSettings := middleware.RequireRestaurantPerm(restaurantperm.SettingsManage)
	r.Get("/recipe-consumption", h.RequireRecipesEnabled, h.RequireRecipeTables, h.RequireConsumptionTables, restaurantSettings, h.GetRecipeConsumption)
	r.Put("/recipe-consumption", h.RequireRecipesEnabled, h.RequireRecipeTables, h.RequireConsumptionTables, restaurantSettings, h.UpdateRecipeConsumption)
	r.Get("/recipe-settings", h.RequireRecipesEnabled, h.RequireRecipeTables, viewCost, h.GetRecipeSettings)
	r.Put("/recipe-settings", h.RequireRecipesEnabled, h.RequireRecipeTables, manageRecipes, viewCost, h.UpdateRecipeSettings)
	r.Get("/recipes", h.RequireRecipesEnabled, h.RequireRecipeTables, viewRecipes, h.ListRecipes)
	r.Get("/recipes/:productId", h.RequireRecipesEnabled, h.RequireRecipeTables, viewRecipes, h.GetRecipe)
	r.Put("/recipes/:productId", h.RequireRecipesEnabled, h.RequireRecipeTables, manageRecipes, h.SaveRecipe)
	r.Delete("/recipes/:productId", h.RequireRecipesEnabled, h.RequireRecipeTables, manageRecipes, h.DeleteRecipe)
	r.Get("/recipes/:productId/price-history", h.RequireRecipesEnabled, h.RequireRecipeTables, viewCost, h.RecipePriceHistory)
	r.Post("/recipes/:productId/apply-price", h.RequireRecipesEnabled, h.RequireRecipeTables, manageRecipes, viewCost, manageDishes,
		h.ApplyRecipePrice)
}
