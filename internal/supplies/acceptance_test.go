// Aceptación de F1a (catálogo de insumos) por los servicios reales. Ver docs/recetas/F1_PROPUESTA.md §6.
//
// Corre en SQLite y, con TUKIFAC_TEST_MYSQL_DSN (BD desechable cuyo nombre contenga "test"), en MySQL/MariaDB.
package supplies_test

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	ecommercesvc "tukifac/internal/ecommerce/service"
	invsvc "tukifac/internal/inventory/service"
	productsvc "tukifac/internal/products/service"
	purchasesvc "tukifac/internal/purchases/service"
	quotationsvc "tukifac/internal/quotations/service"
	restaurantsvc "tukifac/internal/restaurant/service"
	salesvc "tukifac/internal/sales/service"
	"tukifac/pkg/database"
	"tukifac/pkg/database/tenantmigrations"
	"tukifac/pkg/saas"
	"tukifac/pkg/supplies"
	"tukifac/pkg/tax"

	"github.com/glebarez/sqlite"
	mysqldrv "gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type fixture struct {
	db     *gorm.DB
	dish   database.TenantProduct // plato vendible
	supply database.TenantProduct // insumo
	series database.TenantDocumentSeries
}

func openDB(t *testing.T) *gorm.DB {
	t.Helper()
	cfg := &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)}
	dsn := os.Getenv("TUKIFAC_TEST_MYSQL_DSN")
	if dsn == "" {
		db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))), cfg)
		if err != nil {
			t.Fatal(err)
		}
		return db
	}
	db, err := gorm.Open(mysqldrv.Open(dsn), cfg)
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
	sqlDB.SetMaxOpenConns(30)
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func schema(t *testing.T, db *gorm.DB) {
	t.Helper()
	must(t, database.ApplyBaselineSchema(db))
	must(t, (tenantmigrations.V083InventoryIngressEgress{}).Up(db))
	must(t, (tenantmigrations.V150SaleIdempotencyKey{}).Up(db))
	for _, m := range []interface{}{
		&database.TenantBranchDailyComandaCounter{}, &database.TenantSaleCreditInstallment{}, &database.TenantSaleDetraccion{},
		&database.TenantProductBranch{}, &database.TenantProductBranchPrice{}, &database.TenantProductSaleUnit{},
		&database.TenantProductSaleUnitBranchPrice{}, &database.TenantProductAttribute{}, &database.TenantProductModifierGroup{},
		&database.TenantComboGroup{}, &database.TenantComboGroupItem{}, &database.TenantRestaurantSetting{}, &database.TenantProduct{},
		&database.TenantQuotation{}, &database.TenantQuotationItem{}, &database.TenantEcommerceOrder{},
		&database.TenantPurchase{}, &database.TenantPurchaseItem{},
	} {
		must(t, db.AutoMigrate(m))
	}
}

func setup(t *testing.T) fixture {
	t.Helper()
	db := openDB(t)
	schema(t, db)
	must(t, db.Create(&database.TenantCompanyConfig{ID: 1, SunatEnabled: true, TaxRate: 18}).Error)
	must(t, db.Create(&database.TenantBranch{Name: "Principal", Active: true, IsMain: true}).Error)
	must(t, db.Create(&database.TenantBranch{Name: "Dos", Active: true}).Error)
	must(t, database.SeedInventoryOperationTypes(db))
	must(t, db.Where(database.TenantPaymentMethod{Code: "cash"}).Attrs(database.TenantPaymentMethod{Name: "Efectivo", IsSystem: true, Active: true}).
		FirstOrCreate(&database.TenantPaymentMethod{}).Error)
	must(t, db.Create(&database.TenantCashSession{BranchID: 1, UserID: 1, OpenedBy: 1, Status: "open", OpenedAt: time.Now()}).Error)
	f := fixture{db: db}
	f.series = database.TenantDocumentSeries{BranchID: 1, DocType: "Boleta", SunatCode: "03", Series: "B001", Correlative: 1, Active: true}
	must(t, db.Create(&f.series).Error)
	ps := productsvc.NewProductService(db)
	dish, _, err := ps.Create(productsvc.ProductInput{Code: "PLATO", Name: "Lomo saltado", Type: "product", Unit: "NIU", SalePrice: 20,
		IgvAffectationType: "10", PriceIncludesIgv: true, IsRestaurant: true, BranchID: 1, Active: true})
	must(t, err)
	// El cuerpo pide cosas que un insumo no puede tener: deben quedar forzadas.
	supply, _, err := ps.Create(productsvc.ProductInput{IsSupply: true, Code: "INS-ARROZ", Name: "Arroz", Unit: "KGM", SalePrice: 99,
		PurchasePrice: 3.5, IgvAffectationType: "10", PriceIncludesIgv: true, IsRestaurant: true, ShowInDigitalCatalog: true,
		ManageSeries: true, HasModifiers: true, BranchID: 2, Active: true, ActiveSet: true})
	must(t, err)
	f.dish, f.supply = *dish, *supply
	return f
}

func reload(t *testing.T, db *gorm.DB, id uint) database.TenantProduct {
	t.Helper()
	var p database.TenantProduct
	must(t, db.First(&p, id).Error)
	return p
}

func assertSupplyShape(t *testing.T, p database.TenantProduct) {
	t.Helper()
	if !p.IsSupply || p.SupplyKind != "raw" || p.Type != "product" || p.SalePrice != 0 || !p.ManageStock || p.IsRestaurant ||
		p.ShowInDigitalCatalog || p.ManageSeries || p.HasModifiers || p.HasVariants || p.HasCombo || p.BranchID != 0 || p.CategoryID != nil {
		t.Fatalf("el insumo no quedó con la forma esperada: %+v", p)
	}
}

func TestSupplyShapeCodesAndImmutability(t *testing.T) {
	f := setup(t)
	ps := productsvc.NewProductService(f.db)
	assertSupplyShape(t, reload(t, f.db, f.supply.ID))
	if d := reload(t, f.db, f.dish.ID); d.IsSupply {
		t.Fatal("el plato no debe quedar como insumo")
	}

	// Editar como producto no cambia la clase ni habilita nada de venta.
	_, err := ps.Update(f.supply.ID, productsvc.ProductInput{Code: "INS-ARROZ", Name: "Arroz extra", Type: "service", Unit: "KGM",
		SalePrice: 50, IsRestaurant: true, ShowInDigitalCatalog: true, Active: true, ActiveSet: true})
	must(t, err)
	assertSupplyShape(t, reload(t, f.db, f.supply.ID))

	// Los códigos no se cruzan, tampoco entre sucursales.
	if _, _, err := ps.Create(productsvc.ProductInput{Code: "INS-ARROZ", Name: "x", Unit: "NIU", IsRestaurant: true, BranchID: 2, SalePrice: 1}); err == nil {
		t.Fatal("un plato no puede usar el código de un insumo")
	}
	if _, _, err := ps.Create(productsvc.ProductInput{IsSupply: true, Code: "PLATO", Name: "x", Unit: "NIU"}); err == nil {
		t.Fatal("un insumo no puede usar el código de un producto")
	}
	if _, err := ps.Update(f.dish.ID, productsvc.ProductInput{Code: "INS-ARROZ", Name: "Lomo", Unit: "NIU", SalePrice: 20, IsRestaurant: true}); err == nil {
		t.Fatal("editar un plato no puede tomar el código de un insumo")
	}

	// Reactivar por código no cambia la clase.
	must(t, ps.Delete(f.supply.ID))
	if _, _, err := ps.Create(productsvc.ProductInput{Code: "INS-ARROZ", Name: "x", Unit: "NIU", SalePrice: 1}); err == nil {
		t.Fatal("reactivar un insumo como producto debe fallar")
	}
	back, _, err := ps.Create(productsvc.ProductInput{IsSupply: true, Code: "INS-ARROZ", Name: "Arroz", Unit: "KGM", Active: true})
	must(t, err)
	if back.ID != f.supply.ID {
		t.Fatalf("debía reactivar el mismo insumo: %d != %d", back.ID, f.supply.ID)
	}
	assertSupplyShape(t, reload(t, f.db, f.supply.ID))
}

func TestSupplyHiddenFromSalesAndQuota(t *testing.T) {
	f := setup(t)
	ps := productsvc.NewProductService(f.db)
	ids := func(p productsvc.ProductListParams) []uint {
		t.Helper()
		rows, total, err := ps.List(p)
		must(t, err)
		out := []uint{}
		for _, r := range rows {
			out = append(out, r.ID)
		}
		if p.Limit > 0 && int(total) != len(out) {
			t.Fatalf("total %d != filas %d (%+v)", total, len(out), p)
		}
		return out
	}
	has := func(list []uint, id uint) bool {
		for _, v := range list {
			if v == id {
				return true
			}
		}
		return false
	}
	for name, p := range map[string]productsvc.ProductListParams{
		"todo":             {},
		"búsqueda":         {Query: "Arroz"},
		"sucursal":         {BranchID: 1},
		"carta":            {RestaurantOnly: true, BranchID: 1},
		"paginado":         {Limit: 50},
		"con stock":        {ManageStockOnly: true},
		"tienda":           {ShowInDigitalCatalogOnly: true},
		"sin combos":       {ExcludeCombos: true, ActiveOnly: true},
		"bajo stock":       {StockLessThan: new(float64)},
		"tipo producto":    {Type: "product"},
		"sucursal y stock": {BranchID: 2, ManageStockOnly: true},
	} {
		if has(ids(p), f.supply.ID) {
			t.Fatalf("%s: el insumo no debe aparecer", name)
		}
	}
	if got := ids(productsvc.ProductListParams{SupplyMode: supplies.ModeOnly, Limit: 10}); len(got) != 1 || got[0] != f.supply.ID {
		t.Fatalf("solo insumos: %v", got)
	}
	// Con stock solo en la sucursal 1, igual se ve en la 2.
	must(t, f.db.Create(&database.TenantProductStock{ProductID: f.supply.ID, BranchID: 1, Quantity: 4}).Error)
	for _, p := range []productsvc.ProductListParams{
		{SupplyMode: supplies.ModeOnly, BranchID: 2},
		{SupplyMode: supplies.ModeInclude, BranchID: 2},
		{SupplyMode: supplies.ModeInclude, RestaurantOnly: true, BranchID: 1},
		{SupplyMode: supplies.ModeInclude},
	} {
		if !has(ids(p), f.supply.ID) {
			t.Fatalf("debía aparecer con %+v", p)
		}
	}
	if !has(ids(productsvc.ProductListParams{SupplyMode: supplies.ModeInclude, RestaurantOnly: true, BranchID: 1}), f.dish.ID) {
		t.Fatal("con include la carta conserva los platos")
	}
	for _, code := range []string{"INS-ARROZ"} {
		p, err := ps.FindProductByBarcode(code, 1)
		must(t, err)
		if p != nil {
			t.Fatal("el lector no debe encontrar un insumo")
		}
	}
	if n := saas.CountProductSeats(f.db); n != 1 {
		t.Fatalf("cupo: %d, want 1 (el insumo no ocupa)", n)
	}
	if _, err := ecommercesvc.NewEcommerceService(f.db).PublicProductGallery(f.supply.ID); err == nil {
		t.Fatal("la tienda no debe exponer un insumo")
	}
}

func TestSupplyRejectedEverywhereItCouldBeSold(t *testing.T) {
	f := setup(t)
	sid := f.supply.ID
	ps := productsvc.NewProductService(f.db)
	rejected := func(what string, err error) {
		t.Helper()
		if err == nil || !strings.Contains(err.Error(), "insumo") {
			t.Fatalf("%s: debía rechazarlo por ser insumo, obtuve %v", what, err)
		}
	}
	count := func(m interface{}) int64 {
		var n int64
		must(t, f.db.Model(m).Count(&n).Error)
		return n
	}

	// Venta (POS, nota, conversión, pedidos web convertidos).
	sale := func(items []salesvc.SaleItemInput, extra []salesvc.ExtraStockMovement) error {
		_, err := salesvc.NewSaleService(f.db).Create(salesvc.CreateSaleInput{
			BranchID: 1, UserID: 1, SeriesID: f.series.ID, DocType: "03", IssueDate: time.Now(), Currency: "PEN",
			TaxConfig: tax.DefaultConfig(), Payments: []salesvc.PaymentInput{{Method: "cash", Amount: 20}},
			Items: items, ExtraStockMovements: extra, UserCanOverridePrice: true,
		})
		return err
	}
	line := func(id uint) salesvc.SaleItemInput {
		return salesvc.SaleItemInput{ProductID: &id, Description: "x", Code: "X", Unit: "NIU", Quantity: 1, UnitPrice: 20,
			IgvAffectationType: "10", PriceIncludesIgv: true}
	}
	rejected("venta", sale([]salesvc.SaleItemInput{line(f.dish.ID), line(sid)}, nil))
	rejected("salida extra", sale([]salesvc.SaleItemInput{line(f.dish.ID)}, []salesvc.ExtraStockMovement{{ProductID: sid, Quantity: 1}}))
	if count(&database.TenantSale{}) != 0 {
		t.Fatal("no debe quedar ninguna venta")
	}
	var ser database.TenantDocumentSeries
	must(t, f.db.First(&ser, f.series.ID).Error)
	if ser.Correlative != 1 {
		t.Fatalf("no debe consumir correlativo: %d", ser.Correlative)
	}
	must(t, sale([]salesvc.SaleItemInput{line(f.dish.ID)}, nil)) // el plato se sigue vendiendo

	// Mesa: pedido y cobro con una comanda inyectada.
	rs := restaurantsvc.New(f.db)
	floor := database.TenantRestaurantFloor{BranchID: 1, Name: "Sala", Active: true}
	must(t, f.db.Create(&floor).Error)
	table := database.TenantRestaurantTable{BranchID: 1, FloorID: floor.ID, Name: "M1", Capacity: 4, Status: "libre", Active: true}
	must(t, f.db.Create(&table).Error)
	tid := table.ID
	sess, err := rs.OpenTableExtended(restaurantsvc.OpenSessionInput{TableID: &tid, UserID: 1, BranchID: 1, Guests: 2})
	must(t, err)
	_, err = rs.AddOrder(sess.ID, nil, 1, []restaurantsvc.NewOrderItem{{ProductID: &sid, ProductName: "Arroz", Quantity: 1, UnitPrice: 5}}, "")
	rejected("pedido de mesa", err)
	if count(&database.TenantComanda{}) != 0 {
		t.Fatal("no debe quedar ninguna comanda")
	}
	must(t, f.db.Create(&database.TenantComanda{SessionID: sess.ID, ProductID: &sid, ProductCode: "INS-ARROZ", ProductName: "Arroz",
		Quantity: 1, UnitPrice: 5, Status: "pendiente", IgvAffectationType: "10", PriceIncludesIgv: true}).Error)
	_, err = rs.BillTable(restaurantsvc.BillInput{SessionID: sess.ID, UserID: 1, SeriesID: f.series.ID, DocType: "03",
		IssueDate: time.Now(), CloseSession: true, Payments: []restaurantsvc.PaymentInput{{Method: "cash", Amount: 5}}}, tax.DefaultConfig())
	rejected("cobro de mesa", err)
	if count(&database.TenantSale{}) != 1 {
		t.Fatal("el cobro rechazado no debe crear venta")
	}

	// Cotización y pedido web.
	_, err = quotationsvc.NewQuotationService(f.db).Create(quotationsvc.CreateQuotationInput{
		BranchID: 1, UserID: 1, SeriesID: quotationSeries(t, f.db), IssueDate: time.Now(), Currency: "PEN", TaxConfig: tax.DefaultConfig(),
		Items: []quotationsvc.QuotationItemInput{{ProductID: &sid, Description: "Arroz", Quantity: 1, UnitPrice: 5, IgvAffectationType: "10"}},
	})
	rejected("cotización", err)
	// Aunque alguien lo hubiera marcado como publicado por fuera, la tienda no lo acepta.
	must(t, f.db.Table("tenant_products").Where("id = ?", sid).Update("show_in_digital_catalog", true).Error)
	_, err = ecommercesvc.NewEcommerceService(f.db).CreateOrder(ecommercesvc.CreateOrderInput{
		CustomerName: "Ana", CustomerPhone: "999999999", Items: []ecommercesvc.OrderItemInput{{ProductID: sid, Name: "Arroz", Quantity: 1}},
	})
	if err == nil {
		t.Fatal("pedido web: debía rechazarlo")
	}
	must(t, f.db.Table("tenant_products").Where("id = ?", sid).Update("show_in_digital_catalog", false).Error)

	// Combo, masivos, precios.
	_, err = ps.Update(f.dish.ID, productsvc.ProductInput{Code: "PLATO", Name: "Lomo saltado", Unit: "NIU", SalePrice: 20, IsRestaurant: true,
		BranchID: 1, ComboGroups: &[]productsvc.ComboGroupInput{{Name: "G", SelectionType: "single", MinSelect: 1, MaxSelect: 1,
			Items: []productsvc.ComboGroupItemInput{{ProductID: sid, DefaultQuantity: 1, MaxQuantity: 1, IsDefault: true}}}}})
	rejected("combo", err)
	yes, no := true, false
	for what, in := range map[string]productsvc.BulkUpdateCatalogInput{
		"publicar":       {ProductIDs: []uint{sid}, ShowInDigitalCatalog: &yes},
		"llevar a carta": {ProductIDs: []uint{sid}, IsRestaurant: &yes},
		"quitar stock":   {ProductIDs: []uint{sid}, ManageStock: &no},
	} {
		_, err = ps.BulkUpdateCatalog(in)
		rejected("masivo "+what, err)
	}
	_, err = ps.CreateProductBranchPrice(sid, 1, productsvc.ProductBranchPriceInput{})
	rejected("precio por sucursal", err)
	price := 12.0
	res, err := ps.BulkUpdatePrices([]productsvc.BulkPriceUpdateRow{{RowNumber: 2, Code: "INS-ARROZ", SalePrice: &price}})
	must(t, err)
	if res.Rows[0].Status != "error" {
		t.Fatalf("actualización de precios: %+v", res.Rows[0])
	}
	imp, err := ps.BulkImportCatalog([]productsvc.BulkImportItem{{RowNumber: 2, Code: "INS-ARROZ", Name: "Arroz venta", SalePrice: 9, ManageStock: true}}, 1, 1)
	must(t, err)
	if len(imp.Failed) != 1 {
		t.Fatalf("importación: debía fallar la fila, %+v", imp)
	}
	assertSupplyShape(t, reload(t, f.db, sid))

	// Compra con "actualizar precio de venta".
	_, err = purchasesvc.NewPurchaseService(f.db).Create(purchaseInput(sid, true, "F001", "1"))
	rejected("compra actualizando precio de venta", err)
}

func quotationSeries(t *testing.T, db *gorm.DB) uint {
	t.Helper()
	s := database.TenantDocumentSeries{BranchID: 1, DocType: "Cotización", SunatCode: "COT", Series: "COT1", Correlative: 1, Active: true, Category: "cotizacion"}
	must(t, db.Create(&s).Error)
	return s.ID
}

func purchaseInput(productID uint, updateSalePrice bool, series, number string) purchasesvc.CreatePurchaseInput {
	return purchasesvc.CreatePurchaseInput{
		BranchID: 1, UserID: 1, DocType: "01", Series: series, Number: number, IssueDate: time.Now(), Currency: "PEN",
		PaymentMethod: "cash", TaxConfig: tax.DefaultConfig(),
		Items: []purchasesvc.PurchaseItemInput{{ProductID: &productID, Code: "INS-ARROZ", Description: "Arroz", Unit: "KGM",
			Quantity: 10, UnitCost: 4, UpdateSalePrice: updateSalePrice, NewSalePrice: 6, IgvAffectationType: "10", PriceIncludesIgv: true}},
	}
}

func TestSupplyAcceptedInPurchasesAndInventory(t *testing.T) {
	f := setup(t)
	sid := f.supply.ID
	stock := func(branch uint) float64 {
		var q float64
		must(t, f.db.Model(&database.TenantProductStock{}).Where("product_id = ? AND branch_id = ?", sid, branch).
			Select("COALESCE(SUM(quantity),0)").Scan(&q).Error)
		return q
	}
	_, err := purchasesvc.NewPurchaseService(f.db).Create(purchaseInput(sid, false, "F001", "2"))
	must(t, err)
	if stock(1) != 10 {
		t.Fatalf("compra: stock %v", stock(1))
	}
	assertSupplyShape(t, reload(t, f.db, sid))

	inv := invsvc.NewInventoryService(f.db)
	must(t, inv.RecordMovement(invsvc.MovementInput{ProductID: sid, BranchID: 1, Type: "adjustment", Quantity: 8, UserID: 1, Reference: "AJUSTE"}))
	must(t, inv.TransferByProduct(sid, 1, 2, 3, 1, ""))
	if stock(2) != 3 {
		t.Fatalf("traslado: destino %v", stock(2))
	}
	rows, _, err := inv.GetKardex(invsvc.KardexParams{ProductID: sid, RestaurantOnly: true, Limit: 20})
	must(t, err)
	if len(rows) < 4 {
		t.Fatalf("kardex de restaurante debe incluir al insumo: %d filas", len(rows))
	}
	summary, err := inv.StockSummary(1)
	must(t, err)
	found := false
	for _, s := range summary {
		if s.ProductID == sid {
			found = s.IsSupply
		}
	}
	if !found {
		t.Fatalf("el resumen de stock debe traer el insumo rotulado: %+v", summary)
	}

	// Un insumo con movimientos no se purga y no recibe sucursales fijas.
	ps := productsvc.NewProductService(f.db)
	res, err := ps.BulkDeleteCatalog(productsvc.BulkDeleteCatalogInput{ProductIDs: []uint{sid}, Reason: "limpieza", UserID: 1})
	must(t, err)
	if len(res.Blocked) != 1 || len(res.Deleted) != 0 {
		t.Fatalf("borrado masivo: %+v", res)
	}

	// Unidades de compra sin precio.
	var unit database.TenantUnit
	must(t, f.db.Where(database.TenantUnit{Code: "BX"}).Attrs(database.TenantUnit{Name: "Caja", Active: true}).FirstOrCreate(&unit).Error)
	su, err := ps.CreateSaleUnit(sid, productsvc.SaleUnitInput{Name: "Saco 50 kg", UnitID: &unit.ID, ConversionFactor: 50, Price1: 7, Active: true})
	must(t, err)
	if su.Price1 != 0 {
		t.Fatalf("la unidad de compra de un insumo no lleva precio: %v", su.Price1)
	}
	if _, err := ps.CreateSaleUnit(f.dish.ID, productsvc.SaleUnitInput{Name: "x", UnitID: &unit.ID, ConversionFactor: 6, Active: true}); err == nil {
		t.Fatal("un producto sigue exigiendo precio en sus unidades de venta")
	}
}

// Tenant sin V195: todo se comporta como antes; después de migrar, nada existente cambia.
func TestSupplyUnmigratedTenant(t *testing.T) {
	db := openDB(t)
	schema(t, db)
	for _, idx := range []string{"idx_tenant_products_is_supply"} {
		if db.Migrator().HasIndex("tenant_products", idx) {
			must(t, db.Migrator().DropIndex(&database.TenantProduct{}, idx))
		}
	}
	for _, col := range []string{"is_supply", "supply_kind"} {
		must(t, db.Exec("ALTER TABLE tenant_products DROP COLUMN "+col).Error)
	}
	must(t, db.Exec("ALTER TABLE tenant_restaurant_settings DROP COLUMN recipes_enabled").Error)
	must(t, db.Create(&database.TenantBranch{Name: "Principal", Active: true, IsMain: true}).Error)

	ps := productsvc.NewProductService(db)
	p, _, err := ps.Create(productsvc.ProductInput{Code: "A", Name: "Gaseosa", Unit: "NIU", SalePrice: 3, ManageStock: true, Active: true})
	must(t, err)
	_, err = ps.Update(p.ID, productsvc.ProductInput{Code: "A", Name: "Gaseosa 500", Unit: "NIU", SalePrice: 4, ManageStock: true, Active: true, ActiveSet: true})
	must(t, err)
	for _, mode := range []string{supplies.ModeExclude, supplies.ModeInclude} {
		rows, _, err := ps.List(productsvc.ProductListParams{SupplyMode: mode, BranchID: 1})
		must(t, err)
		if len(rows) != 1 {
			t.Fatalf("modo %q: %d filas", mode, len(rows))
		}
	}
	if rows, _, err := ps.List(productsvc.ProductListParams{SupplyMode: supplies.ModeOnly}); err != nil || len(rows) != 0 {
		t.Fatalf("sin columna no hay insumos: %d %v", len(rows), err)
	}
	if n := saas.CountProductSeats(db); n != 1 {
		t.Fatalf("el cupo no puede desactivarse sin la columna: %d", n)
	}
	if _, _, err := ps.Create(productsvc.ProductInput{IsSupply: true, Name: "Arroz", Unit: "KGM"}); err == nil {
		t.Fatal("sin migrar no se crean insumos")
	}
	if database.RecipesEnabled(db) || database.SetRecipesEnabled(db, true) == nil {
		t.Fatal("sin migrar el módulo no se puede activar")
	}
	if err := supplies.Reject(db, p.ID); err != nil && errors.Is(err, supplies.ErrNotSellable) {
		t.Fatal(err)
	}

	// Migrar: idempotente, y lo existente queda como producto normal con el módulo apagado.
	for i := 0; i < 2; i++ {
		must(t, (tenantmigrations.V195Supplies{}).Up(db))
	}
	var n int64
	must(t, db.Raw("SELECT COUNT(*) FROM tenant_products WHERE is_supply = ?", false).Scan(&n).Error)
	if n != 1 {
		t.Fatalf("producto existente debe quedar como no insumo: %d", n)
	}
	if database.RecipesEnabled(db) {
		t.Fatal("el módulo nace apagado")
	}
	must(t, database.SetRecipesEnabled(db, true))
	if !database.RecipesEnabled(db) || !database.TenantFeatureFlags(db)["restaurant_recipes"] && database.SchemaAtLeast(db, 35) {
		t.Fatal("el módulo debía quedar activado")
	}
}
