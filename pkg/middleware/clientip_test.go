package middleware

import (
	"net/http/httptest"
	"testing"
	"time"

	"tukifac/config"

	"github.com/gofiber/fiber/v3"
	"github.com/golang-jwt/jwt/v5"
)

func TestClientIPFrom(t *testing.T) {
	cases := []struct {
		name, peer, cf, want string
	}{
		{"peer de Cloudflare + CF-Connecting-IP válido → IP real", "172.64.222.34", "190.236.1.2", "190.236.1.2"},
		{"conexión directa al origen con CF-Connecting-IP falsificado → se ignora", "200.10.20.30", "1.1.1.1", "200.10.20.30"},
		{"peer de Cloudflare sin header → peer", "172.64.222.34", "", "172.64.222.34"},
		{"peer de Cloudflare con header basura → peer", "172.64.222.34", "no-es-ip", "172.64.222.34"},
		{"IPv6 de cliente se agrupa por /64", "2606:4700::1", "2800:200:1234:5678:aaaa:bbbb:cccc:dddd", "2800:200:1234:5678::/64"},
		{"peer IPv6 de Cloudflare reconocido", "2606:4700:3036::6815:1a31", "190.236.1.2", "190.236.1.2"},
		{"peer IPv6 directo (no Cloudflare) con header falso → peer /64", "2800:200:1:2:3:4:5:6", "9.9.9.9", "2800:200:1:2::/64"},
	}
	for _, tc := range cases {
		if got := clientIPFrom(tc.peer, tc.cf); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestLastIPOfChain(t *testing.T) {
	if got := lastIPOfChain("2a02:4780:75:29c1::1, 162.158.155.68"); got != "162.158.155.68" {
		t.Errorf("cadena: %q", got)
	}
	if got := lastIPOfChain("  10.0.0.1 "); got != "10.0.0.1" {
		t.Errorf("simple: %q", got)
	}
}

func testToken(t *testing.T, tenantID uint, secret string) string {
	t.Helper()
	claims := &TenantClaims{TenantID: tenantID, Type: "tenant"}
	claims.ExpiresAt = jwt.NewNumericDate(time.Now().Add(time.Hour))
	s, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestRateLimitKey_TenantOnlyFromVerifiedJWT(t *testing.T) {
	config.AppConfig = &config.Config{JWTSecret: "s3cret-test"}
	app := fiber.New()
	var got string
	app.Get("/k", func(c fiber.Ctx) error { got = RateLimitKey(c); return c.SendString("ok") })

	do := func(auth string, extra map[string]string) string {
		req := httptest.NewRequest("GET", "/k", nil)
		if auth != "" {
			req.Header.Set("Authorization", "Bearer "+auth)
		}
		for k, v := range extra {
			req.Header.Set(k, v)
		}
		if _, err := app.Test(req); err != nil {
			t.Fatal(err)
		}
		return got
	}
	base := do("", nil)
	if base == "" {
		t.Fatal("la clave no puede ser vacía")
	}
	a := do(testToken(t, 7, "s3cret-test"), nil)
	b := do(testToken(t, 8, "s3cret-test"), nil)
	if a == b || a == base || b == base {
		t.Fatalf("tenants distintos y anónimo deben tener claves distintas: base=%q a=%q b=%q", base, a, b)
	}
	// Token firmado con otro secreto: no cuenta como tenant (no se puede fabricar cupos).
	if forged := do(testToken(t, 9, "otro-secreto"), nil); forged != base {
		t.Fatalf("un JWT no verificable no debe aportar tenant: %q vs %q", forged, base)
	}
	// Cabeceras controlables por el cliente no cambian la clave.
	if spoof := do("", map[string]string{"X-Tenant-Slug": "otro", "CF-Connecting-IP": "8.8.8.8"}); spoof != base {
		t.Fatalf("X-Tenant-Slug / CF-Connecting-IP falsificados no deben alterar la clave: %q vs %q", spoof, base)
	}
}

// Dos tenants detrás de la misma IP no comparten cupo; el mismo tenant sí se limita a sí mismo.
func TestApplyRateLimits_TenantsDoNotShareBucket(t *testing.T) {
	config.AppConfig = &config.Config{JWTSecret: "s3cret-test", RateLimitEnabled: true, RateLimitGlobal: 3, RateLimitAuth: 100, RateLimitPassword: 100, RateLimitBilling: 100, RateLimitUpload: 100, RateLimitPublicConsult: 100}
	app := fiber.New()
	ApplyRateLimits(app)
	app.Get("/api/algo", func(c fiber.Ctx) error { return c.SendString("ok") })

	call := func(tok string) int {
		req := httptest.NewRequest("GET", "/api/algo", nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp.StatusCode
	}
	ta, tb := testToken(t, 1, "s3cret-test"), testToken(t, 2, "s3cret-test")
	for i := 0; i < 3; i++ {
		if c := call(ta); c != 200 {
			t.Fatalf("tenant A petición %d: %d", i+1, c)
		}
	}
	if c := call(ta); c != 429 {
		t.Fatalf("tenant A debe quedar limitado en la 4ª: %d", c)
	}
	if c := call(tb); c != 200 {
		t.Fatalf("tenant B no debe verse afectado por el cupo de A: %d", c)
	}
}
