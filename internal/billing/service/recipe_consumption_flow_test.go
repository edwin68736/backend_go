package service

// F5a: descuento de insumos al vender, por los flujos reales (POS rápido, mesa, nota de venta, nota de crédito, emisión). SQLite y, con
// TUKIFAC_TEST_MYSQL_DSN (BD desechable cuyo nombre contenga "test"), MySQL/MariaDB con simultaneidad real.
//
// Vive en este paquete porque necesita la emisión real del comprobante y de la nota de crédito.

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"tukifac/config"
	invsvc "tukifac/internal/inventory/service"
	productsvc "tukifac/internal/products/service"
	recipesvc "tukifac/internal/recipes/service"
	rsvc "tukifac/internal/restaurant/service"
	salesvc "tukifac/internal/sales/service"
	"tukifac/pkg/database"
	"tukifac/pkg/database/tenantmigrations"
	"tukifac/pkg/dbtest"
	"tukifac/pkg/fiscalclient"
	"tukifac/pkg/logger"
	"tukifac/pkg/tax"

	"github.com/glebarez/sqlite"
	mysqldrv "gorm.io/driver/mysql"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

type recipeFlow struct {
	t       *testing.T
	db      *gorm.DB
	mysql   bool
	billing *BillingService

	nvSeries, boletaSeries, contact, table uint
	meat, bread, cheese                    uint
	burger, drink, combo                   uint

	mu   sync.Mutex
	sent []map[string]any // documentos que recibió el facturador
}

func flowDB(t *testing.T) (*gorm.DB, bool) {
	t.Helper()
	cfg := &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Silent)}
	dsn := os.Getenv("TUKIFAC_TEST_MYSQL_DSN")
	if dsn == "" {
		db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), cfg)
		must(t, err)
		return db, false
	}
	db, err := gorm.Open(mysqldrv.Open(dsn), cfg)
	must(t, err)
	var name string
	db.Raw("SELECT DATABASE()").Scan(&name)
	if !strings.Contains(strings.ToLower(name), "test") {
		t.Fatalf("la prueba borra todas las tablas: BD %q", name)
	}
	var tables []string
	db.Raw("SELECT table_name FROM information_schema.tables WHERE table_schema = DATABASE()").Scan(&tables)
	must(t, db.Connection(func(conn *gorm.DB) error {
		conn.Exec("SET FOREIGN_KEY_CHECKS = 0")
		defer conn.Exec("SET FOREIGN_KEY_CHECKS = 1")
		for _, tb := range tables {
			if err := conn.Exec("DROP TABLE `" + tb + "`").Error; err != nil {
				return err
			}
		}
		return nil
	}))
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(20)
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db, true
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func newRecipeFlow(t *testing.T) *recipeFlow {
	t.Helper()
	db, isMySQL := flowDB(t)
	f := &recipeFlow{t: t, db: db, mysql: isMySQL}

	must(t, database.ApplyBaselineSchema(db))
	for _, m := range []interface{ Up(*gorm.DB) error }{
		tenantmigrations.V083InventoryIngressEgress{}, tenantmigrations.V084InventoryOperationTransfer{},
		tenantmigrations.V085InventoryDocumentSource{}, tenantmigrations.V150SaleIdempotencyKey{},
	} {
		must(t, m.Up(db))
	}
	for _, m := range []interface{}{
		&database.TenantProductBranch{}, &database.TenantProductBranchPrice{}, &database.TenantBranchDailyComandaCounter{},
		&database.TenantSalePrepaymentVoucher{}, &database.TenantSalePrepaymentApplication{}, &database.TenantSaleDetraccion{},
		&database.TenantSaleFiscalProfile{}, &database.TenantSaleCreditInstallment{}, &database.TenantInvoice{},
		&database.TenantRestaurantSetting{}, &database.TenantSaleExport{},
	} {
		must(t, db.AutoMigrate(m))
	}
	must(t, (tenantmigrations.V198Recipes{}).Up(db))
	must(t, (tenantmigrations.V199RecipeConsumption{}).Up(db))
	must(t, (tenantmigrations.V199RecipeConsumption{}).Up(db)) // repetible
	must(t, database.SeedInventoryOperationTypes(db))

	must(t, db.Create(&database.TenantCompanyConfig{ID: 1, RUC: "20123456789", BusinessName: "EMPRESA SAC", TradeName: "EMPRESA",
		Address: "Av. Test 123", Ubigeo: "150101", SunatEnabled: true, TaxRate: 18}).Error)
	must(t, db.Create(&database.UbiRegion{ID: "15", Nombre: "LIMA"}).Error)
	must(t, db.Create(&database.UbiProvincia{ID: "1501", Nombre: "LIMA", RegionID: "15"}).Error)
	must(t, db.Create(&database.UbiDistrito{ID: "150101", Nombre: "LIMA", ProvinciaID: "1501", RegionID: "15"}).Error)
	must(t, db.Create(&database.TenantBranch{Name: "Principal", Active: true, IsMain: true}).Error)
	must(t, db.Where(database.TenantPaymentMethod{Code: "cash"}).Attrs(database.TenantPaymentMethod{Name: "Efectivo", IsSystem: true,
		Active: true, DestinationType: "cash"}).FirstOrCreate(&database.TenantPaymentMethod{}).Error)
	must(t, db.Create(&database.TenantCashSession{BranchID: 1, UserID: 1, OpenedBy: 1, Status: "open", OpenedAt: time.Now()}).Error)

	series := func(sunat, code, category, docType string) uint {
		row := database.TenantDocumentSeries{BranchID: 1, SunatCode: sunat, Series: code, Category: category, DocType: docType,
			Active: true, Correlative: 1}
		must(t, db.Create(&row).Error)
		return row.ID
	}
	f.nvSeries = series("00", "NV01", "venta", "NOTA_VENTA")
	f.boletaSeries = series("03", "B001", "venta", "BOLETA")
	series("07", "BC01", "nota_credito", "NOTA_CREDITO")
	contact := database.TenantContact{Type: "customer", DocType: "1", DocNumber: "45678912", BusinessName: "JUAN PEREZ", Active: true}
	must(t, db.Create(&contact).Error)
	f.contact = contact.ID
	floor := database.TenantRestaurantFloor{BranchID: 1, Name: "Sala", Active: true}
	must(t, db.Create(&floor).Error)
	table := database.TenantRestaurantTable{BranchID: 1, FloorID: floor.ID, Name: "Mesa 1", Capacity: 4, Status: "libre", Active: true}
	must(t, db.Create(&table).Error)
	f.table = table.ID

	inv := invsvc.NewInventoryService(db)
	supply := func(code, name, unit string, qty, cost float64) uint {
		id := f.product(productsvc.ProductInput{IsSupply: true, Code: code, Name: name, Unit: unit, IgvAffectationType: "10", Active: true})
		must(t, inv.RecordSupplyInitialStock(id, 1, qty, cost, 1))
		return id
	}
	f.meat = supply("CARNE", "Carne", "KGM", 10, 20)
	f.bread = supply("PAN", "Pan", "NIU", 100, 0.80)
	f.cheese = supply("QUESO", "Queso", "KGM", 5, 35)
	f.burger = f.dish("BURGER", "Hamburguesa", 15, 50)
	f.drink = f.dish("GASEOSA", "Gaseosa", 4, 50)

	combo := database.TenantProduct{Code: "COMBO", Name: "Combo", Type: "product", Unit: "NIU", SalePrice: 18, IgvAffectationType: "10",
		PriceIncludesIgv: true, IsRestaurant: true, BranchID: 1, HasCombo: true, Active: true}
	must(t, db.Create(&combo).Error)
	f.combo = combo.ID
	for i, comp := range []uint{f.burger, f.drink} {
		g := database.TenantComboGroup{ProductID: combo.ID, Name: fmt.Sprintf("G%d", i), SelectionType: database.ComboSelectionFixed,
			SortOrder: i, Active: true}
		must(t, db.Create(&g).Error)
		must(t, db.Create(&database.TenantComboGroupItem{GroupID: g.ID, ProductID: comp, DefaultQuantity: 1, IsDefault: true, Active: true}).Error)
	}

	// Hamburguesa: 150 g de carne (3.00) + 1 pan (0.80) + 10 g de queso (0.35) = 4.15.
	f.recipe(f.burger, 0, 1, f.line(f.meat, 150, "GRM"), f.line(f.bread, 1, ""), f.line(f.cheese, 10, "GRM"))
	must(t, database.SetRecipesEnabled(db, true))

	f.setupFiscal()
	return f
}

// setupFiscal: facturador falso (guarda el documento que recibiría), BD central mínima y servicio de facturación.
func (f *recipeFlow) setupFiscal() {
	t := f.t
	prevLog, prevCfg := logger.L, config.AppConfig
	logger.L = slog.New(slog.NewTextHandler(io.Discard, nil))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Document map[string]any `json:"document"`
		}
		if json.NewDecoder(r.Body).Decode(&body) == nil && body.Document != nil {
			f.mu.Lock()
			f.sent = append(f.sent, body.Document)
			f.mu.Unlock()
		}
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"document_uuid":"doc-%d","status":"queued"}`, time.Now().UnixNano())))
	}))
	cfg := config.Config{FacturadorBaseURL: server.URL, FacturadorToken: "tok"}
	if prevCfg != nil {
		cfg = *prevCfg
		cfg.FacturadorBaseURL, cfg.FacturadorToken = server.URL, "tok"
	}
	config.AppConfig = &cfg
	fiscalclient.Init(server.URL, "tok")
	t.Cleanup(func() {
		logger.L, config.AppConfig = prevLog, prevCfg
		fiscalclient.Init("", "")
		server.Close()
	})
	f.billing = &BillingService{db: f.db, baseURL: server.URL, token: "tok", tenantSlug: "recetas"}
	setupAmountFlowCentral(t, f.billing)
	logger.L = slog.New(slog.NewTextHandler(io.Discard, nil))
}

func (f *recipeFlow) product(in productsvc.ProductInput) uint {
	f.t.Helper()
	p, _, err := productsvc.NewProductService(f.db).Create(in)
	must(f.t, err)
	return p.ID
}

// dish: plato de la carta CON control de stock propio, para comprobar que con receta deja de descontarlo.
func (f *recipeFlow) dish(code, name string, price, stock float64) uint {
	f.t.Helper()
	id := f.product(productsvc.ProductInput{Code: code, Name: name, Unit: "NIU", SalePrice: price, IsRestaurant: true, Active: true,
		ManageStock: true, PriceIncludesIgv: true, IgvAffectationType: "10"})
	if stock > 0 {
		must(f.t, invsvc.NewInventoryService(f.db).RecordMovement(invsvc.MovementInput{ProductID: id, BranchID: 1, Type: "in", Quantity: stock, UserID: 1}))
	}
	return id
}

func (f *recipeFlow) line(supply uint, qty float64, unit string) recipesvc.LineInput {
	return recipesvc.LineInput{SupplyID: supply, Quantity: qty, Unit: unit}
}

func (f *recipeFlow) recipe(dish, presentation uint, yield float64, lines ...recipesvc.LineInput) {
	f.t.Helper()
	must(f.t, recipesvc.New(f.db).Save(recipesvc.SaveInput{ProductID: dish, PresentationID: presentation, YieldQty: yield, Lines: lines, UserID: 1}))
}

func (f *recipeFlow) consume(on bool, policy string) {
	f.t.Helper()
	must(f.t, recipesvc.SaveConsumptionSettings(f.db, recipesvc.ConsumptionSettings{Consume: on, StockPolicy: policy}))
}

func item(product uint, qty float64) rsvc.NewOrderItem {
	id := product
	return rsvc.NewOrderItem{ProductID: &id, Quantity: qty}
}

func cashPay() []rsvc.PaymentInput { return []rsvc.PaymentInput{{Method: "cash", Amount: 5000}} }

// quick: venta rápida del POS (POSCheckoutService → SaleService.Create).
func (f *recipeFlow) quickErr(series uint, items ...rsvc.NewOrderItem) (*database.TenantSale, error) {
	docType := "NOTA_VENTA"
	if series == f.boletaSeries {
		docType = "BOLETA"
	}
	contact := f.contact
	return rsvc.NewRestaurantPOSCheckoutService(f.db).Checkout(rsvc.POSCheckoutInput{
		BranchID: 1, UserID: 1, OrderType: rsvc.OrderTypeQuickSale, Items: items, SeriesID: series, DocType: docType,
		ContactID: &contact, Payments: cashPay(), IssueDate: time.Now(),
	}, tax.LoadFromDB(f.db))
}

func (f *recipeFlow) quick(series uint, items ...rsvc.NewOrderItem) *database.TenantSale {
	f.t.Helper()
	sale, err := f.quickErr(series, items...)
	must(f.t, err)
	return sale
}

func (f *recipeFlow) open(items ...rsvc.NewOrderItem) (uint, []database.TenantComanda) {
	f.t.Helper()
	svc := rsvc.New(f.db)
	table := f.table
	sess, err := svc.OpenSession(rsvc.OpenSessionInput{TableID: &table, UserID: 1, BranchID: 1, Guests: 2})
	must(f.t, err)
	order, err := svc.AddOrder(sess.ID, nil, 1, items, "")
	must(f.t, err)
	return sess.ID, order.Comandas
}

func (f *recipeFlow) bill(session uint, comandas ...uint) (*database.TenantSale, error) {
	return rsvc.New(f.db).BillTable(rsvc.BillInput{SessionID: session, UserID: 1, SeriesID: f.nvSeries, DocType: "NOTA_VENTA",
		Payments: cashPay(), ComandaIDs: comandas, CloseSession: true, IssueDate: time.Now()}, tax.LoadFromDB(f.db))
}

func (f *recipeFlow) stock(product uint) float64 {
	f.t.Helper()
	var q float64
	must(f.t, f.db.Model(&database.TenantProductStock{}).Select("COALESCE(SUM(quantity),0)").
		Where("product_id = ? AND branch_id = 1", product).Scan(&q).Error)
	return math.Round(q*1000) / 1000
}

func (f *recipeFlow) wantStock(step string, want map[uint]float64) {
	f.t.Helper()
	for id, w := range want {
		if got := f.stock(id); math.Abs(got-w) > 0.0005 {
			f.t.Fatalf("%s: producto %d con stock %v, se esperaba %v", step, id, got, w)
		}
	}
}

func (f *recipeFlow) supplies() map[uint]float64 {
	return map[uint]float64{f.meat: f.stock(f.meat), f.bread: f.stock(f.bread), f.cheese: f.stock(f.cheese)}
}

type lineCost struct {
	ProductID       uint
	Quantity        float64
	RecipeCost      *float64
	RecipeCostTotal *float64
}

func (f *recipeFlow) costs(sale uint) map[uint]lineCost {
	f.t.Helper()
	var rows []lineCost
	must(f.t, f.db.Table("tenant_sale_items").Select("product_id, quantity, recipe_cost, recipe_cost_total").
		Where("sale_id = ?", sale).Order("id").Scan(&rows).Error)
	out := map[uint]lineCost{}
	for _, r := range rows {
		out[r.ProductID] = r
	}
	return out
}

func wantCost(t *testing.T, step string, c lineCost, unit, total float64) {
	t.Helper()
	if c.RecipeCost == nil || c.RecipeCostTotal == nil || math.Abs(*c.RecipeCost-unit) > 0.0001 || math.Abs(*c.RecipeCostTotal-total) > 0.0001 {
		t.Fatalf("%s: costo de receta %v / %v, se esperaba %v / %v", step, deref(c.RecipeCost), deref(c.RecipeCostTotal), unit, total)
	}
}

func deref(v *float64) any {
	if v == nil {
		return "vacío"
	}
	return *v
}

func (f *recipeFlow) count(model any, where string, args ...any) int64 {
	f.t.Helper()
	var n int64
	q := f.db.Model(model)
	if where != "" {
		q = q.Where(where, args...)
	}
	must(f.t, q.Count(&n).Error)
	return n
}

func TestRecetas_apagadoIgualQueHoy(t *testing.T) {
	f := newRecipeFlow(t)
	before := f.supplies()
	check := func(step string) {
		sale := f.quick(f.nvSeries, item(f.burger, 2), item(f.combo, 1))
		f.wantStock(step, before)
		if f.count(&database.TenantRecipeConsumption{}, "") != 0 || f.count(&database.TenantStockMovement{}, "reference LIKE ?", "RECETA/%") != 0 {
			t.Fatalf("%s: no debe haber consumos", step)
		}
		for _, c := range f.costs(sale.ID) {
			if c.RecipeCost != nil || c.RecipeCostTotal != nil {
				t.Fatalf("%s: no debe haber costo de receta", step)
			}
		}
	}
	check("módulo activado y descuento apagado (así nace)")
	f.wantStock("el plato sigue descontando su stock", map[uint]float64{f.burger: 47, f.drink: 49})

	f.consume(true, "allow")
	must(t, database.SetRecipesEnabled(f.db, false))
	check("módulo apagado por el superadmin")

	must(t, database.SetRecipesEnabled(f.db, true))
	t.Setenv("RECIPES_CONSUMPTION", "off")
	check("interruptor de emergencia del servidor")
	f.wantStock("tres ventas iguales", map[uint]float64{f.burger: 41, f.drink: 47})
}

func TestRecetas_ventaRapidaYAnulacion(t *testing.T) {
	f := newRecipeFlow(t)
	f.consume(true, "allow")
	before := f.supplies()

	sale := f.quick(f.nvSeries, item(f.burger, 2))
	f.wantStock("venta", map[uint]float64{f.meat: 9.7, f.bread: 98, f.cheese: 4.98, f.burger: 50})
	wantCost(t, "venta", f.costs(sale.ID)[f.burger], 4.15, 8.30)
	if n := f.count(&database.TenantRecipeConsumption{}, "sale_id = ?", sale.ID); n != 3 {
		t.Fatalf("consumos: %d", n)
	}
	if n := f.count(&database.TenantStockMovement{}, "reference = ?", "VENTA/"+sale.Number); n != 0 {
		t.Fatalf("el plato con receta no debe mover su propio stock: %d movimientos", n)
	}

	// Cambiar la receta y apagar el descuento no cambia lo que se repone: sale del registro de consumos.
	f.recipe(f.burger, 0, 1, f.line(f.bread, 5, ""))
	f.consume(false, "allow")
	sales := salesvc.NewSaleService(f.db)
	must(t, sales.CancelNotaVenta(sale.ID, 1, "prueba"))
	f.wantStock("anulación", before)
	if err := sales.CancelNotaVenta(sale.ID, 1, "otra vez"); err == nil {
		t.Fatal("no se anula dos veces")
	}
	f.wantStock("segunda anulación", before)
	var avg float64
	must(t, f.db.Table("tenant_product_stocks").Select("avg_unit_cost").Where("product_id = ? AND branch_id = 1", f.meat).Scan(&avg).Error)
	if math.Abs(avg-20) > 0.000001 {
		t.Fatalf("el costo promedio debe quedar en 20: %v", avg)
	}
}

func TestRecetas_mesaDivididaYCombo(t *testing.T) {
	f := newRecipeFlow(t)
	f.consume(true, "allow")

	session, comandas := f.open(item(f.burger, 2), item(f.combo, 2))
	var burgerComanda uint
	var comboComandas []uint
	for _, c := range comandas {
		if c.ComboParentKey == "" {
			burgerComanda = c.ID
		} else {
			comboComandas = append(comboComandas, c.ID)
		}
	}
	first, err := f.bill(session, burgerComanda)
	must(t, err)
	f.wantStock("primer cobro: 2 hamburguesas", map[uint]float64{f.meat: 9.7, f.bread: 98, f.burger: 50, f.drink: 50})
	wantCost(t, "primer cobro", f.costs(first.ID)[f.burger], 4.15, 8.30)

	second, err := f.bill(session, comboComandas...)
	must(t, err)
	// El combo consume la receta de la hamburguesa; la gaseosa no tiene receta y sigue descontando su stock.
	f.wantStock("segundo cobro: 2 combos", map[uint]float64{f.meat: 9.4, f.bread: 96, f.cheese: 4.96, f.burger: 50, f.drink: 48})
	c := f.costs(second.ID)[f.combo]
	if c.RecipeCost != nil || c.RecipeCostTotal != nil {
		t.Fatalf("combo con un componente sin receta: el costo queda vacío, no %v", deref(c.RecipeCostTotal))
	}
	var row database.TenantRecipeConsumption
	must(t, f.db.Where("sale_id = ? AND supply_id = ?", second.ID, f.meat).First(&row).Error)
	if row.SourceProductID != f.burger || math.Abs(row.Quantity-0.3) > 0.0005 {
		t.Fatalf("consumo del combo: %+v", row)
	}

	// Con receta también en la gaseosa, el combo tiene costo y la gaseosa ya no mueve su stock.
	f.recipe(f.drink, 0, 1, f.line(f.bread, 1, ""))
	third := f.quick(f.nvSeries, item(f.combo, 1))
	wantCost(t, "combo completo", f.costs(third.ID)[f.combo], 4.95, 4.95)
	f.wantStock("combo completo", map[uint]float64{f.drink: 48, f.bread: 94})

	must(t, salesvc.NewSaleService(f.db).CancelNotaVenta(second.ID, 1, "prueba"))
	f.wantStock("anular el cobro de los combos", map[uint]float64{f.meat: 9.55, f.drink: 50})
}

func TestRecetas_presentaciones(t *testing.T) {
	f := newRecipeFlow(t)
	f.consume(true, "allow")
	must(t, f.db.Model(&database.TenantProduct{}).Where("id = ?", f.burger).Update("has_variants", true).Error)
	simple := database.TenantProductPresentation{ProductID: f.burger, Name: "Simple", SalePrice: 15, Active: true}
	double := database.TenantProductPresentation{ProductID: f.burger, Name: "Doble", SalePrice: 20, Active: true}
	must(t, f.db.Create(&simple).Error)
	must(t, f.db.Create(&double).Error)
	f.recipe(f.burger, double.ID, 1, f.line(f.meat, 300, "GRM"), f.line(f.bread, 1, ""))

	variant := func(p uint, qty float64) rsvc.NewOrderItem {
		it := item(f.burger, qty)
		it.ModifiersJSON = fmt.Sprintf(`[{"type":"variant","option_id":%d}]`, p) // como lo manda el POS
		return it
	}
	// La «Doble» tiene receta propia; la «Simple» usa la del plato. Ninguna tiene stock propio y aun así se venden.
	f.quick(f.nvSeries, variant(double.ID, 1))
	f.wantStock("doble", map[uint]float64{f.meat: 9.7, f.bread: 99, f.cheese: 5})
	session, _ := f.open(variant(simple.ID, 2))
	_, err := f.bill(session)
	must(t, err)
	f.wantStock("simple en mesa", map[uint]float64{f.meat: 9.4, f.bread: 97, f.cheese: 4.98})
}

func TestRecetas_comprobanteDesdeNotaDeVenta(t *testing.T) {
	f := newRecipeFlow(t)
	f.consume(true, "allow")
	nv := f.quick(f.nvSeries, item(f.burger, 3))
	after := f.supplies()

	boleta, err := salesvc.NewSaleService(f.db).IssueElectronicFromNota(nv.ID, f.boletaSeries, 1, "", 0, nil)
	must(t, err)
	f.wantStock("emitir desde la nota no vuelve a consumir", after)
	if f.count(&database.TenantRecipeConsumption{}, "sale_id = ?", boleta.ID) != 0 {
		t.Fatal("el comprobante no registra consumos")
	}
	wantCost(t, "el comprobante copia el costo", f.costs(boleta.ID)[f.burger], 4.15, 12.45)

	// Devolución parcial del comprobante nacido de una nota: no repone, igual que con los productos.
	f.accept(boleta.ID)
	f.creditNote(boleta, "07", 1)
	f.wantStock("devolución parcial sobre comprobante de una nota", after)

	// La anulación total anula también la nota de venta, que es la que consumió.
	f.creditNote(boleta, "01", 0)
	if got := statusOf(f.db, nv.ID); got != "cancelled" {
		t.Fatalf("nota de venta: %s", got)
	}
	f.wantStock("anulación total", map[uint]float64{f.meat: 10, f.bread: 100, f.cheese: 5})
}

// Venta como la de «Nueva venta» de Tukifac (SaleService.Create), con una unidad de venta del plato.
func TestRecetas_ventaTukifacConUnidadDeVenta(t *testing.T) {
	f := newRecipeFlow(t)
	f.consume(true, "allow")
	var pack database.TenantUnit
	must(t, f.db.Where(database.TenantUnit{Code: "BX"}).Attrs(database.TenantUnit{Name: "Caja", Active: true}).FirstOrCreate(&pack).Error)
	unit, err := productsvc.NewProductService(f.db).CreateSaleUnit(f.burger, productsvc.SaleUnitInput{Name: "Media docena",
		UnitID: &pack.ID, ConversionFactor: 6, Price1: 80, Active: true})
	must(t, err)
	contact, burger, unitID := f.contact, f.burger, unit.ID
	sale, err := salesvc.NewSaleService(f.db).Create(salesvc.CreateSaleInput{
		BranchID: 1, UserID: 1, ContactID: &contact, SeriesID: f.nvSeries, DocType: "NOTA_VENTA", IssueDate: time.Now(),
		Payments: []salesvc.PaymentInput{{Method: "cash", Amount: 5000}}, UserCanOverridePrice: true,
		Items: []salesvc.SaleItemInput{{ProductID: &burger, SaleUnitID: &unitID, Description: "Hamburguesa x6", Quantity: 2,
			UnitPrice: 80, IgvAffectationType: "10", PriceIncludesIgv: true}},
	})
	must(t, err)
	// 2 × 6 = 12 hamburguesas.
	f.wantStock("unidad de venta", map[uint]float64{f.meat: 8.2, f.bread: 88, f.cheese: 4.88, f.burger: 50})
	wantCost(t, "unidad de venta", f.costs(sale.ID)[f.burger], 24.90, 49.80)
}

// creditNote: nota de crédito real (CreateCreditNote), aceptada como la acepta SUNAT (PostFiscalAcceptSideEffects).
func (f *recipeFlow) creditNote(boleta *database.TenantSale, reason string, qty float64) *database.TenantSale {
	f.t.Helper()
	var selections []NoteItemSelection
	if qty > 0 {
		var line database.TenantSaleItem
		must(f.t, f.db.Where("sale_id = ?", boleta.ID).First(&line).Error)
		selections = []NoteItemSelection{{OriginalItemID: line.ID, Quantity: qty}}
	}
	note, _, err := f.billing.CreateCreditNote(boleta.ID, "prueba", reason, selections, 0, "")
	if note == nil {
		f.t.Fatalf("no se creó la nota: %v", err)
	}
	f.accept(note.ID)
	f.billing.PostFiscalAcceptSideEffects(note.ID, "SUNAT_ACCEPTED")
	return note
}

func (f *recipeFlow) accept(sale uint) {
	f.t.Helper()
	var inv database.TenantInvoice
	must(f.t, f.db.Where("sale_id = ?", sale).Limit(1).Find(&inv).Error)
	inv.SaleID, inv.PipelineStatus, inv.SunatStatus, inv.SunatCDRCode = sale, "SUNAT_ACCEPTED", "accepted", "0"
	inv.XMLURL, inv.SunatHash, inv.CDRURL = "xml", "hash", "cdr"
	must(f.t, f.db.Save(&inv).Error)
	must(f.t, f.db.Model(&database.TenantSale{}).Where("id = ?", sale).Update("billing_status", "accepted").Error)
}

func TestRecetas_notaDeCreditoParcialRepetida(t *testing.T) {
	f := newRecipeFlow(t)
	f.consume(true, "allow")
	before := f.supplies()
	boleta := f.quick(f.boletaSeries, item(f.burger, 3))
	f.accept(boleta.ID)
	f.wantStock("venta", map[uint]float64{f.meat: 9.55, f.bread: 97, f.cheese: 4.97})

	note := f.creditNote(boleta, "07", 1)
	oneBack := map[uint]float64{f.meat: 9.7, f.bread: 98, f.cheese: 4.98}
	f.wantStock("devuelve 1 de 3", oneBack)
	f.billing.PostFiscalAcceptSideEffects(note.ID, "SUNAT_ACCEPTED")
	f.wantStock("la misma nota procesada otra vez no repone de nuevo", oneBack)

	f.creditNote(boleta, "07", 2)
	f.wantStock("devueltas las 3", before)
	if n := f.count(&database.TenantRecipeConsumptionReversal{}, ""); n != 6 {
		t.Fatalf("reversas: %d, se esperaban 6 (3 insumos × 2 notas)", n)
	}
}

func TestRecetas_notaDeCreditoTotalTrasParcial(t *testing.T) {
	f := newRecipeFlow(t)
	f.consume(true, "allow")
	before := f.supplies()
	boleta := f.quick(f.boletaSeries, item(f.burger, 3))
	f.accept(boleta.ID)
	f.creditNote(boleta, "07", 1)
	f.creditNote(boleta, "01", 0)
	if got := statusOf(f.db, boleta.ID); got != "cancelled" {
		t.Fatalf("la nota total anula la boleta: %s", got)
	}
	f.wantStock("parcial y luego total: repone justo lo consumido", before)
}

func TestRecetas_notaDeDescuentoNoRepone(t *testing.T) {
	f := newRecipeFlow(t)
	f.consume(true, "allow")
	boleta := f.quick(f.boletaSeries, item(f.burger, 2))
	f.accept(boleta.ID)
	after := f.supplies()
	f.creditNote(boleta, "05", 1) // descuento por ítem: el cliente se queda con la comida
	f.wantStock("descuento", after)
}

func TestRecetas_insumoInsuficiente(t *testing.T) {
	f := newRecipeFlow(t)
	f.consume(true, "block")
	// 10 kg de carne alcanzan para 66 hamburguesas.
	sales := f.count(&database.TenantSale{}, "")
	cash := f.count(&database.TenantCashMovement{}, "")
	_, err := f.quickErr(f.nvSeries, item(f.burger, 67))
	if err == nil || !strings.Contains(err.Error(), "Hamburguesa") || !strings.Contains(err.Error(), "Carne") {
		t.Fatalf("debe rechazar nombrando plato e insumo: %v", err)
	}
	f.wantStock("venta rechazada", map[uint]float64{f.meat: 10, f.bread: 100, f.cheese: 5})
	if f.count(&database.TenantSale{}, "") != sales || f.count(&database.TenantCashMovement{}, "") != cash {
		t.Fatal("no debe quedar venta ni movimiento de caja")
	}
	session, _ := f.open(item(f.burger, 67))
	if _, err := f.bill(session); err == nil {
		t.Fatal("el cobro de mesa también se rechaza")
	}
	var sess database.TenantTableSession
	must(t, f.db.First(&sess, session).Error)
	if sess.Status != "open" {
		t.Fatalf("la mesa sigue abierta: %s", sess.Status)
	}
	f.wantStock("cobro rechazado", map[uint]float64{f.meat: 10, f.bread: 100})

	f.consume(true, "allow")
	_, err = f.bill(session)
	must(t, err)
	f.wantStock("vender y quedar en negativo", map[uint]float64{f.meat: -0.05, f.bread: 33})
}

func TestRecetas_agregarPedido(t *testing.T) {
	f := newRecipeFlow(t)
	empty := f.dish("PIZZA", "Pizza", 30, 0)
	f.recipe(empty, 0, 1, f.line(f.cheese, 200, "GRM"))
	svc := rsvc.New(f.db)
	table := f.table
	sess, err := svc.OpenSession(rsvc.OpenSessionInput{TableID: &table, UserID: 1, BranchID: 1, Guests: 1})
	must(t, err)
	if _, err := svc.AddOrder(sess.ID, nil, 1, []rsvc.NewOrderItem{item(empty, 1)}, ""); err == nil {
		t.Fatal("con el descuento apagado se sigue exigiendo el stock del plato")
	}
	f.consume(true, "allow")
	_, err = svc.AddOrder(sess.ID, nil, 1, []rsvc.NewOrderItem{item(empty, 1)}, "")
	must(t, err)
	_, err = f.bill(sess.ID)
	must(t, err)
	f.wantStock("se cobró consumiendo queso", map[uint]float64{f.cheese: 4.8, empty: 0})
}

// El comprobante que se envía al facturador es el mismo con y sin descuento.
func TestRecetas_comprobanteIdentico(t *testing.T) {
	f := newRecipeFlow(t)
	emit := func() map[string]any {
		sale := f.quick(f.boletaSeries, item(f.burger, 2), item(f.combo, 1))
		var cfg database.TenantCompanyConfig
		must(t, f.db.First(&cfg).Error)
		n := len(f.sent)
		_, err := f.billing.emitInvoiceDocument(sale.ID, &cfg)
		must(t, err)
		if len(f.sent) != n+1 {
			t.Fatal("no se envió el documento")
		}
		doc := f.sent[n]
		delete(doc, "correlativo")
		delete(doc, "fechaEmision")
		return doc
	}
	off := emit()
	f.consume(true, "allow")
	on := emit()
	if f.count(&database.TenantRecipeConsumption{}, "") == 0 {
		t.Fatal("la segunda venta debía consumir")
	}
	a, _ := json.Marshal(off)
	b, _ := json.Marshal(on)
	if string(a) != string(b) {
		t.Fatalf("el documento cambió con el descuento:\n%s\n%s", a, b)
	}
	if off["tipoOperacion"] != "0101" || len(off["details"].([]any)) != 2 {
		t.Fatalf("documento inesperado: %s", a)
	}
	if strings.Contains(strings.ToLower(string(b)), "recipe") || strings.Contains(strings.ToLower(string(b)), "receta") {
		t.Fatalf("el documento no debe llevar nada de recetas: %s", b)
	}
}

// Tras ventas y anulaciones al azar, consumido − repuesto = lo que bajó el stock de cada insumo.
func TestRecetas_propiedad(t *testing.T) {
	f := newRecipeFlow(t)
	f.consume(true, "allow")
	f.recipe(f.drink, 0, 3, f.line(f.cheese, 100, "GRM")) // 1/3 por porción: obliga a redondear
	before := f.supplies()
	rng := rand.New(rand.NewSource(7))
	var live []uint
	sales := salesvc.NewSaleService(f.db)
	for i := 0; i < 40; i++ {
		if len(live) > 0 && rng.Intn(3) == 0 {
			k := rng.Intn(len(live))
			must(t, sales.CancelNotaVenta(live[k], 1, "azar"))
			live = append(live[:k], live[k+1:]...)
			continue
		}
		var items []rsvc.NewOrderItem
		for _, p := range []uint{f.burger, f.drink, f.combo} {
			if rng.Intn(2) == 0 {
				items = append(items, item(p, float64(1+rng.Intn(3))))
			}
		}
		if len(items) == 0 {
			items = []rsvc.NewOrderItem{item(f.drink, 1)}
		}
		if rng.Intn(2) == 0 {
			live = append(live, f.quick(f.nvSeries, items...).ID)
			continue
		}
		session, _ := f.open(items...)
		sale, err := f.bill(session)
		must(t, err)
		live = append(live, sale.ID)
	}
	for supply, initial := range before {
		var net float64
		must(t, f.db.Model(&database.TenantRecipeConsumption{}).Select("COALESCE(SUM(quantity - reversed_qty),0)").
			Where("supply_id = ?", supply).Scan(&net).Error)
		if moved := initial - f.stock(supply); math.Abs(moved-net) > 0.0005 {
			t.Fatalf("insumo %d: el stock bajó %v y los consumos netos son %v", supply, moved, net)
		}
	}
	for _, id := range live {
		must(t, sales.CancelNotaVenta(id, 1, "final"))
	}
	f.wantStock("anulado todo", before)
}

// Con una sola conexión: ninguna parte del consumo ni de la reversa puede pedir otra dentro de la transacción.
func TestUnaConexion_recetas(t *testing.T) {
	f := newRecipeFlow(t)
	f.consume(true, "allow")
	var sale *database.TenantSale
	must(t, dbtest.WithSingleConnection(t, f.db, func() (err error) {
		sale, err = f.quickErr(f.nvSeries, item(f.burger, 1), item(f.combo, 1))
		return err
	}))
	session, _ := f.open(item(f.burger, 1))
	must(t, dbtest.WithSingleConnection(t, f.db, func() error { _, err := f.bill(session); return err }))
	must(t, dbtest.WithSingleConnection(t, f.db, func() error {
		return salesvc.NewSaleService(f.db).CancelNotaVenta(sale.ID, 1, "prueba")
	}))
}

func TestMySQL_recetasSimultaneas(t *testing.T) {
	if os.Getenv("TUKIFAC_TEST_MYSQL_DSN") == "" {
		t.Skip("requiere TUKIFAC_TEST_MYSQL_DSN")
	}
	f := newRecipeFlow(t)
	f.consume(true, "allow")
	must(t, f.db.Model(&database.TenantProduct{}).Where("id = ?", f.drink).Update("manage_stock", false).Error)
	f.recipe(f.drink, 0, 1, f.line(f.cheese, 20, "GRM"), f.line(f.meat, 10, "GRM")) // mismos insumos, en otro orden
	before := f.supplies()
	sqlDB, _ := f.db.DB()
	sqlDB.SetMaxOpenConns(3) // como el límite por empresa en producción

	const n = 12
	ids := make([]uint, n)
	errs := make(chan error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			items := []rsvc.NewOrderItem{item(f.burger, 1), item(f.drink, 1)}
			if i%2 == 1 {
				items[0], items[1] = items[1], items[0]
			}
			sale, err := f.quickErr(f.nvSeries, items...)
			if err != nil {
				errs <- err
				return
			}
			ids[i] = sale.ID
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("venta simultánea: %v", err)
	}
	f.wantStock("12 ventas a la vez", map[uint]float64{f.meat: 10 - n*0.16, f.bread: 100 - n, f.cheese: 5 - n*0.03})

	cancelErrs := make(chan error, 2*n)
	for _, id := range ids {
		for k := 0; k < 2; k++ { // cada venta se anula dos veces a la vez: solo una debe reponer
			wg.Add(1)
			go func(id uint) {
				defer wg.Done()
				cancelErrs <- salesvc.NewSaleService(f.db).CancelNotaVenta(id, 1, "simultánea")
			}(id)
		}
	}
	wg.Wait()
	close(cancelErrs)
	ok := 0
	for err := range cancelErrs {
		if err == nil {
			ok++
		} else if !strings.Contains(err.Error(), "ya está cancelada") {
			t.Fatalf("anulación simultánea: %v", err)
		}
	}
	if ok != n {
		t.Fatalf("anulaciones logradas: %d de %d", ok, n)
	}
	f.wantStock("todo anulado", before)
}
