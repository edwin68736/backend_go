package middleware

import (
	"tukifac/pkg/database"

	"github.com/gofiber/fiber/v3"
)

// ValidateTenantBinding evita cross-tenant: el JWT debe coincidir con el tenant resuelto
// (X-Tenant-Slug / subdominio) y la BD tenant del pool.
// Aplicar después de TenantAuthAPI en el grupo /api protegido.
func ValidateTenantBinding() fiber.Handler {
	return func(c fiber.Ctx) error {
		claims, ok := c.Locals("tenant_claims").(*TenantClaims)
		if !ok || claims == nil {
			return c.Next()
		}

		resolvedSlug, _ := c.Locals("tenant_slug").(string)
		if resolvedSlug == "" {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
				"error": "se requiere contexto de empresa (X-Tenant-Slug o subdominio)",
			})
		}

		if claims.TenantSlug != resolvedSlug {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
				"error": "el token no corresponde a la empresa indicada en la solicitud",
			})
		}

		if tenant, ok := c.Locals("tenant").(*database.Tenant); ok && tenant != nil {
			if claims.TenantDB != "" && tenant.DBName != claims.TenantDB {
				return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
					"error": "inconsistencia de base de datos del tenant",
				})
			}
			if claims.TenantID != 0 && tenant.ID != 0 && claims.TenantID != tenant.ID {
				return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
					"error": "el token no corresponde a la empresa activa",
				})
			}
		}

		return c.Next()
	}
}
