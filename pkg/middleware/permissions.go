package middleware

import (
	"github.com/gofiber/fiber/v3"
)

// RequirePermission verifica que el usuario tenga el permiso indicado (formato "module.action").
// Usa los permisos cargados en el JWT (c.Locals("permissions")). Debe usarse después de TenantAuthAPI.
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
		for _, p := range claims.Permissions {
			if p == permission {
				return c.Next()
			}
		}
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
			"error":      "No tienes permiso para esta acción",
			"permission": permission,
		})
	}
}
