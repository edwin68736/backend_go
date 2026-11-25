package service

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"tukifac/internal/inventory/reconcile"
	"tukifac/pkg/database"
	"tukifac/pkg/database/tenantmigrations"
	"tukifac/pkg/tax"

	mysqldrv "gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// F0a-3: cobros de mesa simultáneos (BillTable, flujo real) que comparten platos en distinto orden. Necesita MySQL/MariaDB:
// TUKIFAC_TEST_MYSQL_DSN (BD desechable cuyo nombre contenga "test"); sin ella se omite.

func billConcurrencyDB(t *testing.T) *gorm.DB {
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
	if _, err := tenantmigrations.EnsureStockUniqueIndexes(db, true); err != nil {
		t.Fatal(err)
	}
	for _, m := range []interface{}{
		&database.TenantBranchDailyComandaCounter{}, &database.TenantSaleCreditInstallment{}, &database.TenantSaleDetraccion{},
		&database.TenantProductBranch{}, &database.TenantProductBranchPrice{}, &database.TenantProductSaleUnit{},
		&database.TenantProductSaleUnitBranchPrice{}, &database.TenantProductAttribute{}, &database.TenantProductModifierGroup{},
		&database.TenantComboGroup{}, &database.TenantComboGroupItem{},
	} {
		if err := db.AutoMigrate(m); err != nil {
			t.Fatal(err)
		}
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(60) // BillTable pide conexiones fuera de su transacción (ver F0a-3_INFORME.md §4)
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

func billProducts(t *testing.T, db *gorm.DB, stock float64) []database.TenantProduct {
	t.Helper()
	out := make([]database.TenantProduct, 2)
	for i, code := range []string{"A", "B"} {
		out[i] = database.TenantProduct{Code: code, Name: code, Type: "product", Unit: "NIU", SalePrice: 10, IgvAffectationType: "10",
			PriceIncludesIgv: true, ManageStock: true, IsRestaurant: true, BranchID: 1, Active: true}
		if err := db.Create(&out[i]).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&database.TenantProductStock{ProductID: out[i].ID, BranchID: 1, Quantity: stock}).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&database.TenantStockMovement{ProductID: out[i].ID, BranchID: 1, Type: "in", Quantity: stock, Balance: stock, UserID: 1}).Error; err != nil {
			t.Fatal(err)
		}
	}
	return out
}

func billTableWith(t *testing.T, db *gorm.DB, svc *RestaurantService, n int, ordered []database.TenantProduct) uint {
	t.Helper()
	var floor database.TenantRestaurantFloor
	if err := db.Where(database.TenantRestaurantFloor{BranchID: 1, Name: "Sala"}).Attrs(database.TenantRestaurantFloor{Active: true}).
		FirstOrCreate(&floor).Error; err != nil {
		t.Fatal(err)
	}
	table := database.TenantRestaurantTable{BranchID: 1, FloorID: floor.ID, Name: fmt.Sprintf("M%d", n), Capacity: 4, Status: "libre", Active: true}
	if err := db.Create(&table).Error; err != nil {
		t.Fatal(err)
	}
	sess, err := svc.OpenTableExtended(openInput(table.ID))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range ordered {
		id := p.ID
		c := database.TenantComanda{SessionID: sess.ID, ProductID: &id, ProductCode: p.Code, ProductName: p.Name, Quantity: 1,
			UnitPrice: 10, Status: "pendiente", IgvAffectationType: "10", PriceIncludesIgv: true}
		if err := db.Create(&c).Error; err != nil {
			t.Fatal(err)
		}
	}
	return sess.ID
}

func TestBillTable_cobrosSimultaneosDeVariasMesasNoPierdenStock(t *testing.T) {
	db := billConcurrencyDB(t)
	products := billProducts(t, db, 100)
	svc := New(db)
	series := make([]database.TenantDocumentSeries, 2)
	for i, s := range []string{"B001", "B002"} {
		series[i] = database.TenantDocumentSeries{BranchID: 1, DocType: "Boleta", SunatCode: "03", Series: s, Correlative: 1, Active: true}
		if err := db.Create(&series[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	const n = 16
	sessions := make([]uint, n)
	for i := range sessions {
		ordered := []database.TenantProduct{products[0], products[1]}
		if i%2 == 1 {
			ordered = []database.TenantProduct{products[1], products[0]}
		}
		sessions[i] = billTableWith(t, db, svc, i, ordered)
	}
	errs := make([]error, n)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, errs[i] = New(db).BillTable(BillInput{SessionID: sessions[i], UserID: 1, SeriesID: series[i%2].ID, DocType: "03",
				IssueDate: time.Now(), CloseSession: true, Payments: []PaymentInput{{Method: "cash", Amount: 20}}}, tax.DefaultConfig())
		}(i)
	}
	close(start)
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("cobro %d: %v", i, err)
		}
	}
	for _, p := range products {
		var rows []database.TenantProductStock
		db.Where("product_id = ?", p.ID).Find(&rows)
		if len(rows) != 1 || rows[0].Quantity != 100-n {
			t.Fatalf("producto %s: %+v, quería una fila con %d", p.Code, rows, 100-n)
		}
	}
	rep, err := reconcile.Run(db, reconcile.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if s := rep.Summary; s.ChainBreaks != 0 || s.Mismatches != 0 || s.DuplicateGroups != 0 {
		t.Fatalf("incoherencias: %+v %+v", s, rep.ChainBreaks)
	}
}
