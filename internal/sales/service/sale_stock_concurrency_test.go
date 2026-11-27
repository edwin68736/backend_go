package service

import (
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

// F0a-3: ventas simultáneas por el flujo REAL (SaleService.Create) con las mismas líneas en distinto orden y en dos series: el stock
// baja exactamente lo vendido, el kardex queda coherente y no hay bloqueos mutuos. Necesita MySQL/MariaDB: TUKIFAC_TEST_MYSQL_DSN
// (BD desechable cuyo nombre contenga "test"); sin ella se omite.
func TestSaleCreate_ventasSimultaneasNoPierdenStockNiSeInterbloquean(t *testing.T) {
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
	dropAllTables(t, db)
	if err := database.ApplyBaselineSchema(db); err != nil {
		t.Fatal(err)
	}
	if err := (tenantmigrations.V083InventoryIngressEgress{}).Up(db); err != nil {
		t.Fatal(err)
	}
	if _, err := tenantmigrations.EnsureStockUniqueIndexes(db, true); err != nil {
		t.Fatal(err)
	}
	for _, m := range []interface{}{
		&database.TenantSaleCreditInstallment{}, &database.TenantSaleDetraccion{}, &database.TenantProductBranch{},
		&database.TenantProductSaleUnit{}, &database.TenantProductBranchPrice{}, &database.TenantProductSaleUnitBranchPrice{},
		&database.TenantProductAttribute{}, &database.TenantComboGroup{}, &database.TenantComboGroupItem{},
	} {
		if err := db.AutoMigrate(m); err != nil {
			t.Fatal(err)
		}
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(60) // holgado: Create pide conexiones fuera de su transacción (ver F0a-3_INFORME.md)
	t.Cleanup(func() { _ = sqlDB.Close() })

	for _, err := range []error{
		db.Create(&database.TenantCompanyConfig{ID: 1, SunatEnabled: true, TaxRate: 18}).Error,
		db.Create(&database.TenantBranch{Name: "Principal", Active: true, IsMain: true}).Error,
		db.Create(&database.TenantPaymentMethod{Code: "cash", Name: "Efectivo", IsSystem: true, Active: true}).Error,
		db.Create(&database.TenantCashSession{BranchID: 1, UserID: 1, OpenedBy: 1, Status: "open", OpenedAt: time.Now()}).Error,
	} {
		if err != nil {
			t.Fatal(err)
		}
	}
	products := make([]database.TenantProduct, 2)
	for i, code := range []string{"A", "B"} {
		products[i] = database.TenantProduct{Code: code, Name: code, Type: "product", Unit: "NIU", SalePrice: 10,
			IgvAffectationType: "10", PriceIncludesIgv: true, ManageStock: true, BranchID: 1, Active: true}
		if err := db.Create(&products[i]).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&database.TenantProductStock{ProductID: products[i].ID, BranchID: 1, Quantity: 100}).Error; err != nil {
			t.Fatal(err)
		}
		// Movimiento inicial para que la cadena del kardex arranque en 100.
		if err := db.Create(&database.TenantStockMovement{ProductID: products[i].ID, BranchID: 1, Type: "in", Quantity: 100, Balance: 100, UserID: 1}).Error; err != nil {
			t.Fatal(err)
		}
	}
	series := make([]database.TenantDocumentSeries, 2)
	for i, s := range []string{"NV01", "NV02"} {
		series[i] = database.TenantDocumentSeries{BranchID: 1, DocType: "Nota de Venta", SunatCode: "00", Series: s, Correlative: 1, Active: true}
		if err := db.Create(&series[i]).Error; err != nil {
			t.Fatal(err)
		}
	}

	line := func(p database.TenantProduct) SaleItemInput {
		id := p.ID
		return SaleItemInput{ProductID: &id, Description: p.Name, Quantity: 1, UnitPrice: 10, Unit: "NIU", IgvAffectationType: "10", PriceIncludesIgv: true}
	}
	const n = 24
	errs := make([]error, n)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			items := []SaleItemInput{line(products[0]), line(products[1])}
			if i%2 == 1 {
				items = []SaleItemInput{items[1], items[0]}
			}
			_, errs[i] = NewSaleService(db).Create(CreateSaleInput{
				BranchID: 1, UserID: 1, SeriesID: series[i%2].ID, DocType: "00", IssueDate: time.Now(), Currency: "PEN",
				TaxConfig: tax.DefaultConfig(), Payments: []PaymentInput{{Method: "cash", Amount: 20}}, Items: items,
			})
		}(i)
	}
	close(start)
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("venta %d: %v", i, err)
		}
	}
	for _, p := range products {
		var rows []database.TenantProductStock
		db.Where("product_id = ?", p.ID).Find(&rows)
		if len(rows) != 1 || rows[0].Quantity != 100-n {
			t.Fatalf("producto %s: filas=%d saldo=%+v, quería %d", p.Code, len(rows), rows, 100-n)
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

func dropAllTables(t *testing.T, db *gorm.DB) {
	t.Helper()
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
}
