package restaurant

// F6a: reportes de existencias, compras y mermas por sus rutas reales, con permisos y fechas.

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	invsvc "tukifac/internal/inventory/service"
	rsvc "tukifac/internal/restaurant/service"
	"tukifac/pkg/database"
	"tukifac/pkg/database/tenantmigrations"

	"gorm.io/gorm"
)

func (f *buyFixture) report(user, path string, out any) {
	f.t.Helper()
	s, b := f.call(user, "GET", path, nil)
	wantStatus(f.t, s, b, 200)
	ok(f.t, json.Unmarshal([]byte(b), out))
}

func TestSupplyReports(t *testing.T) {
	f := setupBuy(t)
	sup := f.supplier()
	today := time.Now().Format("2006-01-02")
	period := "?from=" + today + "&to=" + today

	// Dos compras de 1 saco (50 kg a S/ 177 con IGV = S/ 3.00 el kilo); la segunda se anula y no cuenta.
	s, b := f.call("supervisor", "POST", "/supply-purchases", f.purchaseBody(sup, "100", "k1"))
	wantStatus(t, s, b, 201)
	s, b = f.call("supervisor", "POST", "/supply-purchases", f.purchaseBody(sup, "101", "k2"))
	wantStatus(t, s, b, 201)
	var second struct {
		Data struct {
			ID uint `json:"id"`
		} `json:"data"`
	}
	ok(t, json.Unmarshal([]byte(b), &second))
	s, b = f.call("supervisor", "POST", "/supply-purchases/"+u(second.Data.ID)+"/void", map[string]any{})
	wantStatus(t, s, b, 200)

	var buys struct {
		Data    []rsvc.SupplyPurchaseRow   `json:"data"`
		Total   int                        `json:"total"`
		Summary rsvc.SupplyPurchaseSummary `json:"summary"`
	}
	f.report("supervisor", "/reports/supply-purchases"+period, &buys)
	if buys.Total != 1 || len(buys.Data) != 1 {
		t.Fatalf("solo la compra vigente: %+v", buys)
	}
	row := buys.Data[0]
	if row.Quantity != 50 || row.Document != "F001-100" || row.Supplier != "Mercado SAC" || row.UnitCost == nil || !near(*row.UnitCost, 3) ||
		row.Amount == nil || !near(*row.Amount, 150) || buys.Summary.TotalAmount == nil || !near(*buys.Summary.TotalAmount, 150) {
		t.Fatalf("compra en unidad base y sin IGV: %+v total=%v", row, buys.Summary.TotalAmount)
	}

	// Merma de 2 kg y desperdicio de 1 kg; otro desperdicio anulado no cuenta.
	entry := func(kind string, qty float64, key, notes string) uint {
		s, b := f.call("supervisor", "POST", "/supply-entries",
			map[string]any{"supply_id": f.supply, "kind": kind, "quantity": qty, "idempotency_key": key, "notes": notes})
		wantStatus(t, s, b, 201)
		var out struct {
			Data struct {
				DocumentID uint `json:"document_id"`
			} `json:"data"`
		}
		ok(t, json.Unmarshal([]byte(b), &out))
		return out.Data.DocumentID
	}
	entry("merma", 2, "w1", "se humedeció")
	entry("desperdicio", 1, "w2", "se cayó")
	voided := entry("desperdicio", 4, "w3", "error")
	ok(t, invsvc.NewInventoryDocumentService(f.db).VoidInventoryDocument(voided, 2))
	entry("ingreso", 5, "w4", "no es merma")

	var waste struct {
		Data    []rsvc.SupplyWasteRow   `json:"data"`
		Total   int                     `json:"total"`
		Summary rsvc.SupplyWasteSummary `json:"summary"`
	}
	f.report("supervisor", "/reports/supply-waste"+period, &waste)
	if waste.Total != 2 || len(waste.Data) != 2 {
		t.Fatalf("merma y desperdicio vigentes: %+v", waste)
	}
	kinds := map[string]float64{}
	for _, w := range waste.Data {
		kinds[w.Kind] = w.Quantity
		if w.Value == nil || !near(*w.Value, w.Quantity*3) || w.Notes == "" {
			t.Fatalf("valor al costo promedio y motivo: %+v", w)
		}
	}
	if kinds["merma"] != 2 || kinds["desperdicio"] != 1 || waste.Summary.TotalValue == nil || !near(*waste.Summary.TotalValue, 9) {
		t.Fatalf("tipos y total: %v %v", kinds, waste.Summary.TotalValue)
	}
	f.report("supervisor", "/reports/supply-waste"+period+"&per_page=1&page=2", &waste)
	if waste.Total != 2 || len(waste.Data) != 1 || !near(*waste.Summary.TotalValue, 9) {
		t.Fatalf("la segunda página conserva el total: %+v", waste)
	}

	// Existencias: 50 - 2 - 1 + 5 = 52 kg.
	var stock struct {
		Data    []rsvc.SupplyStockRow   `json:"data"`
		Summary rsvc.SupplyStockSummary `json:"summary"`
	}
	f.report("supervisor", "/reports/supply-stock", &stock)
	qty, avg := f.stock()
	if len(stock.Data) != 1 || stock.Data[0].Quantity != qty || qty != 52 || stock.Data[0].Value == nil ||
		!near(*stock.Data[0].Value, qty*avg) || stock.Summary.TotalValue == nil || !near(*stock.Summary.TotalValue, qty*avg) {
		t.Fatalf("existencias: %+v saldo %v a %v", stock, qty, avg)
	}

	// Sin permiso de costos no viaja ninguno.
	svc := rsvc.New(f.db)
	rows, summary, err := svc.SupplyStockReport(1, false)
	ok(t, err)
	raw, _ := json.Marshal(map[string]any{"data": rows, "summary": summary})
	for _, key := range []string{"avg_cost", "last_cost", "\"value\"", "total_value"} {
		if strings.Contains(string(raw), key) {
			t.Fatalf("costo expuesto (%s): %s", key, raw)
		}
	}

	// Permisos y fechas.
	for _, path := range []string{"/reports/supply-stock", "/reports/supply-purchases" + period, "/reports/supply-waste" + period} {
		for _, user := range []string{"mozo", "cocinero"} {
			s, b = f.call(user, "GET", path, nil)
			wantStatus(t, s, b, 403)
		}
	}
	for _, query := range []string{"", "?from=" + today, "?from=2026-01-02&to=2026-01-01", "?from=2025-01-01&to=2026-06-01"} {
		s, b = f.call("supervisor", "GET", "/reports/supply-waste"+query, nil)
		wantStatus(t, s, b, 400)
	}
}

// Consumo y rentabilidad: exigen las tablas del descuento; la rentabilidad, además, ver costos. V200 se puede repetir.
func TestRecipeReportRoutes(t *testing.T) {
	f := setupRecipes(t)
	today := time.Now().Format("2006-01-02")
	period := "?from=" + today + "&to=" + today
	paths := []string{"/reports/supply-consumption" + period, "/reports/dish-profitability" + period}
	for _, path := range paths {
		s, b := f.call("supervisor", "GET", path, nil)
		wantStatus(t, s, b, 403) // todavía sin las tablas del descuento
	}
	ok(t, f.db.AutoMigrate(&database.TenantSale{}, &database.TenantSaleItem{}, &database.TenantUser{}))
	for _, m := range []interface{ Up(*gorm.DB) error }{tenantmigrations.V199RecipeConsumption{}, tenantmigrations.V200RecipeReportIndexes{},
		tenantmigrations.V200RecipeReportIndexes{}} {
		ok(t, m.Up(f.db))
	}
	if !f.db.Migrator().HasIndex(&database.TenantRecipeConsumption{}, "idx_recipe_consumption_branch_date") ||
		!f.db.Migrator().HasIndex(&database.TenantRecipeConsumptionReversal{}, "idx_recipe_reversal_date") {
		t.Fatal("V200 no creó los índices")
	}
	for _, path := range paths {
		s, b := f.call("supervisor", "GET", path, nil)
		wantStatus(t, s, b, 200)
		for _, user := range []string{"mozo", "cocinero"} {
			s, b = f.call(user, "GET", path, nil)
			wantStatus(t, s, b, 403)
		}
	}
}
