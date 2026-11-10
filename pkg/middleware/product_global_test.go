package middleware

import (
	"fmt"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"tukifac/pkg/database"

	"github.com/glebarez/sqlite"
	"github.com/gofiber/fiber/v3"
	"gorm.io/gorm"
)

var pgSeq atomic.Int64

func pgCall(t *testing.T, seeded bool, perms []string) int {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s_%d?mode=memory&cache=shared", t.Name(), pgSeq.Add(1))), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&database.TenantPermission{}); err != nil {
		t.Fatal(err)
	}
	if seeded {
		db.Create(&database.TenantPermission{Module: "products", Action: "manage_global", Label: "x"})
	}
	app := fiber.New()
	app.Use(func(c fiber.Ctx) error {
		c.Locals("tenantDB", db)
		c.Locals("tenant_claims", &TenantClaims{Permissions: perms})
		return c.Next()
	})
	app.Put("/p", RequirePermission("products.edit"), RequireGlobalCatalog(), func(c fiber.Ctx) error { return c.SendStatus(200) })
	resp, err := app.Test(httptest.NewRequest("PUT", "/p", nil))
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode
}

func TestRequireGlobalCatalog(t *testing.T) {
	if c := pgCall(t, true, []string{"products.edit"}); c != 403 {
		t.Errorf("products.edit sin manage_global: %d, quiere 403", c)
	}
	if c := pgCall(t, true, []string{"products.edit", PermProductsManageGlobal}); c != 200 {
		t.Errorf("con manage_global: %d, quiere 200", c)
	}
	if c := pgCall(t, true, []string{"products.manage"}); c != 200 {
		t.Errorf("products.manage implica el módulo: %d, quiere 200", c)
	}
	// Tenant sin V177: no restringe (despliegue escalonado).
	if c := pgCall(t, false, []string{"products.edit"}); c != 200 {
		t.Errorf("sin V177: %d, quiere 200", c)
	}
}
