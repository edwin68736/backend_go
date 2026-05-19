package superadmin

import (
	"tukifac/internal/ajustes"
	consultaHandler "tukifac/internal/consulta/handler"
	"tukifac/internal/payments"
	"tukifac/internal/plans"
	"tukifac/internal/subscriptions"
	"tukifac/internal/superadmin/handler"
	ubigeo "tukifac/internal/ubigeo"
	"tukifac/pkg/middleware"

	"github.com/gofiber/fiber/v3"
)

func RegisterRoutes(app *fiber.App) {
	authHandler := handler.NewAuthSAHandler()
	dashHandler := handler.NewDashboardHandler()
	tenantHandler := handler.NewTenantHandler()
	ubigeoCentral := ubigeo.NewCentralHandler()

	// Login público
	app.Post("/api/superadmin/login", authHandler.LoginAPI)

	// API protegida
	saAPI := app.Group("/api/superadmin", middleware.SuperAdminAuthAPI())
	saAPI.Get("/stats", dashHandler.StatsAPI)
	saAPI.Get("/users", authHandler.ListUsersAPI)
	saAPI.Post("/users", authHandler.CreateUserAPI)
	saAPI.Put("/users/:id", authHandler.UpdateUserAPI)
	saAPI.Post("/users/:id/password", authHandler.ResetUserPasswordAPI)
	saAPI.Post("/me/password", authHandler.ChangeMyPasswordAPI)
	saAPI.Get("/tenants", tenantHandler.ListAPI)
	saAPI.Get("/tenants/conectados-sunat", tenantHandler.ListConectadosSunatAPI)
	saAPI.Get("/pse/empresas", tenantHandler.ListPSEEmpresasAPI)
	saAPI.Get("/pse/empresas/:id", tenantHandler.GetPSEEmpresaAPI)
	saAPI.Post("/pse/empresas", tenantHandler.CreatePSEEmpresaAPI)
	saAPI.Put("/pse/empresas/:id", tenantHandler.UpdatePSEEmpresaAPI)
	saAPI.Patch("/pse/empresas/:id/toggle", tenantHandler.TogglePSEEmpresaAPI)
	saAPI.Get("/tenants/:id", tenantHandler.GetAPI)
	saAPI.Post("/tenants", tenantHandler.CreateAPI)
	saAPI.Put("/tenants/:id", tenantHandler.UpdateAPI)
	saAPI.Patch("/tenants/:id/status", tenantHandler.ToggleStatusAPI)
	saAPI.Get("/tenants/:id/modules", tenantHandler.GetModulesAPI)
	saAPI.Post("/tenants/:id/modules", tenantHandler.SetModuleAPI)
	saAPI.Post("/tenants/:id/migrate", tenantHandler.MigrateAPI)
	saAPI.Post("/tenants/migrate-all", tenantHandler.MigrateAllAPI)
	saAPI.Get("/tenants/:id/sunat-config", tenantHandler.GetSunatConfigAPI)
	saAPI.Put("/tenants/:id/sunat-config", tenantHandler.UpdateSunatConfigAPI)
	saAPI.Patch("/tenants/:id/sunat-env", tenantHandler.PatchSunatEnvAPI)
	saAPI.Post("/tenants/:id/sync-facturador", tenantHandler.SyncFacturadorAPI)
	saAPI.Post("/tenants/:id/pse/sync", tenantHandler.SyncTenantPSECredentialsAPI)

	// Ubigeo Perú (para formularios de empresas)
	saAPI.Get("/ubigeo/regiones", ubigeoCentral.RegionesAPI)
	saAPI.Get("/ubigeo/provincias", ubigeoCentral.ProvinciasAPI)
	saAPI.Get("/ubigeo/distritos", ubigeoCentral.DistritosAPI)

	// Ajustes del sistema central (nombre, slogan, token_consulta, etc.)
	ajustes.RegisterRoutes(saAPI)

	// Consulta DNI/RUC (apiperu.dev) — panel central al registrar tenants
	consultaH := consultaHandler.NewConsultaHandler()
	saAPI.Post("/consulta/dni", consultaH.ConsultaDNIAPI)
	saAPI.Post("/consulta/ruc", consultaH.ConsultaRUCAPI)

	// Planes, módulos del catálogo, suscripciones y pagos
	plans.RegisterRoutes(saAPI)
	subscriptions.RegisterRoutes(saAPI)
	payments.RegisterRoutes(saAPI)
}
