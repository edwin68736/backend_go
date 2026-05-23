package middleware

import (
	"tukifac/config"
	"tukifac/pkg/database"
	"tukifac/pkg/tenantstorage"
	"tukifac/pkg/utils"

	"github.com/gofiber/fiber/v3"
)

// TenantResolver identifica el tenant activo en cada request mediante:
//  1. Subdominio real: {slug}.ROOT_DOMAIN (ej. empresa1.tukifac.com con APP_DOMAIN=tukifac.com)
//  2. Header X-Tenant-Slug (clientes API / Postman)
//  3. Cookie dev_tenant (solo desarrollo, simula subdominio desde localhost)
//
// Hosts no-tenant (api, app, www…) se excluyen vía RESERVED_SUBDOMAINS en .env.
func TenantResolver() fiber.Handler {
	return func(c fiber.Ctx) error {
		host := c.Hostname()

		// Prioridad 1: header explícito (útil para Postman / apps móviles)
		slug := c.Get("X-Tenant-Slug")

		// Prioridad 2: subdominio real (producción y staging)
		if slug == "" {
			slug = utils.ExtractSubdomain(host, config.AppConfig.AppDomain)
		}

		// Prioridad 3: cookie de simulación (solo modo desarrollo)
		// Permite trabajar en localhost sin configurar subdominios en /etc/hosts
		if slug == "" && config.AppConfig.IsDev() {
			slug = c.Cookies("dev_tenant")
		}

		// Sin tenant o subdominio reservado (api, app, www…) → contexto central / rutas públicas
		if slug == "" || config.AppConfig.IsReservedSubdomain(slug) {
			return c.Next()
		}

		// Buscar tenant en BD central (sin filtrar por status).
		// El control de status se delega:
		//   - A LoginAPI para el endpoint de autenticación (retorna 403 descriptivo).
		//   - A TenantAuthAPI para el resto de requests (valida claims.Status del JWT).
		tenant, err := LookupTenantBySlug(slug)
		if err != nil {
			c.ClearCookie("dev_tenant")
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
				"error": "Empresa no encontrada",
				"slug":  slug,
			})
		}

		// Conectar a la BD del tenant (pool dinámico)
		tenantDB, err := database.GetTenantDB(tenant.DBName)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).SendString("Error conectando a la base de datos de la empresa")
		}

		c.Locals("tenant_db_name", tenant.DBName)
		c.Locals("tenant", tenant)
		c.Locals("tenantDB", tenantDB)
		c.Locals("tenant_slug", tenant.Slug)

		if tenant.Status != "active" && tenant.Status != database.TenantStatusBlocked &&
			tenant.Status != database.TenantStatusSuspended && !IsSubscriptionExemptPath(c.Path()) {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
				"error":  "Cuenta suspendida o inactiva",
				"status": tenant.Status,
			})
		}
		if ruc := tenantstorage.SanitizeRUC(tenant.RUC); ruc != "" {
			c.Locals("tenant_ruc", ruc)
		}
		return c.Next()
	}
}

// RequireTenant verifica que exista un contexto de tenant activo.
func RequireTenant() fiber.Handler {
	return func(c fiber.Ctx) error {
		if c.Locals("tenant") == nil {
			return c.Status(fiber.StatusBadRequest).SendString("Acceso no permitido sin contexto de empresa")
		}
		return c.Next()
	}
}
