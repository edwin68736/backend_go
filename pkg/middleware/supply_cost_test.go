package middleware

import (
	"fmt"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"tukifac/pkg/database"

	"github.com/glebarez/sqlite"
	"github.com/gofiber/fiber/v3"
	"gorm.io/gorm"
)

// F2b: sin products.view_cost ni recipes.view_cost, los costos de las filas de insumos salen en 0; el resto no se toca.
func TestStripSupplyCost(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&database.TenantProduct{}); err != nil {
		t.Fatal(err)
	}
	db.Create(&database.TenantProduct{ID: 1, Name: "Plato"})
	db.Create(&database.TenantProduct{ID: 2, Name: "Arroz"})
	db.Table("tenant_products").Where("id = ?", 2).Update("is_supply", true)

	body := fiber.Map{"data": []fiber.Map{
		{"product_id": 1, "unit_cost": 5.5},
		{"product_id": 2, "unit_cost": 3.25, "line_total": 32.5},
		{"id": 2, "is_supply": true, "purchase_price": 3.25},
		{"id": 1, "is_supply": false, "purchase_price": 5.5},
	}}
	run := func(perms []string) string {
		t.Helper()
		app := fiber.New()
		app.Use(func(c fiber.Ctx) error {
			c.Locals("tenantDB", db)
			c.Locals("tenant_claims", &TenantClaims{Permissions: perms})
			return c.Next()
		}, StripSupplyCost())
		app.Get("/x", func(c fiber.Ctx) error { return c.JSON(body) })
		resp, err := app.Test(httptest.NewRequest("GET", "/x", nil))
		if err != nil {
			t.Fatal(err)
		}
		out, _ := io.ReadAll(resp.Body)
		return string(out)
	}
	hidden := run(nil)
	for _, visible := range []string{`"unit_cost":5.5`, `"purchase_price":5.5`} {
		if !strings.Contains(hidden, visible) {
			t.Fatalf("el costo del producto normal no debía cambiar: %s", hidden)
		}
	}
	if strings.Contains(hidden, "3.25") || strings.Contains(hidden, "32.5") {
		t.Fatalf("costo de insumo expuesto: %s", hidden)
	}
	if full := run([]string{PermProductsViewCost}); !strings.Contains(full, "3.25") || !strings.Contains(full, "32.5") {
		t.Fatalf("con permiso debía verse: %s", full)
	}
}
