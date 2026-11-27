package sales

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"tukifac/pkg/middleware"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/recover"
)

// F6a: la utilidad muestra costos y ganancias; ver ventas no basta, hace falta el permiso del reporte (el mismo que pide la pantalla).
func TestProfitDetailExigeSuPermiso(t *testing.T) {
	call := func(permissions ...string) (int, string) {
		app := fiber.New()
		app.Use(recover.New())
		api := app.Group("/api", func(c fiber.Ctx) error {
			c.Locals("tenant_claims", &middleware.TenantClaims{UserID: 1, TenantID: 1, TenantSlug: "utilidad",
				Modules: []string{"sales"}, Permissions: permissions})
			return c.Next()
		})
		RegisterRoutes(api)
		res, err := app.Test(httptest.NewRequest("GET", "/api/sales/profit-detail", nil))
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(res.Body)
		return res.StatusCode, string(body)
	}
	if status, body := call("sales.view"); status != 403 || !strings.Contains(body, "reports.profit") {
		t.Fatalf("solo ver ventas no basta: %d %s", status, body)
	}
	// Con el permiso pasa el control (sin base de datos el reporte falla después, que aquí no importa).
	if status, body := call("sales.view", "reports.profit"); status == 403 {
		t.Fatalf("con el permiso no debe rechazarse: %d %s", status, body)
	}
}
