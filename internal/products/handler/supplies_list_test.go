package handler

import (
	"fmt"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"tukifac/pkg/database"
	"tukifac/pkg/middleware"

	"github.com/glebarez/sqlite"
	"github.com/gofiber/fiber/v3"
	"gorm.io/gorm"
)

// GET /api/products: los insumos no salen por defecto y solo los puede pedir quien tiene permiso.
func TestSearchAPI_supplies(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []interface{}{&database.TenantProduct{}, &database.TenantCategory{}, &database.TenantBrand{},
		&database.TenantProductStock{}, &database.TenantProductPresentation{}, &database.TenantProductSaleUnit{}} {
		if err := db.AutoMigrate(m); err != nil {
			t.Fatal(err)
		}
	}
	db.Create(&database.TenantProduct{Code: "P", Name: "Plato", Type: "product", Active: true})
	supply := database.TenantProduct{Code: "I", Name: "Arroz", Type: "product", Active: true}
	db.Create(&supply)
	db.Table("tenant_products").Where("id = ?", supply.ID).Update("is_supply", true)

	app := fiber.New()
	app.Use(func(c fiber.Ctx) error {
		c.Locals("tenantDB", db)
		c.Locals("is_branch_admin", true)
		perms := []string{}
		if c.Get("X-U") == "erp" {
			perms = []string{"products.view"}
		}
		c.Locals("tenant_claims", &middleware.TenantClaims{Permissions: perms})
		return c.Next()
	})
	app.Get("/products", NewProductHandler().SearchAPI)
	get := func(user, query string) string {
		t.Helper()
		req := httptest.NewRequest("GET", "/products"+query, nil)
		req.Header.Set("X-U", user)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != 200 {
			t.Fatalf("%d: %s", resp.StatusCode, body)
		}
		return string(body)
	}
	check := func(body string, plato, arroz bool) {
		t.Helper()
		if strings.Contains(body, "Plato") != plato || strings.Contains(body, "Arroz") != arroz {
			t.Fatalf("plato=%v arroz=%v: %s", plato, arroz, body)
		}
	}
	check(get("erp", ""), true, false)
	check(get("erp", "?supplies=include"), true, true)
	check(get("erp", "?supplies=only"), false, true)
	check(get("erp", "?report=1"), true, false)
	check(get("pin", "?supplies=only"), true, false) // sin permiso se ignora
}
