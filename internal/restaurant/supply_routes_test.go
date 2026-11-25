package restaurant

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	invsvc "tukifac/internal/inventory/service"
	"tukifac/internal/restaurant/handler"
	"tukifac/pkg/database"
	"tukifac/pkg/middleware"

	"github.com/glebarez/sqlite"
	"github.com/gofiber/fiber/v3"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// F1a: las rutas de insumos que usa Tukichef, con los permisos reales de restaurante (personal con PIN y administrador).
func supplyApp(t *testing.T) (*fiber.App, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())),
		&gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.ApplyBaselineSchema(db); err != nil {
		t.Fatal(err)
	}
	for _, m := range []interface{}{
		&database.TenantRestaurantSetting{}, &database.TenantRestaurantStaff{}, &database.TenantProduct{},
		&database.TenantProductSaleUnit{}, &database.TenantProductBranch{}, &database.TenantProductAttribute{},
		&database.TenantComboGroup{}, &database.TenantComboGroupItem{}, &database.TenantProductModifierGroup{},
	} {
		if err := db.AutoMigrate(m); err != nil {
			t.Fatal(err)
		}
	}
	db.Create(&database.TenantCompanyConfig{ID: 1, TaxRate: 18})
	db.Create(&database.TenantBranch{Name: "Principal", Active: true, IsMain: true})
	for id, kind := range map[uint]string{2: "supervisor", 3: "waiter"} {
		if err := db.Create(&database.TenantRestaurantStaff{UserID: id, EmployeeType: kind, DisplayName: kind, IsActive: true}).Error; err != nil {
			t.Fatal(err)
		}
	}
	app := fiber.New()
	app.Use(func(c fiber.Ctx) error {
		var uid uint
		role := ""
		switch c.Get("X-U") {
		case "admin":
			uid, role = 1, "Administrador"
		case "supervisor":
			uid = 2
		case "mozo":
			uid = 3
		}
		c.Locals("tenantDB", db)
		c.Locals("user_id", uid)
		c.Locals("active_branch_id", uint(1))
		c.Locals("allowed_branch_ids", []uint{1})
		c.Locals("tenant_claims", &middleware.TenantClaims{UserID: uid, RoleName: role, TenantSlug: t.Name(), TenantID: 9})
		return c.Next()
	})
	r := app.Group("/restaurant", middleware.LoadRestaurantPermissions())
	RegisterSupplyRoutes(r, handler.New())
	// Sin permisos cargados = sin recipes.view_cost: sirve para probar que el costo ni sale ni entra.
	h := handler.New()
	app.Get("/nocost/:id", h.GetSupply)
	app.Put("/nocost/:id", h.UpdateSupply)
	app.Get("/nocost/:id/movements", h.SupplyMovements)
	return app, db
}

func call(t *testing.T, app *fiber.App, user, method, path string, body any) (int, string) {
	t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	prefix := "/restaurant"
	if strings.HasPrefix(path, "/nocost") {
		prefix = ""
	}
	req := httptest.NewRequest(method, prefix+path, rd)
	req.Header.Set("X-U", user)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	out, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(out)
}

func TestSupplyRoutes(t *testing.T) {
	app, db := supplyApp(t)
	want := func(got int, body string, code int) {
		t.Helper()
		if got != code {
			t.Fatalf("status %d, want %d: %s", got, code, body)
		}
	}
	// Módulo apagado: ni el administrador entra.
	s, b := call(t, app, "admin", "GET", "/supplies", nil)
	want(s, b, 403)
	if !strings.Contains(b, "recipes_disabled") {
		t.Fatal(b)
	}
	if database.TenantFeatureFlags(db)["restaurant_recipes"] {
		t.Fatal("nace apagado")
	}
	if err := database.SetRecipesEnabled(db, true); err != nil {
		t.Fatal(err)
	}

	s, b = call(t, app, "admin", "POST", "/supplies", map[string]any{"name": "Arroz", "unit": "KGM", "unit_cost": 3.5, "min_stock": 5, "sale_price": 99})
	want(s, b, 201)
	var created struct {
		Data struct {
			ID       uint     `json:"id"`
			UnitCost *float64 `json:"unit_cost"`
			Unit     string   `json:"unit"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(b), &created); err != nil || created.Data.ID == 0 || created.Data.UnitCost == nil || *created.Data.UnitCost != 3.5 {
		t.Fatalf("alta: %s", b)
	}
	id := created.Data.ID
	path := fmt.Sprintf("/supplies/%d", id)
	var p database.TenantProduct
	db.First(&p, id)
	if !p.IsSupply || p.SalePrice != 0 || p.PurchasePrice != 3.5 {
		t.Fatalf("fila: %+v", p)
	}

	// Supervisor: ve y edita, con costo; el costo se conserva si no lo envía.
	s, b = call(t, app, "supervisor", "PUT", path, map[string]any{"name": "Arroz extra"})
	want(s, b, 200)
	db.First(&p, id)
	if p.Name != "Arroz extra" || p.PurchasePrice != 3.5 || p.MinStock != 5 {
		t.Fatalf("edición parcial: %+v", p)
	}
	for _, url := range []string{"/supplies", path, path + "/stock", path + "/movements", path + "/units", "/supplies/unit-catalog"} {
		s, b = call(t, app, "supervisor", "GET", url, nil)
		want(s, b, 200)
		s, b = call(t, app, "mozo", "GET", url, nil)
		want(s, b, 403)
	}
	s, b = call(t, app, "mozo", "POST", "/supplies", map[string]any{"name": "Sal"})
	want(s, b, 403)

	// Sin recipes.view_cost: no viaja el costo y el que se envíe se ignora.
	db.Create(&database.TenantStockMovement{ProductID: id, BranchID: 1, Type: "in", Quantity: 1, Balance: 1, UnitCost: 3.5, UserID: 1})
	s, b = call(t, app, "supervisor", "PUT", fmt.Sprintf("/nocost/%d", id), map[string]any{"name": "Arroz extra", "unit_cost": 99})
	want(s, b, 200)
	for _, url := range []string{fmt.Sprintf("/nocost/%d", id), fmt.Sprintf("/nocost/%d/movements", id)} {
		s, b = call(t, app, "mozo", "GET", url, nil)
		want(s, b, 200)
		if strings.Contains(b, "unit_cost") || strings.Contains(b, "purchase_price") {
			t.Fatalf("costo expuesto en %s: %s", url, b)
		}
	}
	db.First(&p, id)
	if p.PurchasePrice != 3.5 {
		t.Fatalf("el costo no debía cambiar: %v", p.PurchasePrice)
	}
	// F2b: con una entrada con costo, la lista y el detalle traen el promedio; sin permiso de costos, no.
	if err := invsvc.NewInventoryService(db).RecordMovement(invsvc.MovementInput{ProductID: id, BranchID: 1, Type: "in", Quantity: 3, UnitCost: 4, UserID: 1}); err != nil {
		t.Fatal(err)
	}
	for _, url := range []string{"/supplies", path} {
		s, b = call(t, app, "supervisor", "GET", url, nil)
		want(s, b, 200)
		if !strings.Contains(b, `"avg_cost":4`) || !strings.Contains(b, `"unit_cost":3.5`) {
			t.Fatalf("faltan promedio o último costo en %s: %s", url, b)
		}
	}
	s, b = call(t, app, "mozo", "GET", fmt.Sprintf("/nocost/%d", id), nil)
	want(s, b, 200)
	if strings.Contains(b, "avg_cost") {
		t.Fatalf("promedio expuesto sin permiso: %s", b)
	}

	// Con movimientos, la unidad base ya no se cambia.
	s, b = call(t, app, "admin", "PUT", path, map[string]any{"unit": "GRM"})
	want(s, b, 400)

	// Un producto vendible no se alcanza por estas rutas.
	dish := database.TenantProduct{Code: "P", Name: "Plato", Type: "product", Unit: "NIU", SalePrice: 10, Active: true}
	db.Create(&dish)
	s, b = call(t, app, "admin", "PUT", fmt.Sprintf("/supplies/%d", dish.ID), map[string]any{"name": "x"})
	want(s, b, 404)
	s, b = call(t, app, "admin", "GET", "/supplies", nil)
	want(s, b, 200)
	if strings.Contains(b, "Plato") || !strings.Contains(b, "Arroz extra") {
		t.Fatalf("lista: %s", b)
	}
}
