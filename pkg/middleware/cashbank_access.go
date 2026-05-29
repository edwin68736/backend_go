package middleware

import (
	"tukifac/pkg/restaurantperm"

	"github.com/gofiber/fiber/v3"
)

// cashbankTenantPerm mapea acción → permiso tenant.
func cashbankTenantPerm(action string) string {
	switch action {
	case "open":
		return "cashbank.open"
	case "close":
		return "cashbank.close"
	case "movements":
		return "cashbank.movements"
	case "manage":
		return "cashbank.manage"
	default:
		return "cashbank.view"
	}
}

// RequireCashbankAccess permite acceso a caja vía permisos tenant o staff restaurante (c.v+).
// Debe usarse después de RequireModule("cashbank") y LoadRestaurantPermissions().
func RequireCashbankAccess(action string) fiber.Handler {
	tenantPerm := cashbankTenantPerm(action)
	return func(c fiber.Ctx) error {
		claims, ok := c.Locals("tenant_claims").(*TenantClaims)
		if !ok || claims == nil {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Sin contexto de autenticación"})
		}
		if len(claims.Permissions) > 0 {
			for _, p := range claims.Permissions {
				if p == tenantPerm || p == "cashbank.manage" {
					return c.Next()
				}
			}
		}
		if claims.AuthMethod == "pin" || claims.EmployeeType != "" {
			if action == "manage" {
				if HasRestaurantPerm(c, restaurantperm.SettingsManage) {
					return c.Next()
				}
			} else if HasRestaurantPerm(c, restaurantperm.CashView) {
				return c.Next()
			}
			if claims.RoleName == "Administrador" {
				return c.Next()
			}
		}
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
			"error":      "No tienes permiso para operar la caja",
			"permission": tenantPerm,
		})
	}
}
