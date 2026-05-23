package middleware

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"tukifac/config"
	"tukifac/pkg/database"

	"github.com/gofiber/fiber/v3"
	"github.com/golang-jwt/jwt/v5"
)

func TestValidateTenantBinding_rejectsSlugMismatch(t *testing.T) {
	config.AppConfig = &config.Config{JWTSecret: "test-secret"}

	app := fiber.New()
	app.Use(func(c fiber.Ctx) error {
		c.Locals("tenant_slug", "empresa-a")
		c.Locals("tenant", &database.Tenant{ID: 1, Slug: "empresa-a", DBName: "saas_tenant_empresa_a"})
		c.Locals("tenant_claims", &TenantClaims{
			TenantSlug: "empresa-b",
			TenantDB:   "saas_tenant_empresa_b",
			TenantID:   2,
			Type:       "tenant",
			Status:     "active",
		})
		return c.Next()
	})
	app.Get("/api/test", ValidateTenantBinding(), func(c fiber.Ctx) error {
		return c.SendString("ok")
	})

	req := httptest.NewRequest("GET", "/api/test", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
}

func TestValidateTenantBinding_acceptsMatch(t *testing.T) {
	config.AppConfig = &config.Config{JWTSecret: "test-secret"}

	app := fiber.New()
	app.Use(func(c fiber.Ctx) error {
		c.Locals("tenant_slug", "empresa-a")
		c.Locals("tenant", &database.Tenant{ID: 1, Slug: "empresa-a", DBName: "saas_tenant_empresa_a"})
		c.Locals("tenant_claims", &TenantClaims{
			TenantSlug: "empresa-a",
			TenantDB:   "saas_tenant_empresa_a",
			TenantID:   1,
			Type:       "tenant",
			Status:     "active",
		})
		return c.Next()
	})
	app.Get("/api/test", ValidateTenantBinding(), func(c fiber.Ctx) error {
		return c.SendString("ok")
	})

	req := httptest.NewRequest("GET", "/api/test", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestTenantClaimsJWT_roundtrip(t *testing.T) {
	secret := []byte("test-secret-jwt")
	claims := &TenantClaims{
		UserID:     10,
		TenantSlug: "demo",
		TenantDB:   "saas_tenant_demo",
		TenantID:   5,
		Type:       "tenant",
		Status:     "active",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	s, err := token.SignedString(secret)
	if err != nil {
		t.Fatal(err)
	}
	parsed := &TenantClaims{}
	_, err = jwt.ParseWithClaims(s, parsed, func(t *jwt.Token) (interface{}, error) {
		return secret, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if parsed.TenantSlug != "demo" || parsed.TenantDB != "saas_tenant_demo" {
		t.Fatalf("unexpected claims: %+v", parsed)
	}
	b, _ := json.Marshal(parsed)
	if len(b) == 0 {
		t.Fatal("empty json")
	}
}
