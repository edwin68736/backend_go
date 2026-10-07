package equipos

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"tukifac/config"
	"tukifac/pkg/database"
	"tukifac/pkg/logger"
	"tukifac/pkg/middleware"

	"github.com/glebarez/sqlite"
	"github.com/gofiber/fiber/v3"
	"github.com/golang-jwt/jwt/v5"
	"gorm.io/gorm"
)

const testSecret = "equipos-routes-test-secret"

// Cada ruta del módulo exige su permiso exacto: sin token → 401; con token de otro permiso → 403.
func TestRoutes_requireExactPermission(t *testing.T) {
	logger.Init(&config.Config{LogLevel: "error", AppEnv: "development"})
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&database.SuperAdminUser{}, &database.AuditLog{}); err != nil {
		t.Fatal(err)
	}
	prevDB := database.CentralDB
	database.CentralDB = db
	t.Cleanup(func() { database.CentralDB = prevDB })
	prevCfg := config.AppConfig
	config.AppConfig = &config.Config{AppEnv: "development", SAJWTSecret: testSecret}
	t.Cleanup(func() { config.AppConfig = prevCfg })

	user := database.SuperAdminUser{Name: "Equipos", Email: "equipos@example.com", Role: "admin", Active: true}
	if err := user.SetPassword("password123"); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}

	app := fiber.New()
	RegisterRoutes(app.Group("/api/superadmin", middleware.SuperAdminAuthAPI()))

	mint := func(perms []string) string {
		claims := &middleware.SuperAdminClaims{
			UserID: user.ID, Email: user.Email, Role: "admin", Type: "superadmin", Permissions: perms,
			SAJWTVersion:     middleware.CurrentSuperAdminJWTVersion(),
			RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)), IssuedAt: jwt.NewNumericDate(time.Now())},
		}
		s, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(testSecret))
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	routes := []struct{ method, path, perm string }{
		{"GET", "/equipos/products", "equipos.view"},
		{"POST", "/equipos/products", "equipos.catalog"},
		{"PUT", "/equipos/products/1", "equipos.catalog"},
		{"GET", "/equipos/combos", "equipos.view"},
		{"POST", "/equipos/combos", "equipos.catalog"},
		{"PUT", "/equipos/combos/1", "equipos.catalog"},
		{"GET", "/equipos/carriers", "equipos.view"},
		{"POST", "/equipos/carriers", "equipos.carriers"},
		{"PUT", "/equipos/carriers/1", "equipos.carriers"},
		{"GET", "/equipos/settings", "equipos.view"},
		{"PUT", "/equipos/settings", "equipos.settings"},
		{"GET", "/equipos/stock", "equipos.stock_view"},
		{"GET", "/equipos/stock/1/movements", "equipos.stock_view"},
		{"POST", "/equipos/stock/movements", "equipos.stock_adjust"},
		{"POST", "/equipos/import/preview", "equipos.import"},
		{"POST", "/equipos/import/commit", "equipos.import"},
		{"GET", "/equipos/import/batches", "equipos.import"},
	}
	wrong := mint([]string{"dashboard.view"})
	for _, r := range routes {
		req := httptest.NewRequest(r.method, "/api/superadmin"+r.path, nil)
		if resp, _ := app.Test(req); resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s %s sin token: %d, want 401", r.method, r.path, resp.StatusCode)
		}
		req = httptest.NewRequest(r.method, "/api/superadmin"+r.path, nil)
		req.Header.Set("Authorization", "Bearer "+wrong)
		if resp, _ := app.Test(req); resp.StatusCode != http.StatusForbidden {
			t.Errorf("%s %s con permiso ajeno: %d, want 403", r.method, r.path, resp.StatusCode)
		}
		// Un permiso de OTRA acción del mismo módulo tampoco alcanza (p. ej. equipos.view no permite escribir).
		if r.perm != "equipos.view" {
			req = httptest.NewRequest(r.method, "/api/superadmin"+r.path, nil)
			req.Header.Set("Authorization", "Bearer "+mint([]string{"equipos.view"}))
			if resp, _ := app.Test(req); resp.StatusCode != http.StatusForbidden {
				t.Errorf("%s %s solo con equipos.view: %d, want 403 (exige %s)", r.method, r.path, resp.StatusCode, r.perm)
			}
		}
	}
}
