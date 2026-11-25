package tenantmigrations

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"strings"
	"testing"

	"tukifac/pkg/database"

	"github.com/glebarez/sqlite"
	mysqldrv "gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// F0a-2. Por defecto corren en SQLite; con TUKIFAC_TEST_MYSQL_DSN (BD desechable cuyo nombre contenga "test", modo estricto:
// &sql_mode='STRICT_ALL_TABLES') corren contra MySQL/MariaDB, que es la validación que cuenta para el ALTER y el índice.

func f0aDB(t *testing.T) *gorm.DB {
	t.Helper()
	cfg := &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)}
	var db *gorm.DB
	var err error
	if dsn := os.Getenv("TUKIFAC_TEST_MYSQL_DSN"); dsn != "" {
		db, err = gorm.Open(mysqldrv.Open(dsn), cfg)
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
		for _, table := range []string{"tenant_product_stocks", "tenant_product_presentation_stocks", "tenant_inventory_document_details"} {
			if err := db.Exec("DROP TABLE IF EXISTS " + table).Error; err != nil {
				t.Fatal(err)
			}
		}
	} else {
		db, err = gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), cfg)
		if err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func stockTables(t *testing.T) *gorm.DB {
	t.Helper()
	db := f0aDB(t)
	if err := db.AutoMigrate(&database.TenantProductStock{}, &database.TenantProductPresentationStock{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func dbExec(t *testing.T, db *gorm.DB, sql string, args ...interface{}) error {
	t.Helper()
	return db.Exec(sql, args...).Error
}

const insStock = "INSERT INTO tenant_product_stocks (product_id, branch_id, quantity) VALUES (?, ?, ?)"
const insPres = "INSERT INTO tenant_product_presentation_stocks (presentation_id, branch_id, quantity) VALUES (?, ?, ?)"

func TestV193_creaIndiceSinDuplicadosYEsIdempotente(t *testing.T) {
	db := stockTables(t)
	for _, err := range []error{
		dbExec(t, db, insStock, 1, 1, 5), dbExec(t, db, insStock, 1, 2, 5), dbExec(t, db, insStock, 2, 1, 5),
		dbExec(t, db, insPres, 7, 1, 3),
	} {
		if err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 2; i++ {
		if err := (V193StockUniqueIndexes{}).Up(db); err != nil {
			t.Fatalf("pasada %d: %v", i, err)
		}
	}
	if !db.Migrator().HasIndex("tenant_product_stocks", idxProductStockUnique) ||
		!db.Migrator().HasIndex("tenant_product_presentation_stocks", idxPresentationStockUnique) {
		t.Fatal("faltan los índices únicos")
	}
	if err := dbExec(t, db, insStock, 1, 1, 9); err == nil || !isDuplicateKeyError(err) {
		t.Fatalf("una segunda fila (producto, sucursal) debe rechazarse como duplicada: %v", err)
	}
	if err := dbExec(t, db, insPres, 7, 1, 9); err == nil || !isDuplicateKeyError(err) {
		t.Fatalf("una segunda fila (presentación, sucursal) debe rechazarse: %v", err)
	}
	if err := dbExec(t, db, insStock, 1, 3, 1); err != nil {
		t.Fatalf("otra sucursal sí puede: %v", err)
	}
}

func TestV193_conDuplicadosOmiteRegistraElMotivoYNoTocaSaldos(t *testing.T) {
	db := stockTables(t)
	for _, err := range []error{
		dbExec(t, db, insStock, 5, 2, 4), dbExec(t, db, insStock, 5, 2, 4), // duplicado de producto
		dbExec(t, db, insStock, 6, 1, 1),
		dbExec(t, db, insPres, 7, 1, 3), // las presentaciones están limpias
	} {
		if err != nil {
			t.Fatal(err)
		}
	}
	snapshot := func() string {
		var rows []database.TenantProductStock
		db.Order("id").Find(&rows)
		s := ""
		for _, r := range rows {
			s += fmt.Sprintf("%d:%d:%d:%.3f;", r.ID, r.ProductID, r.BranchID, r.Quantity)
		}
		return s
	}
	before := snapshot()

	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)
	if err := (V193StockUniqueIndexes{}).Up(db); err != nil {
		t.Fatalf("con duplicados la migración no debe fallar: %v", err)
	}
	if db.Migrator().HasIndex("tenant_product_stocks", idxProductStockUnique) {
		t.Fatal("no debe crearse el índice con duplicados")
	}
	if !db.Migrator().HasIndex("tenant_product_presentation_stocks", idxPresentationStockUnique) {
		t.Fatal("la tabla limpia sí debe recibir su índice")
	}
	if after := snapshot(); after != before {
		t.Fatalf("no debe modificar saldos: %s -> %s", before, after)
	}
	out := buf.String()
	for _, want := range []string{"ÍNDICE OMITIDO", idxProductStockUnique, "1 grupos duplicados", "producto=5 sucursal=2 filas=2"} {
		if !strings.Contains(out, want) {
			t.Fatalf("el log debe explicar el motivo; falta %q en:\n%s", want, out)
		}
	}
	// El tenant sigue funcionando sin índice (puede seguir insertando como antes).
	if err := dbExec(t, db, insStock, 9, 1, 1); err != nil {
		t.Fatal(err)
	}
}

func TestStockUnique_consultaNoCreaYApplyCreaTrasLimpiar(t *testing.T) {
	db := stockTables(t)
	if err := dbExec(t, db, insStock, 5, 2, 4); err != nil {
		t.Fatal(err)
	}
	if err := dbExec(t, db, insStock, 5, 2, 4); err != nil {
		t.Fatal(err)
	}
	st, err := EnsureStockUniqueIndexes(db, false)
	if err != nil {
		t.Fatal(err)
	}
	if st[0].DuplicateGroups != 1 || st[0].Exists || len(st[0].Samples) != 1 {
		t.Fatalf("consulta: %+v", st[0])
	}
	if st[1].Exists || st[1].DuplicateGroups != 0 {
		t.Fatalf("sin apply no se crea nada: %+v", st[1])
	}
	if db.Migrator().HasIndex("tenant_product_presentation_stocks", idxPresentationStockUnique) {
		t.Fatal("la consulta no debe crear índices")
	}
	// Alguien limpia el duplicado a mano y vuelve a ejecutar con apply.
	if err := dbExec(t, db, "DELETE FROM tenant_product_stocks WHERE id = (SELECT m FROM (SELECT MAX(id) AS m FROM tenant_product_stocks) x)"); err != nil {
		t.Fatal(err)
	}
	st, err = EnsureStockUniqueIndexes(db, true)
	if err != nil {
		t.Fatal(err)
	}
	if !st[0].Created || !st[0].Exists || !st[1].Created {
		t.Fatalf("apply tras limpiar: %+v", st)
	}
	st, err = EnsureStockUniqueIndexes(db, true)
	if err != nil || st[0].Created || !st[0].Exists {
		t.Fatalf("segunda vez: ya existe, nada que crear: %v %+v", err, st)
	}
}

func TestV193_tablasAusentesNoFalla(t *testing.T) {
	db := f0aDB(t)
	if err := (V193StockUniqueIndexes{}).Up(db); err != nil {
		t.Fatal(err)
	}
}

func TestV194_ampliaUnitCostConservandoNullYDatos(t *testing.T) {
	db := f0aDB(t)
	if db.Dialector.Name() != "mysql" {
		if err := (V194InventoryDocumentUnitCost{}).Up(db); err != nil {
			t.Fatalf("en SQLite no hace nada: %v", err)
		}
		t.Skip("el ALTER solo aplica en MySQL/MariaDB (TUKIFAC_TEST_MYSQL_DSN)")
	}
	if err := db.Exec(`CREATE TABLE tenant_inventory_document_details (id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY, unit_cost DECIMAL(15,2) NULL)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO tenant_inventory_document_details (unit_cost) VALUES (3.5), (NULL), (0.0042)`).Error; err != nil {
		t.Fatal(err)
	}
	var truncated float64
	db.Raw("SELECT unit_cost FROM tenant_inventory_document_details WHERE id = 3").Scan(&truncated)
	if truncated != 0 {
		t.Fatalf("antes de V194, 0.0042 se guarda como 0.00 (fue %v)", truncated)
	}
	for i := 0; i < 2; i++ {
		if err := (V194InventoryDocumentUnitCost{}).Up(db); err != nil {
			t.Fatalf("pasada %d: %v", i, err)
		}
	}
	var col struct {
		NumericScale int
		IsNullable   string
	}
	db.Raw(`SELECT NUMERIC_SCALE AS numeric_scale, IS_NULLABLE AS is_nullable FROM information_schema.columns
		WHERE table_schema = DATABASE() AND table_name='tenant_inventory_document_details' AND column_name='unit_cost'`).Scan(&col)
	if col.NumericScale != 6 || col.IsNullable != "YES" {
		t.Fatalf("columna: escala=%d nullable=%s", col.NumericScale, col.IsNullable)
	}
	if err := db.Exec(`INSERT INTO tenant_inventory_document_details (unit_cost) VALUES (0.0042)`).Error; err != nil {
		t.Fatal(err)
	}
	var got, old float64
	db.Raw("SELECT unit_cost FROM tenant_inventory_document_details ORDER BY id DESC LIMIT 1").Scan(&got)
	db.Raw("SELECT unit_cost FROM tenant_inventory_document_details WHERE id = 1").Scan(&old)
	if got != 0.0042 || old != 3.5 {
		t.Fatalf("nuevo=%v (quería 0.0042), existente=%v (quería 3.5)", got, old)
	}
	var nulls int64
	db.Raw("SELECT COUNT(*) FROM tenant_inventory_document_details WHERE unit_cost IS NULL").Scan(&nulls)
	if nulls != 1 {
		t.Fatalf("el NULL debe conservarse: %d", nulls)
	}
}
