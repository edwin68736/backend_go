package service

import (
	"os"
	"strings"
	"testing"
	"time"

	"tukifac/pkg/database"
	"tukifac/pkg/database/tenantmigrations"
	"tukifac/pkg/tax"

	mysqldrv "gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// F0b con concurrencia real (MySQL/MariaDB): TUKIFAC_TEST_MYSQL_DSN apuntando a una BD desechable cuyo nombre contenga "test";
// sin esa variable se omite.

func exactnessMySQL(t *testing.T) *gorm.DB {
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
	if err := (tenantmigrations.V150SaleIdempotencyKey{}).Up(db); err != nil {
		t.Fatal(err)
	}
	for _, m := range []interface{}{
		&database.TenantBranchDailyComandaCounter{}, &database.TenantSaleCreditInstallment{}, &database.TenantSaleDetraccion{},
		&database.TenantProductBranch{}, &database.TenantProductBranchPrice{},
	} {
		if err := db.AutoMigrate(m); err != nil {
			t.Fatal(err)
		}
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(30) // el cobro pide una segunda conexión dentro de su transacción (hallazgo aparte)
	t.Cleanup(func() { _ = sqlDB.Close() })
	for _, err := range []error{
		db.Create(&database.TenantCompanyConfig{ID: 1, SunatEnabled: true, TaxRate: 18}).Error,
		db.Create(&database.TenantBranch{Name: "Principal", Active: true, IsMain: true}).Error,
	} {
		if err != nil {
			t.Fatal(err)
		}
	}
	seedCashSessionForBilling(t, db)
	return db
}

// mysqlTable crea una mesa con `n` comandas y devuelve la sesión y las comandas.
func mysqlTable(t *testing.T, db *gorm.DB, name string, n int) (uint, []database.TenantComanda) {
	t.Helper()
	var floor database.TenantRestaurantFloor
	if err := db.Where(database.TenantRestaurantFloor{BranchID: 1, Name: "Sala"}).Attrs(database.TenantRestaurantFloor{Active: true}).
		FirstOrCreate(&floor).Error; err != nil {
		t.Fatal(err)
	}
	table := database.TenantRestaurantTable{BranchID: 1, FloorID: floor.ID, Name: name, Capacity: 4, Status: "libre", Active: true}
	if err := db.Create(&table).Error; err != nil {
		t.Fatal(err)
	}
	sess, err := New(db).OpenTableExtended(openInput(table.ID))
	if err != nil {
		t.Fatal(err)
	}
	var out []database.TenantComanda
	for i := 0; i < n; i++ {
		c := database.TenantComanda{SessionID: sess.ID, ProductCode: "P", ProductName: "Plato", Quantity: 1, UnitPrice: 10,
			Status: "pendiente", IgvAffectationType: "10", PriceIncludesIgv: true}
		if err := db.Create(&c).Error; err != nil {
			t.Fatal(err)
		}
		out = append(out, c)
	}
	return sess.ID, out
}

func TestMySQL_dosCajasMismaMesaSinCerrar_unaSolaVenta(t *testing.T) {
	db := exactnessMySQL(t)
	series := database.TenantDocumentSeries{BranchID: 1, DocType: "Boleta", SunatCode: "03", Series: "B001", Correlative: 1, Active: true}
	if err := db.Create(&series).Error; err != nil {
		t.Fatal(err)
	}
	const trials = 15
	for trial := 0; trial < trials; trial++ {
		sess, _ := mysqlTable(t, db, "M"+string(rune('A'+trial)), 2)
		errs := make(chan error, 2)
		start := make(chan struct{})
		for _, key := range []string{"caja-1", "caja-2"} {
			go func(key string) {
				<-start
				_, err := New(db).BillTable(BillInput{SessionID: sess, UserID: 1, SeriesID: series.ID, DocType: "03", IssueDate: time.Now(),
					IdempotencyKey: key, Payments: []PaymentInput{{Method: "cash", Amount: 20}}}, tax.DefaultConfig())
				errs <- err
			}(key + "-" + string(rune('A'+trial)))
		}
		close(start)
		e1, e2 := <-errs, <-errs
		if (e1 == nil) == (e2 == nil) {
			t.Fatalf("intento %d: exactamente uno debe cobrar (err1=%v err2=%v)", trial, e1, e2)
		}
		var sales int64
		db.Model(&database.TenantSale{}).Where("restaurant_session_id = ?", sess).Count(&sales)
		if sales != 1 {
			t.Fatalf("intento %d: ventas=%d, quería 1 (err1=%v err2=%v)", trial, sales, e1, e2)
		}
	}
}

// Cobrar y anular una comanda a la vez: nunca queda cobrada y anulada, y no se bloquean mutuamente.
func TestMySQL_cobrarYAnularALaVez(t *testing.T) {
	db := exactnessMySQL(t)
	series := database.TenantDocumentSeries{BranchID: 1, DocType: "Boleta", SunatCode: "03", Series: "B001", Correlative: 1, Active: true}
	if err := db.Create(&series).Error; err != nil {
		t.Fatal(err)
	}
	for trial := 0; trial < 15; trial++ {
		sess, comandas := mysqlTable(t, db, "C"+string(rune('A'+trial)), 2)
		svc := New(db)
		billErr := make(chan error, 1)
		cancelErr := make(chan error, 1)
		start := make(chan struct{})
		go func() {
			<-start
			_, err := New(db).BillTable(BillInput{SessionID: sess, UserID: 1, SeriesID: series.ID, DocType: "03", IssueDate: time.Now(),
				Payments: []PaymentInput{{Method: "cash", Amount: 20}}}, tax.DefaultConfig())
			billErr <- err
		}()
		go func() {
			<-start
			cancelErr <- svc.CancelComanda(comandas[0].ID, "prueba", 1)
		}()
		close(start)
		be, ce := <-billErr, <-cancelErr
		for _, err := range []error{be, ce} {
			if err != nil && (strings.Contains(err.Error(), "1213") || strings.Contains(err.Error(), "1205")) {
				t.Fatalf("intento %d: bloqueo mutuo: %v", trial, err)
			}
		}
		var both int64
		db.Model(&database.TenantComanda{}).Where("id = ? AND billed_at IS NOT NULL AND cancelled_at IS NOT NULL", comandas[0].ID).Count(&both)
		if both != 0 {
			t.Fatalf("intento %d: comanda cobrada y anulada (cobro=%v anulación=%v)", trial, be, ce)
		}
		// Ambas pueden tener éxito solo si la anulación terminó antes de que el cobro leyera las comandas: entonces la venta NO
		// incluye el plato anulado (10 de la otra comanda, no 20).
		if be == nil && ce == nil {
			var total float64
			db.Model(&database.TenantSale{}).Where("restaurant_session_id = ?", sess).Select("total").Scan(&total)
			if total != 10 {
				t.Fatalf("intento %d: la venta incluye el plato anulado: total=%.2f", trial, total)
			}
		}
	}
}
