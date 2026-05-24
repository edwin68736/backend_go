package tenantctx

import (
	"errors"

	"tukifac/pkg/database"

	"github.com/gofiber/fiber/v3"
	"gorm.io/gorm"
)

// Locals keys (request-scoped; Fiber crea un contexto por request).
const (
	LocalTenant     = "tenant"
	LocalTenantDB   = "tenantDB"
	LocalTenantSlug = "tenant_slug"
	LocalTenantDBName = "tenant_db_name"
	LocalTenantID   = "tenant_id"
)

var (
	ErrNoTenant   = errors.New("tenant context not found")
	ErrNoTenantDB = errors.New("tenant database not found")
)

// Bind inyecta metadata inmutable del tenant resuelto en este request.
func Bind(c fiber.Ctx, tenant *database.Tenant, db *gorm.DB) {
	if tenant == nil {
		return
	}
	c.Locals(LocalTenant, tenant)
	c.Locals(LocalTenantSlug, tenant.Slug)
	c.Locals(LocalTenantDBName, tenant.DBName)
	c.Locals(LocalTenantID, tenant.ID)
	if db != nil {
		c.Locals(LocalTenantDB, db)
	}
}

// Tenant devuelve el tenant del request actual.
func Tenant(c fiber.Ctx) (*database.Tenant, bool) {
	t, ok := c.Locals(LocalTenant).(*database.Tenant)
	return t, ok && t != nil
}

// TenantDB devuelve el pool GORM del tenant para este request.
func TenantDB(c fiber.Ctx) (*gorm.DB, bool) {
	db, ok := c.Locals(LocalTenantDB).(*gorm.DB)
	return db, ok && db != nil
}

// MustTenantDB para handlers; falla si no hay contexto tenant.
func MustTenantDB(c fiber.Ctx) (*gorm.DB, error) {
	if db, ok := TenantDB(c); ok {
		return db, nil
	}
	return nil, ErrNoTenantDB
}

// Slug del tenant resuelto en este request.
func Slug(c fiber.Ctx) string {
	s, _ := c.Locals(LocalTenantSlug).(string)
	return s
}

// DBName nombre físico de la BD MySQL del tenant.
func DBName(c fiber.Ctx) string {
	n, _ := c.Locals(LocalTenantDBName).(string)
	return n
}

// ID central del tenant.
func ID(c fiber.Ctx) (uint, bool) {
	switch v := c.Locals(LocalTenantID).(type) {
	case uint:
		return v, v > 0
	case int:
		if v > 0 {
			return uint(v), true
		}
	}
	if t, ok := Tenant(c); ok {
		return t.ID, t.ID > 0
	}
	return 0, false
}
