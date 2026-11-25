package service

import (
	"strings"
	"testing"
	"time"

	"tukifac/pkg/database"
	"tukifac/pkg/tax"

	"gorm.io/gorm"
)

// F0b: lo cobrado de una mesa se cobra UNA sola vez, aunque otra caja cobre, anule o agregue entre la lectura de las comandas y la
// transacción del cobro. El hook reproduce ese intervalo de forma determinista.

type billFixture struct {
	db       *gorm.DB
	svc      *RestaurantService
	session  uint
	series   uint
	comandas []database.TenantComanda
}

func newBillFixture(t *testing.T, quantities ...float64) *billFixture {
	t.Helper()
	db, table := setupTableSessionTestDB(t)
	svc := New(db)
	sess, err := svc.OpenTableExtended(openInput(table.ID))
	if err != nil {
		t.Fatal(err)
	}
	f := &billFixture{db: db, svc: svc, session: sess.ID, series: seedSplitBillSeries(t, db).ID}
	for i, q := range quantities {
		c := database.TenantComanda{SessionID: sess.ID, ProductCode: "P", ProductName: []string{"Lomo", "Chicha", "Postre"}[i], Quantity: q,
			UnitPrice: 10, Status: "pendiente", IgvAffectationType: "10", PriceIncludesIgv: true}
		if err := db.Create(&c).Error; err != nil {
			t.Fatal(err)
		}
		f.comandas = append(f.comandas, c)
	}
	return f
}

func (f *billFixture) input(key string, amount float64) BillInput {
	return BillInput{SessionID: f.session, UserID: 1, SeriesID: f.series, DocType: "03", IssueDate: time.Now(), IdempotencyKey: key,
		Payments: []PaymentInput{{Method: "cash", Amount: amount}}}
}

func (f *billFixture) sales(t *testing.T) int64 {
	t.Helper()
	var n int64
	if err := f.db.Model(&database.TenantSale{}).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	return n
}

// during ejecuta `other` justo en el intervalo del cobro externo (una sola vez).
func during(t *testing.T, other func()) {
	t.Helper()
	done := false
	billAfterReadHook = func() {
		if done {
			return
		}
		done = true
		other()
	}
	t.Cleanup(func() { billAfterReadHook = nil })
}

func assertStale(t *testing.T, err error) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), "La mesa cambió o ya se cobró") {
		t.Fatalf("el segundo cobro debe rechazarse con el mensaje de mesa cambiada, fue: %v", err)
	}
}

func TestBill_dosCajasSinCerrarMesa_unaSolaVenta(t *testing.T) {
	f := newBillFixture(t, 1, 1)
	during(t, func() {
		if _, err := f.svc.BillTable(f.input("caja-1", 20), tax.DefaultConfig()); err != nil {
			t.Fatalf("la otra caja cobra: %v", err)
		}
	})
	_, err := f.svc.BillTable(f.input("caja-2", 20), tax.DefaultConfig())
	assertStale(t, err)
	if n := f.sales(t); n != 1 {
		t.Fatalf("ventas = %d, quería 1", n)
	}
	var sess database.TenantTableSession
	f.db.First(&sess, f.session)
	if sess.Status != sessionStatusOpen {
		t.Fatalf("la mesa debe quedar abierta y operativa: %s", sess.Status)
	}
}

func TestBill_seleccionRepetida_unaSolaVentaYDistintaSeCobra(t *testing.T) {
	f := newBillFixture(t, 1, 1)
	in := f.input("a", 10)
	in.ComandaIDs = []uint{f.comandas[0].ID}
	during(t, func() {
		other := f.input("b", 10)
		other.ComandaIDs = []uint{f.comandas[0].ID}
		if _, err := f.svc.BillTable(other, tax.DefaultConfig()); err != nil {
			t.Fatal(err)
		}
	})
	_, err := f.svc.BillTable(in, tax.DefaultConfig())
	assertStale(t, err)
	if n := f.sales(t); n != 1 {
		t.Fatalf("ventas = %d, quería 1", n)
	}
	// La otra comanda sí se puede cobrar después.
	billAfterReadHook = nil
	rest := f.input("c", 10)
	rest.ComandaIDs = []uint{f.comandas[1].ID}
	if _, err := f.svc.BillTable(rest, tax.DefaultConfig()); err != nil {
		t.Fatalf("la comanda restante debe cobrarse: %v", err)
	}
	if n := f.sales(t); n != 2 {
		t.Fatalf("ventas = %d, quería 2", n)
	}
}

func TestBill_seleccionesDistintasALaVez_ambasSeCobran(t *testing.T) {
	f := newBillFixture(t, 1, 1)
	during(t, func() {
		other := f.input("b", 10)
		other.ComandaIDs = []uint{f.comandas[1].ID}
		if _, err := f.svc.BillTable(other, tax.DefaultConfig()); err != nil {
			t.Fatal(err)
		}
	})
	in := f.input("a", 10)
	in.ComandaIDs = []uint{f.comandas[0].ID}
	if _, err := f.svc.BillTable(in, tax.DefaultConfig()); err != nil {
		t.Fatalf("una selección que nadie más cobró debe pasar: %v", err)
	}
	if n := f.sales(t); n != 2 {
		t.Fatalf("ventas = %d, quería 2", n)
	}
}

func TestBill_cantidadParcialNuncaCobraMasDeLoPedido(t *testing.T) {
	f := newBillFixture(t, 3)
	partial := func(key string) BillInput {
		in := f.input(key, 20)
		in.ComandaSplits = []ComandaSplit{{ComandaID: f.comandas[0].ID, Quantity: 2}}
		return in
	}
	during(t, func() {
		if _, err := f.svc.BillTable(partial("b"), tax.DefaultConfig()); err != nil {
			t.Fatal(err)
		}
	})
	_, err := f.svc.BillTable(partial("a"), tax.DefaultConfig())
	if err == nil {
		t.Fatal("solo quedaba 1 unidad: cobrar 2 más debe rechazarse")
	}
	if n := f.sales(t); n != 1 {
		t.Fatalf("ventas = %d, quería 1", n)
	}
	var billed float64
	f.db.Model(&database.TenantComanda{}).Where("billed_at IS NOT NULL").Select("COALESCE(SUM(quantity),0)").Scan(&billed)
	if billed != 2 {
		t.Fatalf("unidades cobradas = %.0f, quería 2", billed)
	}
}

func TestBill_comandaAgregadaMientrasSeCierraNoSePierde(t *testing.T) {
	f := newBillFixture(t, 1)
	var late database.TenantComanda
	during(t, func() {
		late = database.TenantComanda{SessionID: f.session, ProductCode: "T", ProductName: "Tarde", Quantity: 1, UnitPrice: 5,
			Status: "pendiente", IgvAffectationType: "10"}
		if err := f.db.Create(&late).Error; err != nil {
			t.Fatal(err)
		}
	})
	in := f.input("a", 10)
	in.CloseSession = true
	_, err := f.svc.BillTable(in, tax.DefaultConfig())
	assertStale(t, err)
	var n int64
	f.db.Model(&database.TenantComanda{}).Where("id = ?", late.ID).Count(&n)
	if n != 1 || f.sales(t) != 0 {
		t.Fatalf("la comanda nueva no se pierde y no se emite venta: comanda=%d ventas=%d", n, f.sales(t))
	}
}

func TestBill_comandaAnuladaMientrasSeCobra_seRechaza(t *testing.T) {
	f := newBillFixture(t, 1, 1)
	during(t, func() {
		if err := f.svc.CancelComanda(f.comandas[0].ID, "error", 1); err != nil {
			t.Fatal(err)
		}
	})
	_, err := f.svc.BillTable(f.input("a", 20), tax.DefaultConfig())
	assertStale(t, err)
	if n := f.sales(t); n != 0 {
		t.Fatalf("no debe emitirse venta con una comanda anulada: %d", n)
	}
}

func TestCancelComanda_conDatosViejosNoAnulaLoCobrado(t *testing.T) {
	f := newBillFixture(t, 1, 1)
	stale := f.comandas[0] // leída antes del cobro
	in := f.input("a", 10)
	in.ComandaIDs = []uint{stale.ID}
	if _, err := f.svc.BillTable(in, tax.DefaultConfig()); err != nil {
		t.Fatal(err)
	}
	err := f.db.Transaction(func(tx *gorm.DB) error { return f.svc.cancelComandaTx(tx, &stale, "tarde", 1) })
	if err == nil {
		t.Fatal("no se puede anular una comanda ya cobrada, aunque los datos leídos sean viejos")
	}
	var c database.TenantComanda
	f.db.First(&c, stale.ID)
	if c.CancelledAt != nil {
		t.Fatal("la comanda cobrada quedó además anulada")
	}
}
