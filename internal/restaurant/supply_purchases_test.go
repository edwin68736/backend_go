package restaurant

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	productsvc "tukifac/internal/products/service"
	purchasesvc "tukifac/internal/purchases/service"
	"tukifac/internal/restaurant/handler"
	"tukifac/pkg/database"
	"tukifac/pkg/database/tenantmigrations"
	"tukifac/pkg/middleware"
	"tukifac/pkg/tax"

	"github.com/glebarez/sqlite"
	"github.com/gofiber/fiber/v3"
	mysqldrv "gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// F3a: compras de insumos, proveedores, carga inicial e ingreso/merma por las rutas de Tukichef. SQLite y, con
// TUKIFAC_TEST_MYSQL_DSN (BD desechable cuyo nombre contenga "test"), MySQL/MariaDB con simultaneidad real.

type buyFixture struct {
	t      *testing.T
	app    *fiber.App
	db     *gorm.DB
	supply uint
	dish   uint
	unit   uint // unidad de compra «Saco 50»
	mysql  bool
}

func buyDB(t *testing.T) (*gorm.DB, bool) {
	t.Helper()
	cfg := &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)}
	dsn := os.Getenv("TUKIFAC_TEST_MYSQL_DSN")
	if dsn == "" {
		db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), cfg)
		if err != nil {
			t.Fatal(err)
		}
		return db, false
	}
	db, err := gorm.Open(mysqldrv.Open(dsn), cfg)
	if err != nil {
		t.Fatal(err)
	}
	var name string
	db.Raw("SELECT DATABASE()").Scan(&name)
	if !strings.Contains(strings.ToLower(name), "test") {
		t.Fatalf("la prueba borra todas las tablas: BD %q", name)
	}
	var tables []string
	db.Raw("SELECT table_name FROM information_schema.tables WHERE table_schema = DATABASE()").Scan(&tables)
	if err := db.Connection(func(conn *gorm.DB) error {
		conn.Exec("SET FOREIGN_KEY_CHECKS = 0")
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
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(40)
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db, true
}

func ok(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func setupBuy(t *testing.T) *buyFixture {
	t.Helper()
	db, isMySQL := buyDB(t)
	ok(t, database.ApplyBaselineSchema(db))
	for _, m := range []interface {
		Up(*gorm.DB) error
	}{tenantmigrations.V083InventoryIngressEgress{}, tenantmigrations.V084InventoryOperationTransfer{}, tenantmigrations.V085InventoryDocumentSource{},
		tenantmigrations.V150SaleIdempotencyKey{}} {
		ok(t, m.Up(db))
	}
	for _, m := range []interface{}{&database.TenantRestaurantSetting{}, &database.TenantRestaurantStaff{}, &database.TenantProduct{},
		&database.TenantProductStock{}, &database.TenantStockMovement{}, &database.TenantProductSaleUnit{}, &database.TenantProductBranch{},
		&database.TenantProductAttribute{}, &database.TenantProductModifierGroup{}, &database.TenantComboGroup{}, &database.TenantComboGroupItem{},
		&database.TenantContact{}, &database.TenantContactPerson{}, &database.TenantPurchase{}, &database.TenantPurchaseItem{},
		&database.TenantTransferLog{}} {
		ok(t, db.AutoMigrate(m))
	}
	ok(t, (tenantmigrations.V197PurchaseIdempotencyKey{}).Up(db))
	ok(t, database.SeedInventoryOperationTypes(db))
	ok(t, db.Create(&database.TenantCompanyConfig{ID: 1, SunatEnabled: true, TaxRate: 18}).Error)
	ok(t, db.Create(&database.TenantBranch{Name: "Principal", Active: true, IsMain: true}).Error)
	ok(t, db.Where(database.TenantPaymentMethod{Code: "cash"}).Attrs(database.TenantPaymentMethod{Name: "Efectivo", IsSystem: true, Active: true, DestinationType: "cash"}).
		FirstOrCreate(&database.TenantPaymentMethod{}).Error)
	ok(t, db.Create(&database.TenantCashSession{BranchID: 1, UserID: 2, OpenedBy: 2, Status: "open", OpenedAt: time.Now()}).Error)
	for id, kind := range map[uint]string{2: "supervisor", 3: "waiter", 4: "supervisor", 5: "cook"} {
		ok(t, db.Create(&database.TenantRestaurantStaff{UserID: id, EmployeeType: kind, DisplayName: kind, IsActive: true}).Error)
	}
	ok(t, database.SetRecipesEnabled(db, true))

	f := &buyFixture{t: t, db: db, mysql: isMySQL}
	ps := productsvc.NewProductService(db)
	supply, _, err := ps.Create(productsvc.ProductInput{IsSupply: true, Code: "ARROZ", Name: "Arroz", Unit: "KGM", IgvAffectationType: "10", Active: true})
	ok(t, err)
	dish, _, err := ps.Create(productsvc.ProductInput{Code: "PLATO", Name: "Lomo", Unit: "NIU", SalePrice: 20, ManageStock: true, Active: true})
	ok(t, err)
	var bag database.TenantUnit
	ok(t, db.Where(database.TenantUnit{Code: "BG"}).Attrs(database.TenantUnit{Name: "Bolsa", Active: true}).FirstOrCreate(&bag).Error)
	su, err := ps.CreateSaleUnit(supply.ID, productsvc.SaleUnitInput{Name: "Saco 50", UnitID: &bag.ID, ConversionFactor: 50, Active: true})
	ok(t, err)
	f.supply, f.dish, f.unit = supply.ID, dish.ID, su.ID

	f.app = fiber.New()
	slug := fmt.Sprintf("%s-%d", t.Name(), time.Now().UnixNano())
	f.app.Use(func(c fiber.Ctx) error {
		uids := map[string]uint{"supervisor": 2, "mozo": 3, "sincaja": 4, "sinmodulo": 2, "cocinero": 5}
		uid := uids[c.Get("X-U")]
		modules := []string{"restaurant", "purchases", "cashbank"}
		if c.Get("X-U") == "sinmodulo" {
			modules = []string{"restaurant", "cashbank"}
		}
		c.Locals("tenantDB", db)
		c.Locals("user_id", uid)
		c.Locals("active_branch_id", uint(1))
		c.Locals("allowed_branch_ids", []uint{1})
		c.Locals("tenant_claims", &middleware.TenantClaims{UserID: uid, TenantSlug: slug, TenantID: 7, Modules: modules})
		return c.Next()
	})
	r := f.app.Group("/restaurant", middleware.LoadRestaurantPermissions())
	RegisterSupplyRoutes(r, handler.New())
	return f
}

func (f *buyFixture) call(user, method, path string, body any) (int, string) {
	f.t.Helper()
	return call(f.t, f.app, user, method, path, body)
}

func (f *buyFixture) supplier() uint {
	f.t.Helper()
	s, b := f.call("supervisor", "POST", "/suppliers", map[string]any{"doc_type": "6", "doc_number": "20100070970", "business_name": "Mercado SAC"})
	if s != 201 {
		f.t.Fatalf("proveedor: %d %s", s, b)
	}
	var out struct{ Data struct{ ID uint } }
	ok(f.t, json.Unmarshal([]byte(b), &out))
	return out.Data.ID
}

func (f *buyFixture) purchaseBody(supplier uint, number, key string) map[string]any {
	return map[string]any{"supplier_id": supplier, "series": "F001", "number": number, "payment_method": "cash", "price_includes_igv": true,
		"idempotency_key": key, "lines": []map[string]any{{"supply_id": f.supply, "sale_unit_id": f.unit, "quantity": 1, "line_total": 177}}}
}

func (f *buyFixture) stock() (qty, avg float64) {
	f.t.Helper()
	var row struct {
		Quantity    float64
		AvgUnitCost *float64
	}
	ok(f.t, f.db.Table("tenant_product_stocks").Select("quantity, avg_unit_cost").Where("product_id = ? AND branch_id = 1", f.supply).Scan(&row).Error)
	if row.AvgUnitCost != nil {
		avg = *row.AvgUnitCost
	}
	return row.Quantity, avg
}

func (f *buyFixture) count(model any, where string, args ...any) int64 {
	var n int64
	ok(f.t, f.db.Model(model).Where(where, args...).Count(&n).Error)
	return n
}

func purchaseID(t *testing.T, body string) uint {
	t.Helper()
	var out struct{ Data struct{ ID uint } }
	if err := json.Unmarshal([]byte(body), &out); err != nil || out.Data.ID == 0 {
		t.Fatalf("sin id: %s", body)
	}
	return out.Data.ID
}

func wantStatus(t *testing.T, got int, body string, code int) {
	t.Helper()
	if got != code {
		t.Fatalf("status %d, quería %d: %s", got, code, body)
	}
}

func TestSupplyPurchaseFlow(t *testing.T) {
	f := setupBuy(t)
	sup := f.supplier()

	s, b := f.call("supervisor", "POST", "/supply-purchases", f.purchaseBody(sup, "100", "k1"))
	wantStatus(t, s, b, 201)
	id := purchaseID(t, b)
	if qty, avg := f.stock(); qty != 50 || math.Abs(avg-3) > 1e-6 {
		t.Fatalf("stock %v promedio %v, quería 50 y 3 (177 con IGV = 150 sin IGV)", qty, avg)
	}
	var cash database.TenantCashMovement
	ok(t, f.db.Where("purchase_id = ? AND type = ?", id, "expense").First(&cash).Error)
	if cash.Amount != 177 {
		t.Fatalf("egreso de caja %v", cash.Amount)
	}

	// Reintento con la misma clave: la misma compra, sin duplicar stock.
	s, b = f.call("supervisor", "POST", "/supply-purchases", f.purchaseBody(sup, "100", "k1"))
	wantStatus(t, s, b, 200)
	if purchaseID(t, b) != id {
		t.Fatal("el reintento debe devolver la misma compra")
	}
	// Mismo comprobante con otra clave: rechazado.
	s, b = f.call("supervisor", "POST", "/supply-purchases", f.purchaseBody(sup, "100", "k2"))
	wantStatus(t, s, b, 409)
	if qty, _ := f.stock(); qty != 50 || f.count(&database.TenantPurchase{}, "1 = 1") != 1 {
		t.Fatalf("no debía duplicar: stock %v", qty)
	}

	// Líneas que no son insumos, sin permiso, sin módulo, sin caja y sin clave.
	body := f.purchaseBody(sup, "101", "k3")
	body["lines"] = []map[string]any{{"supply_id": f.dish, "quantity": 1, "line_total": 10}}
	s, b = f.call("supervisor", "POST", "/supply-purchases", body)
	wantStatus(t, s, b, 400)
	s, b = f.call("mozo", "POST", "/supply-purchases", f.purchaseBody(sup, "102", "k4"))
	wantStatus(t, s, b, 403)
	s, b = f.call("mozo", "GET", "/suppliers", nil)
	wantStatus(t, s, b, 403)
	s, b = f.call("sinmodulo", "POST", "/supply-purchases", f.purchaseBody(sup, "103", "k5"))
	wantStatus(t, s, b, 403)
	s, b = f.call("sincaja", "POST", "/supply-purchases", f.purchaseBody(sup, "104", "k6"))
	wantStatus(t, s, b, 400)
	s, b = f.call("supervisor", "POST", "/supply-purchases", f.purchaseBody(sup, "105", ""))
	wantStatus(t, s, b, 400)

	// Una compra de otra cosa (hecha en Tukifac) no existe para estas rutas.
	contact := sup
	dishID := f.dish
	other, err := purchasesvc.NewPurchaseService(f.db).Create(purchasesvc.CreatePurchaseInput{BranchID: 1, UserID: 2, ContactID: &contact,
		DocType: "FACTURA", Series: "F009", Number: "1", IssueDate: time.Now(), Currency: "PEN", PaymentMethod: "cash", TaxConfig: tax.DefaultConfig(),
		Items: []purchasesvc.PurchaseItemInput{{ProductID: &dishID, Description: "Lomo", Quantity: 1, UnitCost: 5, IgvAffectationType: "10"}}})
	ok(t, err)
	s, b = f.call("supervisor", "GET", "/supply-purchases", nil)
	wantStatus(t, s, b, 200)
	if !strings.Contains(b, `"total":1`) || strings.Contains(b, "F009") {
		t.Fatalf("la lista solo debe traer compras de insumos: %s", b)
	}
	s, b = f.call("supervisor", "GET", fmt.Sprintf("/supply-purchases/%d", other.ID), nil)
	wantStatus(t, s, b, 404)
	s, b = f.call("supervisor", "POST", fmt.Sprintf("/supply-purchases/%d/void", other.ID), nil)
	wantStatus(t, s, b, 404)

	s, b = f.call("supervisor", "GET", fmt.Sprintf("/supplies/%d/purchases", f.supply), nil)
	wantStatus(t, s, b, 200)
	if !strings.Contains(b, "Mercado SAC") || !strings.Contains(b, `"line_total":177`) || !strings.Contains(b, `"unit":"Saco 50"`) {
		t.Fatalf("historial: %s", b)
	}

	s, b = f.call("supervisor", "POST", fmt.Sprintf("/supply-purchases/%d/void", id), nil)
	wantStatus(t, s, b, 200)
	if qty, avg := f.stock(); qty != 0 || avg != 0 {
		t.Fatalf("tras anular: stock %v promedio %v", qty, avg)
	}
	s, b = f.call("supervisor", "POST", fmt.Sprintf("/supply-purchases/%d/void", id), nil)
	wantStatus(t, s, b, 400)
	// Anulada, el comprobante se puede volver a registrar.
	s, b = f.call("supervisor", "POST", "/supply-purchases", f.purchaseBody(sup, "100", "k7"))
	wantStatus(t, s, b, 201)
}

func TestSupplyInitialStockAndEntries(t *testing.T) {
	f := setupBuy(t)
	load := fmt.Sprintf("/supplies/%d/initial-stock", f.supply)
	s, b := f.call("mozo", "POST", load, map[string]any{"quantity": 10, "unit_cost": 3})
	wantStatus(t, s, b, 403)
	s, b = f.call("supervisor", "POST", load, map[string]any{"quantity": 10, "unit_cost": 3})
	wantStatus(t, s, b, 201)
	s, b = f.call("supervisor", "POST", load, map[string]any{"quantity": 10, "unit_cost": 3})
	wantStatus(t, s, b, 409)
	if qty, avg := f.stock(); qty != 10 || avg != 3 {
		t.Fatalf("carga inicial: %v %v", qty, avg)
	}

	entry := func(kind string, qty, cost float64, key string) (int, string) {
		return f.call("supervisor", "POST", "/supply-entries", map[string]any{"supply_id": f.supply, "kind": kind, "quantity": qty,
			"total_cost": cost, "idempotency_key": key, "notes": "prueba"})
	}
	s, b = entry("ingreso", 10, 50, "e1") // 5 por kilo
	wantStatus(t, s, b, 201)
	s, b = entry("ingreso", 10, 50, "e1")
	wantStatus(t, s, b, 200)
	if qty, avg := f.stock(); qty != 20 || avg != 4 {
		t.Fatalf("ingreso: %v %v", qty, avg)
	}
	s, b = entry("merma", 2, 999, "e2")
	wantStatus(t, s, b, 201)
	s, b = entry("desperdicio", 1, 0, "e3")
	wantStatus(t, s, b, 201)
	if qty, avg := f.stock(); qty != 17 || avg != 4 {
		t.Fatalf("merma: %v %v", qty, avg)
	}
	s, b = entry("merma", 100, 0, "e4")
	wantStatus(t, s, b, 400)
	s, b = entry("regalo", 1, 0, "e5")
	wantStatus(t, s, b, 400)
	s, b = entry("ingreso", 1, 1, "")
	wantStatus(t, s, b, 400)
	if f.count(&database.TenantStockMovement{}, "notes LIKE ?", "%APP:%") != 0 {
		t.Fatal("la clave interna no debe aparecer en el kardex")
	}
	if f.count(&database.TenantInventoryDocument{}, "reference LIKE ?", "APP:%") != 3 {
		t.Fatal("un documento por intento aceptado")
	}
	s, b = f.call("supervisor", "POST", "/supply-entries", map[string]any{"supply_id": f.dish, "kind": "ingreso", "quantity": 1, "idempotency_key": "e6"})
	wantStatus(t, s, b, 404)
}

// parallelStatus lanza n peticiones a la vez y cuenta los estados.
func (f *buyFixture) parallelStatus(n int, method, path string, body any) map[int]int {
	f.t.Helper()
	out := map[int]int{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	start := make(chan struct{})
	raw, _ := json.Marshal(body)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			req := httptest.NewRequest(method, "/restaurant"+path, strings.NewReader(string(raw)))
			req.Header.Set("X-U", "supervisor")
			req.Header.Set("Content-Type", "application/json")
			resp, err := f.app.Test(req, fiber.TestConfig{Timeout: 60 * time.Second})
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				out[0]++
				return
			}
			out[resp.StatusCode]++
		}()
	}
	close(start)
	wg.Wait()
	return out
}

func TestSupplyConcurrency(t *testing.T) {
	f := setupBuy(t)
	if !f.mysql {
		t.Skip("requiere TUKIFAC_TEST_MYSQL_DSN (MySQL/MariaDB)")
	}
	const n = 8
	sup := f.supplier()

	got := f.parallelStatus(n, "POST", "/supply-purchases", f.purchaseBody(sup, "200", "same"))
	if got[201] != 1 || got[200] != n-1 || f.count(&database.TenantPurchase{}, "number = ?", "200") != 1 {
		t.Fatalf("misma clave a la vez: %v", got)
	}
	var created database.TenantPurchase
	ok(t, f.db.Where("number = ?", "200").First(&created).Error)
	if qty, _ := f.stock(); qty != 50 {
		t.Fatalf("stock %v", qty)
	}

	// Mismo comprobante con claves distintas a la vez.
	var wg sync.WaitGroup
	codes := make([]int, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			codes[i] = f.parallelStatus(1, "POST", "/supply-purchases", f.purchaseBody(sup, "201", fmt.Sprintf("d%d", i)))[201]
		}(i)
	}
	wg.Wait()
	if f.count(&database.TenantPurchase{}, "number = ?", "201") != 1 {
		t.Fatalf("mismo comprobante a la vez: %v", codes)
	}

	got = f.parallelStatus(n, "POST", fmt.Sprintf("/supply-purchases/%d/void", created.ID), nil)
	if got[200] != 1 {
		t.Fatalf("anulación a la vez: %v", got)
	}
	if qty, _ := f.stock(); qty != 50 {
		t.Fatalf("la anulación debe revertir una sola vez: stock %v", qty)
	}
	if f.count(&database.TenantCashMovement{}, "purchase_id = ? AND type = ?", created.ID, "income") > 1 {
		t.Fatal("caja revertida dos veces")
	}

	got = f.parallelStatus(n, "POST", "/supply-entries", map[string]any{"supply_id": f.supply, "kind": "ingreso", "quantity": 5, "total_cost": 20, "idempotency_key": "p1"})
	if got[201] != 1 || got[200] != n-1 {
		t.Fatalf("ingreso a la vez: %v", got)
	}
	if qty, _ := f.stock(); qty != 55 {
		t.Fatalf("stock %v", qty)
	}
}

func TestSupplyInitialStockConcurrent(t *testing.T) {
	f := setupBuy(t)
	if !f.mysql {
		t.Skip("requiere TUKIFAC_TEST_MYSQL_DSN (MySQL/MariaDB)")
	}
	got := f.parallelStatus(8, "POST", fmt.Sprintf("/supplies/%d/initial-stock", f.supply), map[string]any{"quantity": 10, "unit_cost": 3})
	if got[201] != 1 {
		t.Fatalf("carga inicial a la vez: %v", got)
	}
	if qty, _ := f.stock(); qty != 10 {
		t.Fatalf("stock %v", qty)
	}
}

// Las compras de siempre (sin clave) no cambian, y una BD sin V197 sigue comprando.
func TestPurchaseWithoutKeyUnchanged(t *testing.T) {
	f := setupBuy(t)
	sup := f.supplier()
	dishID := f.dish
	in := purchasesvc.CreatePurchaseInput{BranchID: 1, UserID: 2, ContactID: &sup, DocType: "FACTURA", Series: "F001", Number: "7",
		IssueDate: time.Now(), Currency: "PEN", PaymentMethod: "cash", TaxConfig: tax.DefaultConfig(),
		Items: []purchasesvc.PurchaseItemInput{{ProductID: &dishID, Description: "Lomo", Quantity: 1, UnitCost: 5, IgvAffectationType: "10"}}}
	for i := 0; i < 2; i++ {
		_, err := purchasesvc.NewPurchaseService(f.db).Create(in)
		ok(t, err)
	}
	if f.count(&database.TenantPurchase{}, "number = ?", "7") != 2 {
		t.Fatal("sin clave el comportamiento no cambia")
	}
	if f.mysql {
		ok(t, f.db.Exec("DROP INDEX uk_tenant_purchases_user_idempotency ON tenant_purchases").Error)
	} else {
		ok(t, f.db.Exec("DROP INDEX uk_tenant_purchases_user_idempotency").Error)
	}
	ok(t, f.db.Exec("ALTER TABLE tenant_purchases DROP COLUMN idempotency_key").Error)
	// Otra conexión: el sondeo positivo se cachea por pool.
	fresh := f.db.Session(&gorm.Session{})
	in.Number, in.IdempotencyKey = "8", ""
	_, err := purchasesvc.NewPurchaseService(fresh).Create(in)
	ok(t, err)
}

// La anulación normal (Tukifac) también revierte una sola vez con 8 peticiones a la vez.
func TestPurchaseVoidConcurrent(t *testing.T) {
	f := setupBuy(t)
	if !f.mysql {
		t.Skip("requiere TUKIFAC_TEST_MYSQL_DSN (MySQL/MariaDB)")
	}
	sup := f.supplier()
	for _, n := range []string{"900", "901"} { // la segunda deja stock de sobra para una doble reversa
		s, b := f.call("supervisor", "POST", "/supply-purchases", f.purchaseBody(sup, n, "z"+n))
		wantStatus(t, s, b, 201)
	}
	var p database.TenantPurchase
	ok(t, f.db.Where("number = ?", "900").First(&p).Error)
	errs := make([]error, 8)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			errs[i] = purchasesvc.NewPurchaseService(f.db).Void(p.ID, 2)
		}(i)
	}
	close(start)
	wg.Wait()
	succeeded := 0
	for _, err := range errs {
		if err == nil {
			succeeded++
		}
	}
	qty, _ := f.stock()
	if succeeded != 1 || qty != 50 || f.count(&database.TenantCashMovement{}, "type = ?", "income") != 1 {
		t.Fatalf("anulaciones=%d stock=%v, quería 1 y 50 con un solo ingreso de caja", succeeded, qty)
	}
}
