package middleware

import (
	"github.com/gofiber/fiber/v3"
)

// TenantDBRelease decrementa el uso del pool tenant al finalizar el request (evita use-after-close en eviction).
func TenantDBRelease() fiber.Handler {
	return func(c fiber.Ctx) error {
		dbName, _ := c.Locals("tenant_db_name").(string)
		err := c.Next()
		if dbName != "" {
			releaseTenantDB(dbName)
		}
		return err
	}
}
