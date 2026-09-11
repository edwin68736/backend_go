package middleware

import (
	"github.com/gofiber/fiber/v3"
)

// RequirePermission verifica que el usuario tenga el permiso indicado (formato "module.action"),
// incluyendo las implicaciones de tenantHasPermission (p. ej. "{modulo}.manage" concede todas las
// acciones de ese módulo). Antes esta función solo hacía match exacto contra claims.Permissions,
// distinto del criterio que ya usaban RequireSalesAccess/RequireCashbankAccess (tenantHasPermission)
// — un mismo permiso "funcionaba" en unos módulos y no en otros según qué helper protegía la ruta.
// Unificado para que asignar/quitar un permiso en Roles tenga el mismo efecto en todos los módulos.
// Usa los permisos cargados en el JWT (c.Locals("tenant_claims")). Debe usarse después de TenantAuthAPI.
func RequirePermission(permission string) fiber.Handler {
	return func(c fiber.Ctx) error {
		claims, ok := c.Locals("tenant_claims").(*TenantClaims)
		if !ok || claims == nil {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
				"error": "Sin contexto de autenticación",
			})
		}
		if claims.Permissions == nil {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
				"error": "Sesión sin permisos. Inicia sesión de nuevo.",
			})
		}
		if tenantHasPermission(claims.Permissions, permission) {
			return c.Next()
		}
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
			"error":      "No tienes permiso para esta acción",
			"permission": permission,
		})
	}
}
