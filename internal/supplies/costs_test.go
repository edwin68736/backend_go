// F2b: costo promedio de insumos, por los servicios reales (compra, anulación, documentos, traslados, Excel).
// Reglas: docs/recetas/F2_PROPUESTA.md §3.
package supplies_test

import (
	"fmt"
	"math"
	"testing"
	"time"

	invsvc "tukifac/internal/inventory/service"
	productsvc "tukifac/internal/products/service"
	purchasesvc "tukifac/internal/purchases/service"
	"tukifac/pkg/database"
	"tukifac/pkg/database/tenantmigrations"
	"tukifac/pkg/tax"

	"gorm.io/gorm"
)

type costs struct {
	fixture
	t   *testing.T
	seq int
}

func setupCosts(t *testing.T) *costs {
	t.Helper()
	f := setup(t)
	for _, m := range []interface {
		Up(*gorm.DB) error
	}{tenantmigrations.V084InventoryOperationTransfer{}, tenantmigrations.V085InventoryDocumentSource{}} {
		must(t, m.Up(f.db))
	}
	must(t, database.SeedInventoryDocumentSeriesForBranch(f.db, 1))
	return &costs{fixture: f, t: t}
}

func (c *costs) avg(branch uint) float64 {
	c.t.Helper()
	var rows []struct{ AvgUnitCost *float64 }
	must(c.t, c.db.Table("tenant_product_stocks").Select("avg_unit_cost").Where("product_id = ? AND branch_id = ?", c.supply.ID, branch).Find(&rows).Error)
	if len(rows) == 0 || rows[0].AvgUnitCost == nil {
		return -1 // sin promedio
	}
	return *rows[0].AvgUnitCost
}

func (c *costs) wantAvg(branch uint, want float64) {
	c.t.Helper()
	if got := c.avg(branch); math.Abs(got-want) > 1e-6 {
		c.t.Fatalf("promedio sucursal %d = %v, quería %v", branch, got, want)
	}
}

func (c *costs) last() database.TenantStockMovement {
	c.t.Helper()
	var m database.TenantStockMovement
	must(c.t, c.db.Where("product_id = ?", c.supply.ID).Order("id DESC").First(&m).Error)
	return m
}

func (c *costs) wantLast(unitCost float64, unknown bool) {
	c.t.Helper()
	if m := c.last(); math.Abs(m.UnitCost-unitCost) > 1e-6 || m.CostUnknown != unknown {
		c.t.Fatalf("último movimiento: costo=%v desconocido=%v, quería %v %v", m.UnitCost, m.CostUnknown, unitCost, unknown)
	}
}

type buyOpts struct {
	qty, cost    float64
	includesIgv  bool
	currency     string
	rate         float64
	saleUnitID   *uint
	extraProduct bool
}

func (c *costs) buy(o buyOpts) uint {
	c.t.Helper()
	c.seq++
	pid := c.supply.ID
	in := purchasesvc.CreatePurchaseInput{
		BranchID: 1, UserID: 1, DocType: "01", Series: "F900", Number: fmt.Sprint(c.seq), IssueDate: time.Now(), Currency: "PEN",
		PaymentMethod: "cash", TaxConfig: tax.DefaultConfig(),
		Items: []purchasesvc.PurchaseItemInput{{ProductID: &pid, Description: "Arroz", Unit: "KGM", Quantity: o.qty, UnitCost: o.cost,
			IgvAffectationType: "10", PriceIncludesIgv: o.includesIgv, SaleUnitID: o.saleUnitID}},
	}
	if o.currency != "" {
		in.Currency, in.ExchangeRate = o.currency, &o.rate
	}
	p, err := purchasesvc.NewPurchaseService(c.db).Create(in)
	must(c.t, err)
	return p.ID
}

func (c *costs) void(purchaseID uint) {
	c.t.Helper()
	must(c.t, purchasesvc.NewPurchaseService(c.db).Void(purchaseID, 1))
}

func (c *costs) move(kind string, qty, cost float64, negative bool) {
	c.t.Helper()
	must(c.t, invsvc.NewInventoryService(c.db).RecordMovement(invsvc.MovementInput{
		ProductID: c.supply.ID, BranchID: 1, Type: kind, Quantity: qty, UnitCost: cost, UserID: 1, AllowNegative: negative,
	}))
}

func (c *costs) lastCost() float64 { return reload(c.t, c.db, c.supply.ID).PurchasePrice }

func TestSupplyCost_comprasSinIgvPonderan(t *testing.T) {
	c := setupCosts(t)
	if c.avg(1) != -1 {
		t.Fatal("el costo escrito al crear no debe generar promedio")
	}
	c.buy(buyOpts{qty: 10, cost: 5})
	c.wantAvg(1, 5)
	c.wantLast(5, false)
	// 10.62 con IGV = 9 sin IGV
	c.buy(buyOpts{qty: 10, cost: 10.62, includesIgv: true})
	c.wantAvg(1, 7)
	c.wantLast(9, false)
	if c.lastCost() != 9 {
		t.Fatalf("último costo = %v, quería 9 (sin IGV)", c.lastCost())
	}
}

func TestSupplyCost_nuevoRusIncluyeIgv(t *testing.T) {
	c := setupCosts(t)
	must(t, c.db.Table("tenant_company_configs").Where("id = ?", 1).Update("taxpayer_regime", "nrus").Error)
	c.buy(buyOpts{qty: 10, cost: 11.8, includesIgv: true})
	c.wantAvg(1, 11.8)
}

func TestSupplyCost_unidadDeCompra(t *testing.T) {
	c := setupCosts(t)
	var unit database.TenantUnit
	must(t, c.db.Where(database.TenantUnit{Code: "BG"}).Attrs(database.TenantUnit{Name: "Bolsa", Active: true}).FirstOrCreate(&unit).Error)
	su, err := productsvc.NewProductService(c.db).CreateSaleUnit(c.supply.ID, productsvc.SaleUnitInput{Name: "Saco 50", UnitID: &unit.ID, ConversionFactor: 50, Active: true})
	must(t, err)
	c.buy(buyOpts{qty: 1, cost: 175, saleUnitID: &su.ID})
	c.wantAvg(1, 3.5)
}

// El tipo de cambio se usa a 4 decimales, como lo guarda la compra: 10 × 3.7123 = 37.123
func TestSupplyCost_dolares(t *testing.T) {
	c := setupCosts(t)
	c.buy(buyOpts{qty: 10, cost: 10, currency: "USD", rate: 3.71229999})
	c.wantAvg(1, 37.123)
}

func TestSupplyCost_anularCompra(t *testing.T) {
	c := setupCosts(t)
	c.buy(buyOpts{qty: 10, cost: 5})
	second := c.buy(buyOpts{qty: 10, cost: 9})
	c.wantAvg(1, 7)
	c.void(second)
	c.wantAvg(1, 5) // vuelve al de antes
	c.wantLast(9, false)

	third := c.buy(buyOpts{qty: 10, cost: 9})
	c.buy(buyOpts{qty: 10, cost: 11})
	c.wantAvg(1, (20*7+10*11)/30.0)
	c.void(third) // hubo otra entrada después: no se reescribe
	c.wantAvg(1, (20*7+10*11)/30.0)
}

func TestSupplyCost_anularTodoVuelveASinPromedio(t *testing.T) {
	c := setupCosts(t)
	a := c.buy(buyOpts{qty: 10, cost: 5})
	b := c.buy(buyOpts{qty: 10, cost: 9})
	c.void(b)
	c.void(a)
	if c.avg(1) != -1 {
		t.Fatalf("sin compras vigentes no debe quedar promedio: %v", c.avg(1))
	}
}

func TestSupplyCost_salidasYAjustesNoCambianElPromedio(t *testing.T) {
	c := setupCosts(t)
	c.move("out", 2, 99, true) // sin promedio todavía
	c.wantLast(0, true)
	c.buy(buyOpts{qty: 10, cost: 4}) // saldo venía negativo: el promedio es el de la entrada
	c.wantAvg(1, 4)
	c.move("out", 3, 99, false)
	c.wantLast(4, false)
	c.move("in", 2, 0, false) // entrada sin costo
	c.wantLast(4, false)
	c.wantAvg(1, 4)
	c.move("adjustment", 0.001, 77, false)
	c.wantAvg(1, 4)
	c.wantLast(4, false)
	c.move("out", 0.001, 0, false)
	c.buy(buyOpts{qty: 5, cost: 6}) // saldo en cero: manda la entrada
	c.wantAvg(1, 6)
	c.move("out", 10, 0, true)
	c.buy(buyOpts{qty: 2, cost: 8}) // sigue negativo
	c.wantAvg(1, 8)
}

func TestSupplyCost_documentosDeInventario(t *testing.T) {
	c := setupCosts(t)
	c.buy(buyOpts{qty: 10, cost: 5})
	docs := invsvc.NewInventoryDocumentService(c.db)
	doc := func(direction, code string, qty, cost float64) uint {
		t.Helper()
		var op database.TenantInventoryOperationType
		must(t, c.db.Where("code = ?", code).First(&op).Error)
		id, err := docs.CreateInventoryDocument(invsvc.CreateDocumentInput{Direction: direction, OperationTypeID: op.ID, BranchID: 1,
			DocumentDate: time.Now(), UserID: 1, Manual: true, MovementReason: "prueba",
			Lines: []invsvc.DocumentLineInput{{ProductID: c.supply.ID, Quantity: qty, UnitCost: cost}}})
		must(t, err)
		must(t, docs.ConfirmInventoryDocument(id, 1))
		return id
	}
	in := doc("IN", "OTHER_IN", 10, 9)
	c.wantAvg(1, 7)
	out := doc("OUT", "OTHER_OUT", 4, 99) // lo digitado se ignora
	c.wantLast(7, false)
	c.wantAvg(1, 7)

	must(t, docs.VoidInventoryDocument(out, 1)) // vuelve a su costo original
	c.wantLast(7, false)
	c.wantAvg(1, 7)

	must(t, docs.VoidInventoryDocument(in, 1)) // hubo una entrada posterior (la anulación del egreso): no se reescribe
	c.wantAvg(1, 7)

	again := doc("IN", "OTHER_IN", 10, 17)
	before := c.avg(1)
	must(t, docs.VoidInventoryDocument(again, 1))
	if math.Abs(c.avg(1)-7) > 1e-6 || before == 7 {
		t.Fatalf("anular el ingreso debía devolver el promedio a 7 (antes %v, ahora %v)", before, c.avg(1))
	}
}

func TestSupplyCost_traslados(t *testing.T) {
	c := setupCosts(t)
	inv := invsvc.NewInventoryService(c.db)
	c.buy(buyOpts{qty: 20, cost: 5})

	id, err := inv.CreateTransferWithLines(1, 2, 1, "", []invsvc.TransferLineInput{{ProductID: c.supply.ID, Quantity: 5}})
	must(t, err)
	c.wantLast(5, false)
	c.buy(buyOpts{qty: 15, cost: 11}) // el origen cambia a 8; lo despachado sigue a 5
	c.wantAvg(1, 8)
	must(t, inv.ConfirmTransfer(id, 1))
	c.wantAvg(2, 5)

	id2, err := inv.CreateTransferWithLines(1, 2, 1, "", []invsvc.TransferLineInput{{ProductID: c.supply.ID, Quantity: 10}})
	must(t, err)
	must(t, inv.CancelTransfer(id2, 1))
	c.wantAvg(1, 8)
	c.wantAvg(2, 5)

	must(t, inv.Transfer(c.supply.ID, 1, 2, 5, 1, "")) // directo: 5 a 8 sobre 5 a 5
	c.wantAvg(2, 6.5)
}

func TestSupplyCost_conteoPorExcelNoLoCambia(t *testing.T) {
	c := setupCosts(t)
	c.buy(buyOpts{qty: 10, cost: 4})
	c.buy(buyOpts{qty: 20, cost: 4.25}) // promedio con decimales largos
	want := c.avg(1)
	docs := invsvc.NewInventoryDocumentService(c.db)
	for _, stock := range []float64{40, 12} {
		res, err := docs.ConfirmImportAdjustment(invsvc.ImportAdjustmentConfirmInput{BranchID: 1, MovementReason: "Conteo",
			Rows: []invsvc.ImportAdjustmentRowInput{{RowNumber: 2, Barcode: "INS-ARROZ", NewStock: stock}}}, 1)
		must(t, err)
		if res == nil {
			t.Fatal("sin resultado")
		}
		c.wantAvg(1, want)
		c.wantLast(want, false)
	}
}

// Prueba dorada: un producto normal registra exactamente lo que se le pasa y no recibe promedio.
func TestSupplyCost_productosNormalesSinCambios(t *testing.T) {
	c := setupCosts(t)
	inv := invsvc.NewInventoryService(c.db)
	must(t, c.db.Table("tenant_products").Where("id = ?", c.dish.ID).Update("manage_stock", true).Error)
	for _, m := range []invsvc.MovementInput{
		{Type: "in", Quantity: 10, UnitCost: 5}, {Type: "out", Quantity: 2, UnitCost: 99}, {Type: "in", Quantity: 1}, {Type: "adjustment", Quantity: 3, UnitCost: 7},
	} {
		m.ProductID, m.BranchID, m.UserID = c.dish.ID, 1, 1
		must(t, inv.RecordMovement(m))
	}
	var movs []database.TenantStockMovement
	must(t, c.db.Where("product_id = ?", c.dish.ID).Order("id").Find(&movs).Error)
	for i, want := range []float64{5, 99, 0, 7} {
		if movs[i].UnitCost != want || movs[i].CostUnknown || movs[i].CostApplied || movs[i].AvgBefore != nil {
			t.Fatalf("movimiento %d de un producto normal cambió: %+v", i, movs[i])
		}
	}
	var n int64
	must(t, c.db.Table("tenant_product_stocks").Where("product_id = ? AND avg_unit_cost IS NOT NULL", c.dish.ID).Count(&n).Error)
	if n != 0 {
		t.Fatal("un producto normal no debe tener promedio")
	}
}

// Tenant sin V196: los insumos se siguen comprando y moviendo como antes; al migrar se calcula el promedio con su historial.
func TestSupplyCost_sinMigrarYBackfill(t *testing.T) {
	c := setupCosts(t)
	for table, cols := range map[string][]string{
		"tenant_product_stocks": {"avg_unit_cost"}, "tenant_stock_movements": {"avg_before", "cost_applied", "cost_unknown"},
		"tenant_transfer_logs": {"unit_cost"},
	} {
		for _, col := range cols {
			must(t, c.db.Exec("ALTER TABLE "+table+" DROP COLUMN "+col).Error)
		}
	}
	inv := invsvc.NewInventoryService(c.db)
	for _, m := range []invsvc.MovementInput{{Type: "in", Quantity: 10, UnitCost: 5}, {Type: "out", Quantity: 4}, {Type: "in", Quantity: 6, UnitCost: 9}} {
		m.ProductID, m.BranchID, m.UserID = c.supply.ID, 1, 1
		must(t, inv.RecordMovement(m))
	}
	id, err := inv.CreateTransferWithLines(1, 2, 1, "", []invsvc.TransferLineInput{{ProductID: c.supply.ID, Quantity: 1}})
	must(t, err)
	must(t, inv.ConfirmTransfer(id, 1))

	for i := 0; i < 2; i++ {
		must(t, (tenantmigrations.V196SupplyCosts{}).Up(c.db))
	}
	c.wantAvg(1, 7) // (6·5 + 6·9) / 12
	if c.avg(2) != -1 {
		t.Fatalf("la sucursal que solo recibió sin costo no tiene promedio: %v", c.avg(2))
	}
}
