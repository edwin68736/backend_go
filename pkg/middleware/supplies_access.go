package middleware

import (
	"tukifac/pkg/restaurantperm"

	"github.com/gofiber/fiber/v3"
)

// CanListSupplies: quién puede pedir insumos en el listado de productos (parámetro supplies). El personal de restaurante que solo
// entra por la carta necesita supplies.view; sin esto, pedir supplies=only se saltaría el permiso de las rutas de insumos.
func CanListSupplies(c fiber.Ctx) bool {
	if claims, ok := c.Locals("tenant_claims").(*TenantClaims); ok && claims != nil && tenantHasPermission(claims.Permissions, "products.view") {
		return true
	}
	ensureRestaurantPermissions(c)
	return HasRestaurantPerm(c, restaurantperm.SuppliesView) || HasRestaurantPerm(c, restaurantperm.SuppliesManage)
}
