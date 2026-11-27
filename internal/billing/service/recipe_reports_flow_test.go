package service

// F6a: reportes de rentabilidad y consumo sobre ventas hechas por los flujos reales (POS, notas de crédito, anulación y emisión
// desde nota de venta). Las cifras esperadas están calculadas a mano.

import (
	"math"
	"testing"
	"time"

	rsvc "tukifac/internal/restaurant/service"
	salesvc "tukifac/internal/sales/service"
	"tukifac/pkg/database"
)

func today() rsvc.ReportPeriod {
	now := time.Now()
	from := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	return rsvc.ReportPeriod{From: from, To: from.Add(24*time.Hour - time.Nanosecond)}
}

func wantNear(t *testing.T, what string, got, want, tolerance float64) {
	t.Helper()
	if math.Abs(got-want) > tolerance {
		t.Fatalf("%s: %v, se esperaba %v", what, got, want)
	}
}

func TestReportes_rentabilidadYConsumo(t *testing.T) {
	f := newRecipeFlow(t)
	f.consume(true, "allow")

	// 1) Boleta de 3 hamburguesas y devolución de 1 (repone insumos).
	s1 := f.quick(f.boletaSeries, item(f.burger, 3))
	f.accept(s1.ID)
	f.creditNote(s1, "07", 1)
	// 2) Boleta de 2 y descuento sobre 1 (baja la venta, no el costo).
	s2 := f.quick(f.boletaSeries, item(f.burger, 2))
	f.accept(s2.ID)
	f.creditNote(s2, "05", 1)
	// y una rebaja por importe (S/ 3.00 con IGV), que no apunta a la línea.
	rebate, _, err := f.billing.CreateCreditNote(s2.ID, "rebaja", "09", nil, 3, "")
	if rebate == nil {
		t.Fatalf("no se creó la rebaja: %v", err)
	}
	f.accept(rebate.ID)
	f.billing.PostFiscalAcceptSideEffects(rebate.ID, "SUNAT_ACCEPTED")
	// 3) Nota de venta convertida en boleta: cuenta una sola vez.
	s3 := f.quick(f.nvSeries, item(f.burger, 1))
	_, err = salesvc.NewSaleService(f.db).IssueElectronicFromNota(s3.ID, f.boletaSeries, 1, "", 0, nil)
	must(t, err)
	// 4) Nota de venta anulada: no cuenta.
	s4 := f.quick(f.nvSeries, item(f.burger, 1))
	must(t, salesvc.NewSaleService(f.db).CancelNotaVenta(s4.ID, 1, "prueba"))
	// 5) Gaseosa sin receta y combo con un componente sin receta: sin costo.
	f.quick(f.nvSeries, item(f.drink, 2))
	f.quick(f.nvSeries, item(f.combo, 1))
	// 6) Nota de crédito todavía no aceptada: no resta.
	var line database.TenantSaleItem
	must(t, f.db.Where("sale_id = ?", s1.ID).First(&line).Error)
	pending, _, err := f.billing.CreateCreditNote(s1.ID, "pendiente", "07", []NoteItemSelection{{OriginalItemID: line.ID, Quantity: 1}}, 0, "")
	if pending == nil {
		t.Fatalf("no se creó la nota pendiente: %v", err)
	}

	svc := rsvc.New(f.db)
	rows, summary, err := svc.DishProfitReport(1, today())
	must(t, err)
	byName := map[string]rsvc.DishProfitRow{}
	for _, r := range rows {
		byName[r.Name] = r
	}
	burger := byName["Hamburguesa"]
	wantNear(t, "hamburguesa: unidades (3-1) + 2 + 1", burger.Units, 5, 0.0005)
	wantNear(t, "hamburguesa: venta sin IGV menos la rebaja", burger.Revenue, 48.31, 0.03)
	wantNear(t, "hamburguesa: costo (12.45-4.15) + 8.30 + 4.15", burger.Cost, 20.75, 0.001)
	wantNear(t, "hamburguesa: margen", burger.Margin, 27.56, 0.03)
	if burger.UnitsWithoutCost != 0 || burger.FoodCostPct == nil {
		t.Fatalf("hamburguesa: todo tiene costo: %+v", burger)
	}
	wantNear(t, "hamburguesa: food cost", *burger.FoodCostPct, 20.75/48.31*100, 0.1)

	drink := byName["Gaseosa"]
	wantNear(t, "gaseosa: unidades", drink.Units, 2, 0.0005)
	wantNear(t, "gaseosa: todo sin costo", drink.UnitsWithoutCost, 2, 0.0005)
	if drink.Margin != 0 || drink.MarginPct != nil || drink.Cost != 0 {
		t.Fatalf("sin costo no hay margen: %+v", drink)
	}
	combo := byName["Combo"]
	wantNear(t, "combo sin costo", combo.UnitsWithoutCost, 1, 0.0005)

	wantNear(t, "total: costo", summary.Cost, 20.75, 0.001)
	wantNear(t, "total: sin costo no entra al margen", summary.Margin, 27.56, 0.03)
	wantNear(t, "total: venta", summary.Revenue, burger.Revenue+drink.Revenue+combo.Revenue, 0.011)

	if other, _, err := svc.DishProfitReport(2, today()); err != nil || len(other) != 0 {
		t.Fatalf("otra sucursal no ve estas ventas: %v %v", other, err)
	}
	yesterday := today()
	yesterday.From, yesterday.To = yesterday.From.AddDate(0, 0, -2), yesterday.From.AddDate(0, 0, -1)
	if old, _, err := svc.DishProfitReport(1, yesterday); err != nil || len(old) != 0 {
		t.Fatalf("fuera del período: %v %v", old, err)
	}

	// Consumo: cuadra con el kardex, insumo por insumo.
	consumption, cs, err := svc.SupplyConsumptionReport(1, today(), true)
	must(t, err)
	got := map[uint]rsvc.SupplyConsumptionRow{}
	for _, r := range consumption {
		got[r.SupplyID] = r
	}
	total := 0.0
	for _, supply := range []uint{f.meat, f.bread, f.cheese} {
		var kardex float64
		must(t, f.db.Table("tenant_stock_movements AS m").
			Joins("JOIN tenant_inventory_operation_types ot ON ot.id = m.operation_type_id").
			Where("m.product_id = ? AND m.branch_id = 1 AND ot.code IN ?", supply, []string{"RECIPE_CONSUMPTION", "RECIPE_REVERSAL"}).
			Select("COALESCE(SUM(CASE WHEN m.type = 'out' THEN m.quantity ELSE -m.quantity END), 0)").Scan(&kardex).Error)
		wantNear(t, "consumo neto contra kardex", got[supply].Net, kardex, 0.0005)
		if got[supply].Cost == nil {
			t.Fatal("con permiso de costos viaja el costo")
		}
		total += *got[supply].Cost
	}
	wantNear(t, "carne consumida", got[f.meat].Consumed, 1.2, 0.0005)
	wantNear(t, "carne repuesta", got[f.meat].Restored, 0.3, 0.0005)
	wantNear(t, "carne neta", got[f.meat].Net, 0.9, 0.0005)
	wantNear(t, "costo total del consumo", *cs.TotalCost, total, 0.011)
	if consumption[0].SupplyID != f.meat {
		t.Fatalf("ordenado por costo: primero la carne, llegó %q", consumption[0].Supply)
	}
	noCost, ns, err := svc.SupplyConsumptionReport(1, today(), false)
	must(t, err)
	if ns.TotalCost != nil || noCost[0].Cost != nil {
		t.Fatal("sin permiso de costos no viajan costos")
	}

	// Utilidad de Tukifac: el plato con receta usa el costo de sus insumos; el resto, igual que antes.
	profit, _, err := salesvc.NewSaleService(f.db).ProfitDetail(salesvc.ProfitDetailParams{BranchID: 1})
	must(t, err)
	var burgerLines, otherLines int
	for _, r := range profit {
		switch r.ProductName {
		case "Hamburguesa":
			burgerLines++
			wantNear(t, "utilidad: costo de la hamburguesa", r.PurchasePrice, 4.15, 0.0001)
		case "Gaseosa", "Combo":
			otherLines++
			wantNear(t, "utilidad: sin receta no cambia", r.PurchasePrice, 0, 0.0001)
		}
	}
	if burgerLines != 3 || otherLines != 2 {
		t.Fatalf("líneas de utilidad: %d de hamburguesa y %d otras", burgerLines, otherLines)
	}
}
