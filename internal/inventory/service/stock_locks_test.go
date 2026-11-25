package service

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"tukifac/internal/inventory/reconcile"
	"tukifac/pkg/database"
	"tukifac/pkg/database/tenantmigrations"

	mysqlerr "github.com/go-sql-driver/mysql"
	mysqldrv "gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// F0a-3. Las pruebas de concurrencia necesitan MySQL/MariaDB (SQLite no tiene FOR UPDATE): TUKIFAC_TEST_MYSQL_DSN apuntando a una
// BD desechable cuyo nombre contenga "test" (recomendado: &sql_mode='STRICT_ALL_TABLES'&innodb_lock_wait_timeout=5). Sin esa
// variable se omiten. Las demás corren en SQLite.

func concurrencyDB(t *testing.T, maxOpen int) (*gorm.DB, uint, uint) {
	t.Helper()
	dsn := os.Getenv("TUKIFAC_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("requiere TUKIFAC_TEST_MYSQL_DSN (MySQL/MariaDB)")
	}
	db, err := gorm.Open(mysqldrv.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	var name string
	if err := db.Raw("SELECT DATABASE()").Scan(&name).Error; err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.ToLower(name), "test") {
		t.Fatalf("la prueba borra todas las tablas: solo corre contra una BD cuyo nombre contenga 'test' (es %q)", name)
	}
	var tables []string
	if err := db.Raw("SELECT table_name FROM information_schema.tables WHERE table_schema = DATABASE()").Scan(&tables).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Connection(func(conn *gorm.DB) error {
		if err := conn.Exec("SET FOREIGN_KEY_CHECKS = 0").Error; err != nil {
			return err
		}
		defer conn.Exec("SET FOREIGN_KEY_CHECKS = 1")
		for _, tb := range tables {
			if err := conn.Exec("DROP TABLE `" + tb + "`").Error; err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := database.ApplyBaselineSchema(db); err != nil {
		t.Fatal(err)
	}
	if err := (tenantmigrations.V083InventoryIngressEgress{}).Up(db); err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(maxOpen)
	t.Cleanup(func() { _ = sqlDB.Close() })

	branch := database.TenantBranch{Name: "Principal", Active: true, IsMain: true}
	if err := db.Create(&branch).Error; err != nil {
		t.Fatal(err)
	}
	p := database.TenantProduct{Code: "P1", Name: "Harina", Type: "product", Unit: "NIU", ManageStock: true, Active: true}
	if err := db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	return db, p.ID, branch.ID
}

func newProduct(t *testing.T, db *gorm.DB, code string) uint {
	t.Helper()
	p := database.TenantProduct{Code: code, Name: code, Type: "product", Unit: "NIU", ManageStock: true, Active: true}
	if err := db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	return p.ID
}

func stockOf(t *testing.T, db *gorm.DB, productID, branchID uint) (rows int, qty float64) {
	t.Helper()
	var list []database.TenantProductStock
	if err := db.Where("product_id = ? AND branch_id = ?", productID, branchID).Find(&list).Error; err != nil {
		t.Fatal(err)
	}
	for _, r := range list {
		qty += r.Quantity
	}
	return len(list), qty
}

// parallel lanza n operaciones a la vez (barrera de inicio) y devuelve sus errores.
func parallel(n int, op func(i int) error) []error {
	errs := make([]error, n)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			errs[i] = op(i)
		}(i)
	}
	close(start)
	wg.Wait()
	return errs
}

func move(inv *InventoryService, productID, branchID uint, typ string, qty float64) error {
	return inv.RecordMovement(MovementInput{ProductID: productID, BranchID: branchID, Type: typ, Quantity: qty, Reference: "TEST", UserID: 1})
}

func assertCoherent(t *testing.T, db *gorm.DB) {
	t.Helper()
	rep, err := reconcile.Run(db, reconcile.Options{})
	if err != nil {
		t.Fatal(err)
	}
	s := rep.Summary
	if s.ChainBreaks != 0 || s.Mismatches != 0 || s.DuplicateGroups != 0 || s.NegativeBalances != 0 {
		t.Fatalf("kardex y saldo incoherentes: %+v %+v %+v", s, rep.ChainBreaks, rep.Diffs)
	}
}

func TestConcurrencia_salidasSimultaneasNoPierdenActualizaciones(t *testing.T) {
	db, pid, bid := concurrencyDB(t, 8)
	inv := NewInventoryService(db)
	if err := move(inv, pid, bid, "in", 100); err != nil {
		t.Fatal(err)
	}
	for i, err := range parallel(30, func(int) error { return move(inv, pid, bid, "out", 1) }) {
		if err != nil {
			t.Fatalf("operación %d: %v", i, err)
		}
	}
	if rows, qty := stockOf(t, db, pid, bid); rows != 1 || qty != 70 {
		t.Fatalf("filas=%d saldo=%.3f, quería 1 fila y 70", rows, qty)
	}
	assertCoherent(t, db)
}

func TestConcurrencia_sinStockSuficienteVendeSoloLoQueHay(t *testing.T) {
	db, pid, bid := concurrencyDB(t, 8)
	inv := NewInventoryService(db)
	if err := move(inv, pid, bid, "in", 10); err != nil {
		t.Fatal(err)
	}
	ok, rejected := 0, 0
	for _, err := range parallel(30, func(int) error { return move(inv, pid, bid, "out", 1) }) {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, ErrInsufficientStock):
			rejected++
		default:
			t.Fatalf("error inesperado: %v", err)
		}
	}
	if ok != 10 || rejected != 20 {
		t.Fatalf("ok=%d rechazadas=%d, quería 10 y 20", ok, rejected)
	}
	if _, qty := stockOf(t, db, pid, bid); qty != 0 {
		t.Fatalf("saldo=%.3f, quería 0 (nunca negativo)", qty)
	}
	assertCoherent(t, db)
}

func TestConcurrencia_primerMovimientoSimultaneoCreaUnaSolaFila(t *testing.T) {
	db, pid, bid := concurrencyDB(t, 8)
	if _, err := tenantmigrations.EnsureStockUniqueIndexes(db, true); err != nil {
		t.Fatal(err)
	}
	inv := NewInventoryService(db)
	for i, err := range parallel(20, func(int) error { return move(inv, pid, bid, "in", 1) }) {
		if err != nil {
			t.Fatalf("operación %d: %v", i, err)
		}
	}
	if rows, qty := stockOf(t, db, pid, bid); rows != 1 || qty != 20 {
		t.Fatalf("filas=%d saldo=%.3f, quería 1 y 20", rows, qty)
	}
	assertCoherent(t, db)
}

// Tenant SIN índice único (p. ej. omitido por duplicados): la primera creación simultánea tampoco debe duplicar filas.
func TestConcurrencia_primerMovimientoSinIndiceUnicoTampocoDuplica(t *testing.T) {
	db, pid, bid := concurrencyDB(t, 8)
	inv := NewInventoryService(db)
	for i, err := range parallel(20, func(int) error { return move(inv, pid, bid, "in", 1) }) {
		if err != nil {
			t.Fatalf("operación %d: %v", i, err)
		}
	}
	if rows, qty := stockOf(t, db, pid, bid); rows != 1 || qty != 20 {
		t.Fatalf("filas=%d saldo=%.3f, quería 1 y 20", rows, qty)
	}
	assertCoherent(t, db)
}

func TestConcurrencia_ordenOpuestoConPreBloqueoNoSeInterbloquea(t *testing.T) {
	db, a, bid := concurrencyDB(t, 8)
	b := newProduct(t, db, "P2")
	inv := NewInventoryService(db)
	for _, p := range []uint{a, b} {
		if err := move(inv, p, bid, "in", 1000); err != nil {
			t.Fatal(err)
		}
	}
	// Sin runInventoryTx: aquí no hay reintento, el pre-bloqueo ordenado debe bastar (así se usa en ventas y compras).
	errs := parallel(40, func(i int) error {
		first, second := a, b
		if i%2 == 1 {
			first, second = b, a
		}
		return db.Transaction(func(tx *gorm.DB) error {
			if err := LockStockRowsTx(tx, []StockKey{{ProductID: first, BranchID: bid}, {ProductID: second, BranchID: bid}}); err != nil {
				return err
			}
			for _, p := range []uint{first, second} {
				if err := inv.RecordMovementTx(tx, MovementInput{ProductID: p, BranchID: bid, Type: "out", Quantity: 1, UserID: 1}); err != nil {
					return err
				}
				time.Sleep(2 * time.Millisecond)
			}
			return nil
		})
	})
	for i, err := range errs {
		if err != nil {
			t.Fatalf("operación %d: %v", i, err)
		}
	}
	for _, p := range []uint{a, b} {
		if _, qty := stockOf(t, db, p, bid); qty != 960 {
			t.Fatalf("producto %d saldo=%.3f, quería 960", p, qty)
		}
	}
	assertCoherent(t, db)
}

func TestConcurrencia_poolPequenoNoSeAgota(t *testing.T) {
	db, pid, bid := concurrencyDB(t, 2)
	inv := NewInventoryService(db)
	if err := move(inv, pid, bid, "in", 100); err != nil {
		t.Fatal(err)
	}
	done := make(chan []error, 1)
	go func() { done <- parallel(20, func(int) error { return move(inv, pid, bid, "out", 1) }) }()
	select {
	case errs := <-done:
		for _, err := range errs {
			if err != nil {
				t.Fatal(err)
			}
		}
	case <-time.After(30 * time.Second):
		t.Fatal("20 operaciones con 2 conexiones no terminaron en 30 s")
	}
	assertCoherent(t, db)
}

// Documentos con las mismas líneas en orden opuesto, confirmados a la vez: se completan (pre-bloqueo + reintento).
func TestConcurrencia_documentosEnOrdenOpuesto(t *testing.T) {
	db, a, bid := concurrencyDB(t, 8)
	b := newProduct(t, db, "P2")
	if err := database.SeedInventoryDocumentSeriesForBranch(db, bid); err != nil {
		t.Fatal(err)
	}
	op, err := LookupOperationTypeByCode(db, "INVENTORY_ADJUSTMENT_IN")
	if err != nil {
		t.Fatal(err)
	}
	svc := NewInventoryDocumentService(db)
	var ids []uint
	for i := 0; i < 8; i++ {
		lines := []DocumentLineInput{{ProductID: a, Quantity: 1}, {ProductID: b, Quantity: 1}}
		if i%2 == 1 {
			lines = []DocumentLineInput{lines[1], lines[0]}
		}
		id, err := svc.CreateInventoryDocument(CreateDocumentInput{
			Direction: "IN", OperationTypeID: op.ID, BranchID: bid, DocumentDate: time.Now(),
			MovementReason: "carga", Notes: "carga", Lines: lines, UserID: 1,
		})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	for i, err := range parallel(len(ids), func(i int) error { return svc.ConfirmInventoryDocument(ids[i], 1) }) {
		if err != nil {
			t.Fatalf("documento %d: %v", i, err)
		}
	}
	for _, p := range []uint{a, b} {
		if _, qty := stockOf(t, db, p, bid); qty != 8 {
			t.Fatalf("producto %d saldo=%.3f, quería 8", p, qty)
		}
	}
	assertCoherent(t, db)
}

// ---- Sin concurrencia (SQLite) ----

func TestRecordMovement_allowNegativeApagadoPorDefecto(t *testing.T) {
	db := setupKardexTestDB(t)
	var branch database.TenantBranch
	db.Where("is_main = ?", true).First(&branch)
	pid := newProduct(t, db, "NEG")
	inv := NewInventoryService(db)

	err := move(inv, pid, branch.ID, "out", 3)
	if !errors.Is(err, ErrInsufficientStock) {
		t.Fatalf("por defecto debe rechazar: %v", err)
	}
	if want := "stock insuficiente para «NEG»: requiere 3.00, hay 0.00"; err.Error() != want {
		t.Fatalf("el texto no debe cambiar: %q", err.Error())
	}
	if rows, _ := stockOf(t, db, pid, branch.ID); rows != 0 {
		t.Fatalf("un movimiento rechazado no deja fila de saldo: %d", rows)
	}
	if err := inv.RecordMovement(MovementInput{ProductID: pid, BranchID: branch.ID, Type: "out", Quantity: 3, UserID: 1, AllowNegative: true}); err != nil {
		t.Fatal(err)
	}
	if rows, qty := stockOf(t, db, pid, branch.ID); rows != 1 || qty != -3 {
		t.Fatalf("con AllowNegative: filas=%d saldo=%.3f", rows, qty)
	}
}

func TestRecordMovement_actualizaLaMismaFilaYElInterruptorVuelveAlModoAnterior(t *testing.T) {
	for _, mode := range []string{"", "off"} {
		t.Run("INVENTORY_ROW_LOCKS="+mode, func(t *testing.T) {
			t.Setenv("INVENTORY_ROW_LOCKS", mode)
			db := setupKardexTestDB(t)
			var branch database.TenantBranch
			db.Where("is_main = ?", true).First(&branch)
			pid := newProduct(t, db, "SW"+mode)
			inv := NewInventoryService(db)
			for _, step := range []struct {
				typ string
				q   float64
			}{{"in", 10}, {"out", 4}, {"adjustment", 7}, {"adjustment_out", 7}, {"adjustment_in", 2}} {
				if err := move(inv, pid, branch.ID, step.typ, step.q); err != nil {
					t.Fatalf("%s %v: %v", step.typ, step.q, err)
				}
			}
			if rows, qty := stockOf(t, db, pid, branch.ID); rows != 1 || qty != 2 {
				t.Fatalf("filas=%d saldo=%.3f, quería 1 y 2", rows, qty)
			}
			assertCoherent(t, db)
		})
	}
}

func TestGetKardex_empateDeFechaSeDesempataPorID(t *testing.T) {
	db := setupKardexTestDB(t)
	var branch database.TenantBranch
	db.Where("is_main = ?", true).First(&branch)
	pid := newProduct(t, db, "TIE")
	same := time.Date(2026, 11, 25, 10, 0, 0, 0, time.Local)
	for i := 1; i <= 4; i++ {
		if err := db.Create(&database.TenantStockMovement{ProductID: pid, BranchID: branch.ID, Type: "in", Quantity: 1, Balance: float64(i), CreatedAt: same, UserID: 1}).Error; err != nil {
			t.Fatal(err)
		}
	}
	movs, _, err := NewInventoryService(db).GetKardex(KardexParams{ProductID: pid})
	if err != nil {
		t.Fatal(err)
	}
	got := ""
	for _, m := range movs {
		got += fmt.Sprintf("%.0f,", m.Balance)
	}
	if got != "4,3,2,1," {
		t.Fatalf("orden con la misma fecha: %s", got)
	}
}

func TestRetryOnLockConflict(t *testing.T) {
	calls := 0
	if err := retryOnLockConflict(func() error {
		calls++
		if calls < 3 {
			return fmt.Errorf("x: %w", &mysqlerr.MySQLError{Number: 1213})
		}
		return nil
	}); err != nil || calls != 3 {
		t.Fatalf("deadlock se reintenta: calls=%d err=%v", calls, err)
	}
	calls = 0
	err := retryOnLockConflict(func() error { calls++; return &mysqlerr.MySQLError{Number: 1205} })
	if calls != maxInventoryTxAttempts || !isLockConflict(err) {
		t.Fatalf("se rinde tras %d intentos: calls=%d err=%v", maxInventoryTxAttempts, calls, err)
	}
	calls = 0
	_ = retryOnLockConflict(func() error { calls++; return ErrInsufficientStock })
	if calls != 1 {
		t.Fatalf("otros errores no se reintentan: %d", calls)
	}
}

func TestRunInventoryTx_dentroDeOtraTransaccionNoReintenta(t *testing.T) {
	db := setupKardexTestDB(t)
	calls := 0
	_ = db.Transaction(func(tx *gorm.DB) error {
		return runInventoryTx(tx, func(*gorm.DB) error { calls++; return &mysqlerr.MySQLError{Number: 1213} })
	})
	if calls != 1 {
		t.Fatalf("anidada: un solo intento, hubo %d", calls)
	}
}

func TestSortedTargets_ordenEstableYSinRepetidos(t *testing.T) {
	pres := uint(9)
	got := sortedTargets([]StockKey{
		{ProductID: 5, BranchID: 2}, {ProductID: 1, PresentationID: &pres, BranchID: 1},
		{ProductID: 7, BranchID: 1}, {ProductID: 5, BranchID: 2}, {ProductID: 0, BranchID: 1}, {ProductID: 3, BranchID: 1},
	})
	s := ""
	for _, t := range got {
		s += fmt.Sprintf("%v:%d:%d;", t.byPresentation, t.branchID, t.entityID)
	}
	if s != "false:1:3;false:1:7;false:2:5;true:1:9;" {
		t.Fatalf("orden: %s", s)
	}
}
