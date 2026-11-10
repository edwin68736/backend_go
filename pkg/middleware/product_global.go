package middleware

import (
	"tukifac/pkg/database"

	"github.com/gofiber/fiber/v3"
	"gorm.io/gorm"
)

// PermProductsManageGlobal protege la información GLOBAL del catálogo: datos maestros del producto (nombre, código,
// categoría, marca, unidad, impuestos, precio global, imágenes, galería, unidades de venta, atributos), su alta/baja y
// las categorías/marcas/unidades. La información comercial POR SUCURSAL (precio por sucursal, visibilidad y stock)
// sigue regida por products.edit y similares.
const PermProductsManageGlobal = "products.manage_global"

func productsGlobalCatalogReady(db *gorm.DB) bool {
	if db == nil {
		return false
	}
	return database.CachedSchemaProbe(db, "perm:"+PermProductsManageGlobal, func() bool {
		var n int64
		db.Model(&database.TenantPermission{}).Where("module = ? AND action = ?", "products", "manage_global").Count(&n)
		return n > 0
	})
}

// RequireGlobalCatalog exige products.manage_global (además del permiso de acción de la ruta). Mientras el tenant no
// tenga la migración V177 (permiso no sembrado) no restringe, para no bloquear a nadie en un despliegue escalonado.
func RequireGlobalCatalog() fiber.Handler {
	return func(c fiber.Ctx) error {
		tdb, _ := c.Locals("tenantDB").(*gorm.DB)
		if !productsGlobalCatalogReady(tdb) {
			return c.Next()
		}
		claims, _ := c.Locals("tenant_claims").(*TenantClaims)
		if claims != nil && HasPermission(claims, PermProductsManageGlobal) {
			return c.Next()
		}
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
			"error":      "No tiene permiso para modificar el catálogo global de productos",
			"permission": PermProductsManageGlobal,
		})
	}
}

// CanManageGlobalCatalog: variante booleana para handlers que deciden según el body (p. ej. actualización masiva de
// precios: con sucursal es comercial, sin sucursal es el precio global).
func CanManageGlobalCatalog(c fiber.Ctx) bool {
	tdb, _ := c.Locals("tenantDB").(*gorm.DB)
	if !productsGlobalCatalogReady(tdb) {
		return true
	}
	claims, _ := c.Locals("tenant_claims").(*TenantClaims)
	return claims != nil && HasPermission(claims, PermProductsManageGlobal)
}
