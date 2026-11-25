package service

import (
	"strings"
	"testing"

	"tukifac/pkg/database"

	"gorm.io/gorm"
)

// F2a: un traslado se confirma, se cancela o se anula UNA sola vez, aunque lleguen varias peticiones a la vez.
// Necesita MySQL/MariaDB (TUKIFAC_TEST_MYSQL_DSN); sin ella se omite.

const transferTries = 8

// transferFixture: origen con 100 unidades, destino vacío.
func transferFixture(t *testing.T) (db *gorm.DB, product, from, to uint) {
	t.Helper()
	db, product, from = concurrencyDB(t, 30)
	other := database.TenantBranch{Name: "Destino", Active: true}
	if err := db.Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	if err := NewInventoryService(db).RecordMovement(MovementInput{ProductID: product, BranchID: from, Type: "in", Quantity: 100, UserID: 1}); err != nil {
		t.Fatal(err)
	}
	return db, product, from, other.ID
}

func pendingTransfer(t *testing.T, db *gorm.DB, product, from, to uint) uint {
	t.Helper()
	id, err := NewInventoryService(db).CreateTransferWithLines(from, to, 1, "", []TransferLineInput{{ProductID: product, Quantity: 10}})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func succeeded(errs []error) (n int) {
	for _, err := range errs {
		if err == nil {
			n++
		}
	}
	return n
}

func assertStock(t *testing.T, db *gorm.DB, product, from, to uint, wantFrom, wantTo float64) {
	t.Helper()
	_, a := stockOf(t, db, product, from)
	_, b := stockOf(t, db, product, to)
	if a != wantFrom || b != wantTo {
		t.Fatalf("stock origen=%v destino=%v, quería origen=%v destino=%v", a, b, wantFrom, wantTo)
	}
}

func TestTransfer_confirmarALaVezSumaUnaSolaVez(t *testing.T) {
	db, p, from, to := transferFixture(t)
	for round := 1; round <= 5; round++ {
		id := pendingTransfer(t, db, p, from, to)
		errs := parallel(transferTries, func(int) error { return NewInventoryService(db).ConfirmTransfer(id, 1) })
		if n := succeeded(errs); n != 1 {
			t.Fatalf("ronda %d: %d confirmaciones exitosas, quería 1", round, n)
		}
		assertStock(t, db, p, from, to, 100-float64(10*round), float64(10*round))
	}
}

func TestTransfer_cancelarALaVezDevuelveUnaSolaVez(t *testing.T) {
	db, p, from, to := transferFixture(t)
	for round := 1; round <= 5; round++ {
		id := pendingTransfer(t, db, p, from, to)
		errs := parallel(transferTries, func(int) error { return NewInventoryService(db).CancelTransfer(id, 1) })
		if n := succeeded(errs); n != 1 {
			t.Fatalf("ronda %d: %d cancelaciones exitosas, quería 1", round, n)
		}
		assertStock(t, db, p, from, to, 100, 0)
	}
}

func TestTransfer_confirmarContraCancelar(t *testing.T) {
	db, p, from, to := transferFixture(t)
	var confirmed float64
	for round := 1; round <= 5; round++ {
		id := pendingTransfer(t, db, p, from, to)
		errs := parallel(transferTries, func(i int) error {
			svc := NewInventoryService(db)
			if i%2 == 0 {
				return svc.ConfirmTransfer(id, 1)
			}
			return svc.CancelTransfer(id, 1)
		})
		if n := succeeded(errs); n != 1 {
			t.Fatalf("ronda %d: %d operaciones exitosas, quería 1", round, n)
		}
		var tr database.TenantTransfer
		if err := db.First(&tr, id).Error; err != nil {
			t.Fatal(err)
		}
		if tr.Status == "confirmed" {
			confirmed += 10
		}
		assertStock(t, db, p, from, to, 100-confirmed, confirmed)
	}
}

// legacyTransfer: traslado antiguo (sin cabecera), el único que se puede anular.
func legacyTransfer(t *testing.T, db *gorm.DB, product, from, to uint, qty float64) uint {
	t.Helper()
	if err := NewInventoryService(db).Transfer(product, from, to, qty, 1, ""); err != nil {
		t.Fatal(err)
	}
	var log database.TenantTransferLog
	if err := db.Order("id DESC").First(&log).Error; err != nil {
		t.Fatal(err)
	}
	return log.ID
}

func TestTransfer_anularALaVezRevierteUnaSolaVez(t *testing.T) {
	db, p, from, to := transferFixture(t)
	// El destino tiene stock de sobra: sin la protección, cada anulación repetida seguiría moviendo unidades.
	if err := NewInventoryService(db).RecordMovement(MovementInput{ProductID: p, BranchID: to, Type: "in", Quantity: 100, UserID: 1}); err != nil {
		t.Fatal(err)
	}
	id := legacyTransfer(t, db, p, from, to, 10)
	errs := parallel(transferTries, func(int) error { return NewInventoryService(db).ReverseTransfer(id, 1) })
	if n := succeeded(errs); n != 1 {
		t.Fatalf("%d anulaciones exitosas, quería 1", n)
	}
	assertStock(t, db, p, from, to, 100, 100)
}

// Una línea de un traslado con cabecera no se anula por la vía antigua: movería stock que el destino quizá nunca recibió.
func TestTransfer_noSeAnulaPorLineaUnTrasladoConCabecera(t *testing.T) {
	db, p, from, to := transferFixture(t)
	if err := NewInventoryService(db).RecordMovement(MovementInput{ProductID: p, BranchID: to, Type: "in", Quantity: 50, UserID: 1}); err != nil {
		t.Fatal(err)
	}
	id := pendingTransfer(t, db, p, from, to)
	var log database.TenantTransferLog
	if err := db.Where("transfer_id = ?", id).First(&log).Error; err != nil {
		t.Fatal(err)
	}
	err := NewInventoryService(db).ReverseTransfer(log.ID, 1)
	if err == nil || !strings.Contains(err.Error(), "cancél") {
		t.Fatalf("debía rechazarlo: %v", err)
	}
	assertStock(t, db, p, from, to, 90, 50)
}

// El traslado directo sale y entra en una sola transacción: si la entrada falla, la salida no queda.
func TestTransfer_directoEsAtomico(t *testing.T) {
	db, p, from, to := transferFixture(t)
	if err := db.Exec("CREATE TRIGGER falla_destino BEFORE INSERT ON tenant_stock_movements FOR EACH ROW " +
		"BEGIN IF NEW.type = 'in' AND NEW.reference = 'TRANSFER' THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'fallo en destino'; END IF; END").Error; err != nil {
		t.Skipf("no se pudo crear el disparador de prueba: %v", err)
	}
	defer db.Exec("DROP TRIGGER IF EXISTS falla_destino")
	if err := NewInventoryService(db).Transfer(p, from, to, 10, 1, ""); err == nil {
		t.Fatal("debía fallar")
	}
	assertStock(t, db, p, from, to, 100, 0)
}
