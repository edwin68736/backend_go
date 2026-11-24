package reconcile

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"tukifac/pkg/database"

	"github.com/glebarez/sqlite"
	mysqldrv "gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// F0a-1: el reconciliador es de solo lectura y debe (a) detectar duplicados de saldo, (b) comparar saldo y kardex tratando "adjustment"
// como saldo absoluto, (c) detectar cadenas rotas sin falsos positivos y (d) no escribir nada. Por defecto corre en SQLite; con
// TUKIFAC_TEST_MYSQL_DSN (una BD de pruebas desechable cuyo nombre contenga "test", modo estricto: &sql_mode='STRICT_ALL_TABLES')
// corre contra MySQL/MariaDB, que es la validación que cuenta para producción.

var models = func() []interface{} {
	return []interface{}{&database.TenantProductStock{}, &database.TenantProductPresentationStock{}, &database.TenantStockMovement{}}
}

func openDB(t *testing.T) *gorm.DB {
	t.Helper()
	var db *gorm.DB
	var err error
	if dsn := os.Getenv("TUKIFAC_TEST_MYSQL_DSN"); dsn != "" {
		db, err = gorm.Open(mysqldrv.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
		if err != nil {
			t.Fatal(err)
		}
		var name string
		if err := db.Raw("SELECT DATABASE()").Scan(&name).Error; err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(strings.ToLower(name), "test") {
			t.Fatalf("TUKIFAC_TEST_MYSQL_DSN apunta a %q: la prueba borra tablas y solo corre contra una BD cuyo nombre contenga 'test'", name)
		}
		for _, m := range models() {
			if err := db.Migrator().DropTable(m); err != nil {
				t.Fatal(err)
			}
		}
	} else {
		db, err = gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, m := range models() {
		if err := db.AutoMigrate(m); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

type mov struct {
	product uint
	pres    uint
	branch  uint
	typ     string
	qty     float64
	balance float64
}

func addMovs(t *testing.T, db *gorm.DB, ms ...mov) {
	t.Helper()
	for _, m := range ms {
		row := database.TenantStockMovement{ProductID: m.product, BranchID: m.branch, Type: m.typ, Quantity: m.qty, Balance: m.balance}
		if m.pres != 0 {
			p := m.pres
			row.PresentationID = &p
		}
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
}

func addStock(t *testing.T, db *gorm.DB, product, branch uint, qty float64) uint {
	t.Helper()
	row := database.TenantProductStock{ProductID: product, BranchID: branch, Quantity: qty}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	return row.ID
}

func run(t *testing.T, db *gorm.DB, o Options) *Report {
	t.Helper()
	rep, err := Run(db, o)
	if err != nil {
		t.Fatal(err)
	}
	return rep
}

func TestRun_tenantCoherenteNoReportaNada(t *testing.T) {
	db := openDB(t)
	addMovs(t, db,
		mov{product: 1, branch: 1, typ: "in", qty: 10, balance: 10},
		mov{product: 1, branch: 1, typ: "out", qty: 3, balance: 7},
		mov{product: 1, branch: 1, typ: "in", qty: 5, balance: 12},
	)
	addStock(t, db, 1, 1, 12)
	rep := run(t, db, Options{})
	s := rep.Summary
	if s.DuplicateGroups+s.Mismatches+s.ChainBreaks+s.NegativeBalances+s.ZeroOrNullKeyRows+s.StockWithoutKardex+s.KardexWithoutStock != 0 {
		t.Fatalf("un tenant coherente no debe tener hallazgos: %+v", s)
	}
	if s.Movements != 3 || s.ProductStockRows != 1 {
		t.Fatalf("conteos: %+v", s)
	}
}

func TestRun_detectaDuplicadosSinConsolidar(t *testing.T) {
	db := openDB(t)
	addMovs(t, db, mov{product: 7, branch: 2, typ: "in", qty: 4, balance: 4})
	first := addStock(t, db, 7, 2, 4)
	second := addStock(t, db, 7, 2, 4) // carrera de doble creación: el segundo congelado
	rep := run(t, db, Options{})
	if rep.Summary.DuplicateGroups != 1 || rep.Summary.DuplicateExtraRows != 1 {
		t.Fatalf("duplicados: %+v", rep.Summary)
	}
	d := rep.Duplicates[0]
	if d.WriterRowID != first || len(d.Rows) != 2 || d.Rows[1].ID != second || d.Sum != 8 {
		t.Fatalf("detalle del duplicado: %+v", d)
	}
	if d.KardexBalance == nil || *d.KardexBalance != 4 {
		t.Fatalf("el kardex dice 4: %+v", d.KardexBalance)
	}
	// La fila que escribe (4) coincide con el kardex: no es una diferencia; el duplicado se informa aparte.
	if rep.Summary.Mismatches != 0 {
		t.Fatalf("no debe haber diferencia: %+v", rep.Summary)
	}
	// No se consolidó nada.
	var n int64
	db.Model(&database.TenantProductStock{}).Count(&n)
	if n != 2 {
		t.Fatalf("el reconciliador no debe tocar los saldos: quedan %d filas", n)
	}
}

func TestRun_diferenciaSaldoVsKardex(t *testing.T) {
	db := openDB(t)
	addMovs(t, db, mov{product: 1, branch: 1, typ: "in", qty: 10, balance: 10}, mov{product: 1, branch: 1, typ: "out", qty: 2, balance: 8})
	addStock(t, db, 1, 1, 9) // el saldo no coincide con el kardex
	rep := run(t, db, Options{})
	if rep.Summary.Mismatches != 1 {
		t.Fatalf("%+v", rep.Summary)
	}
	d := rep.Diffs[0]
	if d.Category != DiffMismatch || d.Delta != 1 || d.KardexBalance == nil || *d.KardexBalance != 8 || d.Stock == nil || *d.Stock != 9 {
		t.Fatalf("%+v", d)
	}
}

func TestRun_adjustmentEsSaldoAbsolutoYNoGeneraFalsosPositivos(t *testing.T) {
	db := openDB(t)
	addMovs(t, db,
		mov{product: 1, branch: 1, typ: "in", qty: 10, balance: 10},
		mov{product: 1, branch: 1, typ: "adjustment", qty: 4, balance: 4}, // fija el saldo en 4 (no suma 4)
		mov{product: 1, branch: 1, typ: "out", qty: 1, balance: 3},
		mov{product: 1, branch: 1, typ: "adjustment", qty: 50, balance: 50},    // sube a 50
		mov{product: 1, branch: 1, typ: "adjustment_out", qty: 50, balance: 0}, // ajuste a cero
	)
	addStock(t, db, 1, 1, 0)
	rep := run(t, db, Options{})
	if rep.Summary.ChainBreaks != 0 || rep.Summary.Mismatches != 0 {
		t.Fatalf("un 'adjustment' fija el saldo: no es ruptura ni diferencia: %+v %+v", rep.Summary, rep.ChainBreaks)
	}
}

func TestRun_cadenaRotaSeCuentaUnaVez(t *testing.T) {
	db := openDB(t)
	// Dos salidas concurrentes leyeron el mismo saldo (10): la actualización perdida deja el kardex con el MISMO balance (8) dos veces.
	addMovs(t, db,
		mov{product: 1, branch: 1, typ: "in", qty: 10, balance: 10},
		mov{product: 1, branch: 1, typ: "out", qty: 2, balance: 8},
		mov{product: 1, branch: 1, typ: "out", qty: 2, balance: 8}, // debía ser 6
		mov{product: 1, branch: 1, typ: "out", qty: 1, balance: 7}, // consistente desde el saldo guardado (8 → 7)
	)
	addStock(t, db, 1, 1, 7)
	rep := run(t, db, Options{})
	if rep.Summary.ChainBreaks != 1 {
		t.Fatalf("una sola ruptura (no se arrastra): %+v", rep.Summary)
	}
	b := rep.ChainBreaks[0]
	if b.Previous != 8 || b.Expected != 6 || b.Stored != 8 || b.Type != "out" {
		t.Fatalf("%+v", b)
	}
}

func TestRun_primerMovimientoSinSaldoPrevioEsInformativo(t *testing.T) {
	db := openDB(t)
	// Stock anterior al kardex: el primer movimiento no parte de 0.
	addMovs(t, db, mov{product: 1, branch: 1, typ: "out", qty: 2, balance: 18}, mov{product: 1, branch: 1, typ: "out", qty: 3, balance: 15})
	addStock(t, db, 1, 1, 15)
	rep := run(t, db, Options{})
	if rep.Summary.InitialOffsets != 1 || rep.Summary.ChainBreaks != 0 || rep.Summary.Mismatches != 0 {
		t.Fatalf("%+v", rep.Summary)
	}
}

func TestRun_presentacionesTienenSuPropiaClave(t *testing.T) {
	db := openDB(t)
	addMovs(t, db,
		mov{product: 1, pres: 11, branch: 1, typ: "in", qty: 5, balance: 5},
		mov{product: 1, pres: 12, branch: 1, typ: "in", qty: 3, balance: 3},
	)
	if err := db.Create(&database.TenantProductPresentationStock{PresentationID: 11, BranchID: 1, Quantity: 5}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&database.TenantProductPresentationStock{PresentationID: 12, BranchID: 1, Quantity: 4}).Error; err != nil { // 4 ≠ 3
		t.Fatal(err)
	}
	rep := run(t, db, Options{})
	if rep.Summary.Mismatches != 1 || rep.Diffs[0].Kind != KindPresentation || rep.Diffs[0].EntityID != 12 {
		t.Fatalf("%+v %+v", rep.Summary, rep.Diffs)
	}
	if rep.Summary.PresentationStockRows != 2 {
		t.Fatalf("%+v", rep.Summary)
	}
}

func TestRun_sinKardexSinStockNegativosYClavesNulas(t *testing.T) {
	db := openDB(t)
	addStock(t, db, 1, 1, 20)                                                 // stock previo al kardex
	addMovs(t, db, mov{product: 2, branch: 1, typ: "in", qty: 6, balance: 6}) // kardex sin fila de saldo
	addStock(t, db, 3, 1, -2)                                                 // saldo negativo
	addMovs(t, db, mov{product: 3, branch: 1, typ: "in", qty: 1, balance: 1}, mov{product: 3, branch: 1, typ: "out", qty: 3, balance: -2})
	addStock(t, db, 0, 1, 5) // clave con producto 0
	rep := run(t, db, Options{})
	s := rep.Summary
	if s.StockWithoutKardex != 1 || s.KardexWithoutStock != 1 || s.NegativeBalances != 1 || s.ZeroOrNullKeyRows != 1 {
		t.Fatalf("%+v", s)
	}
}

func TestRun_filtrosProductoYSucursal(t *testing.T) {
	db := openDB(t)
	addMovs(t, db, mov{product: 1, branch: 1, typ: "in", qty: 1, balance: 1}, mov{product: 2, branch: 2, typ: "in", qty: 1, balance: 1})
	addStock(t, db, 1, 1, 9) // diferencia
	addStock(t, db, 2, 2, 9) // diferencia
	if n := run(t, db, Options{ProductID: 1}).Summary.Mismatches; n != 1 {
		t.Fatalf("producto 1: %d", n)
	}
	if n := run(t, db, Options{BranchID: 2}).Summary.Mismatches; n != 1 {
		t.Fatalf("sucursal 2: %d", n)
	}
	if n := run(t, db, Options{}).Summary.Mismatches; n != 2 {
		t.Fatalf("sin filtro: %d", n)
	}
}

func TestRun_maxDetailTruncaElDetalleNoLosConteos(t *testing.T) {
	db := openDB(t)
	for p := uint(1); p <= 5; p++ {
		addMovs(t, db, mov{product: p, branch: 1, typ: "in", qty: 1, balance: 1})
		addStock(t, db, p, 1, 9)
	}
	rep := run(t, db, Options{MaxDetail: 2})
	if rep.Summary.Mismatches != 5 || len(rep.Diffs) != 2 || !rep.Truncated {
		t.Fatalf("conteo exacto y detalle acotado: %d diffs=%d truncated=%v", rep.Summary.Mismatches, len(rep.Diffs), rep.Truncated)
	}
}

func TestRun_tablasAusentesSeOmiteSinError(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:reconcile_vacio?mode=memory&cache=shared"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	rep, err := Run(db, Options{})
	if err != nil || rep.Skipped == "" {
		t.Fatalf("debe omitirse con motivo: %v %+v", err, rep)
	}
}

// Prueba de "solo lectura": con la sesión en modo solo lectura el reconciliador funciona (solo hace SELECT) y los datos no cambian.
func TestRun_funcionaConLaBaseEnSoloLecturaYNoModificaNada(t *testing.T) {
	db := openDB(t)
	addMovs(t, db, mov{product: 1, branch: 1, typ: "in", qty: 10, balance: 10}, mov{product: 1, branch: 1, typ: "out", qty: 2, balance: 9})
	addStock(t, db, 1, 1, 8)
	addStock(t, db, 1, 1, 8)

	snapshot := func() string {
		var st []database.TenantProductStock
		var mv []database.TenantStockMovement
		db.Order("id").Find(&st)
		db.Order("id").Find(&mv)
		return fmt.Sprintf("%v|%v", st, mv)
	}
	before := snapshot()

	err := db.Connection(func(conn *gorm.DB) error {
		if db.Dialector.Name() == "mysql" {
			if err := conn.Exec("SET SESSION TRANSACTION READ ONLY").Error; err != nil {
				return err
			}
			// La propia sesión rechaza escrituras: demuestra que cualquier INSERT/UPDATE del reconciliador fallaría.
			if err := conn.Exec("UPDATE tenant_product_stocks SET quantity = 99").Error; err == nil {
				t.Fatal("la sesión de solo lectura debió rechazar un UPDATE")
			}
		} else {
			if err := conn.Exec("PRAGMA query_only = ON").Error; err != nil {
				return err
			}
			defer conn.Exec("PRAGMA query_only = OFF")
		}
		if _, err := Run(conn, Options{}); err != nil {
			t.Fatalf("Run falló con la sesión en solo lectura: %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if after := snapshot(); after != before {
		t.Fatalf("el reconciliador modificó datos:\nantes=%s\ndespués=%s", before, after)
	}
}
