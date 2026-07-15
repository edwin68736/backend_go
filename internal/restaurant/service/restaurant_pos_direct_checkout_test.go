package service

import (
	"strings"
	"testing"

	"tukifac/pkg/database"
	"tukifac/pkg/tax"

	"gorm.io/gorm"
)

// setupDirectCheckoutDB extiende el fixture de combos con lo que necesita emitir una venta.
func setupDirectCheckoutDB(t *testing.T) (*gorm.DB, comboFixture) {
	t.Helper()
	db, _ := setupComboOrderTestDB(t)
	if err := db.AutoMigrate(
		&database.TenantBranch{},
		&database.TenantProductStock{},
		&database.TenantStockMovement{},
		&database.TenantInventoryOperationType{},
		&database.TenantCompanyConfig{},
	); err != nil {
		t.Fatal(err)
	}
	if err := database.SeedInventoryOperationTypes(db); err != nil {
		t.Fatal(err)
	}
	f := seedComboFamiliar(t, db)
	return db, f
}

func directCheckoutInput(f comboFixture, items []NewOrderItem, seriesID uint) POSCheckoutInput {
	return POSCheckoutInput{
		BranchID:  1,
		UserID:    1,
		OrderType: OrderTypeQuickSale,
		Items:     items,
		SeriesID:  seriesID,
		DocType:   "03",
		Payments:  []PaymentInput{{Method: "card", Amount: 20}},
	}
}

func TestIsDirectSaleCheckout(t *testing.T) {
	sessionID := uint(7)
	cases := []struct {
		name string
		in   POSCheckoutInput
		want bool
	}{
		{"venta rápida sin sesión", POSCheckoutInput{OrderType: OrderTypeQuickSale}, true},
		{"sin order_type y sin mesa: se normaliza a venta rápida", POSCheckoutInput{}, true},
		{"venta rápida con sesión previa (borrador POS)", POSCheckoutInput{OrderType: OrderTypeQuickSale, SessionID: &sessionID}, false},
		{"para llevar: puede mandar a cocina", POSCheckoutInput{OrderType: OrderTypeTakeaway}, false},
		{"delivery: puede mandar a cocina", POSCheckoutInput{OrderType: OrderTypeDelivery}, false},
		{"en mesa", POSCheckoutInput{OrderType: OrderTypeDineIn}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isDirectSaleCheckout(tc.in); got != tc.want {
				t.Errorf("isDirectSaleCheckout = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestResolveDirectSaleItems_PlainProducts: el carrito se traduce a líneas de venta con el
// precio del catálogo, sin tocar sesiones ni comandas.
func TestResolveDirectSaleItems_PlainProducts(t *testing.T) {
	db, f := setupDirectCheckoutDB(t)
	svc := NewRestaurantPOSCheckoutService(db)

	polloID, aguaID := f.Pollo.ID, f.Agua.ID
	items, extra, err := svc.resolveDirectSaleItems([]NewOrderItem{
		{ProductID: &polloID, Quantity: 2},
		{ProductID: &aguaID, Quantity: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("esperaba 2 líneas, got %d", len(items))
	}
	if items[0].Description != "Pollo a la brasa" || items[0].UnitPrice != 20 || items[0].Quantity != 2 {
		t.Errorf("línea del pollo inesperada: %+v", items[0])
	}
	if items[0].IgvAffectationType != "10" {
		t.Errorf("esperaba afectación 10, got %q", items[0].IgvAffectationType)
	}
	if len(extra) != 0 {
		t.Errorf("sin combos no debe haber kardex extra, got %d", len(extra))
	}
}

// TestResolveDirectSaleItems_ComboBecomesOneLine: el combo se factura como una sola línea a
// su precio fijo, y el stock sale de los componentes.
func TestResolveDirectSaleItems_ComboBecomesOneLine(t *testing.T) {
	db, f := setupDirectCheckoutDB(t)
	svc := NewRestaurantPOSCheckoutService(db)

	comboID := f.Combo.ID
	items, extra, err := svc.resolveDirectSaleItems([]NewOrderItem{{
		ProductID: &comboID, Quantity: 2,
		ComboJSON: comboSelectionJSON(t, f.BebidaG.ID, f.Agua.ID, 1),
	}})
	if err != nil {
		t.Fatal(err)
	}

	if len(items) != 1 {
		t.Fatalf("el combo es una sola línea facturable, got %d", len(items))
	}
	line := items[0]
	if line.ProductID == nil || *line.ProductID != comboID {
		t.Errorf("la línea debe apuntar al combo, got %v", line.ProductID)
	}
	if line.UnitPrice != 18 || line.Quantity != 2 {
		t.Errorf("esperaba 2 x 18.00, got %g x %.2f", line.Quantity, line.UnitPrice)
	}
	// Los componentes viajan como detalle de la línea, igual que en el flujo de mesas.
	if !strings.Contains(line.ModifiersJSON, "Pollo a la brasa") ||
		!strings.Contains(line.ModifiersJSON, "Agua mineral") {
		t.Errorf("esperaba los componentes en el detalle, got %s", line.ModifiersJSON)
	}

	// 2 combos = 2 pollos + 2 aguas de almacén, aunque la venta tenga una sola línea.
	if len(extra) != 2 {
		t.Fatalf("esperaba kardex de 2 componentes, got %d", len(extra))
	}
	byProduct := map[uint]float64{}
	for _, mv := range extra {
		byProduct[mv.ProductID] = mv.Quantity
	}
	if byProduct[f.Pollo.ID] != 2 || byProduct[f.Agua.ID] != 2 {
		t.Errorf("esperaba 2 de cada componente, got %+v", byProduct)
	}
	if _, ok := byProduct[comboID]; ok {
		t.Error("el combo no tiene stock propio: no debe generar kardex")
	}
}

// TestResolveDirectSaleItems_ComboExtraPrice: elegir la opción premium sube el precio, igual
// que en el flujo de mesas.
func TestResolveDirectSaleItems_ComboExtraPrice(t *testing.T) {
	db, f := setupDirectCheckoutDB(t)
	svc := NewRestaurantPOSCheckoutService(db)

	comboID := f.Combo.ID
	items, _, err := svc.resolveDirectSaleItems([]NewOrderItem{{
		ProductID: &comboID, Quantity: 1,
		ComboJSON: comboSelectionJSON(t, f.BebidaG.ID, f.Gaseosa.ID, 1),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if items[0].UnitPrice != 19.50 {
		t.Errorf("precio = %.2f, want 19.50 (18 + 1.50 de la gaseosa)", items[0].UnitPrice)
	}
}

// TestResolveDirectSaleItems_ComboComponentsAccumulate: el mismo componente en dos líneas
// deja un solo asiento de kardex con la cantidad sumada.
func TestResolveDirectSaleItems_ComboComponentsAccumulate(t *testing.T) {
	db, f := setupDirectCheckoutDB(t)
	svc := NewRestaurantPOSCheckoutService(db)

	comboID := f.Combo.ID
	sel := comboSelectionJSON(t, f.BebidaG.ID, f.Agua.ID, 1)
	_, extra, err := svc.resolveDirectSaleItems([]NewOrderItem{
		{ProductID: &comboID, Quantity: 1, ComboJSON: sel},
		{ProductID: &comboID, Quantity: 2, ComboJSON: sel},
	})
	if err != nil {
		t.Fatal(err)
	}
	byProduct := map[uint]float64{}
	for _, mv := range extra {
		byProduct[mv.ProductID] += mv.Quantity
	}
	if len(extra) != 2 {
		t.Fatalf("esperaba un asiento por componente, got %d", len(extra))
	}
	if byProduct[f.Pollo.ID] != 3 {
		t.Errorf("1 + 2 combos = 3 pollos, got %g", byProduct[f.Pollo.ID])
	}
}

// TestResolveDirectSaleItems_RejectsInvalidCombo: la validación del combo sigue viva en la
// venta directa; no se puede colar una selección inválida por saltarse AddOrder.
func TestResolveDirectSaleItems_RejectsInvalidCombo(t *testing.T) {
	db, f := setupDirectCheckoutDB(t)
	svc := NewRestaurantPOSCheckoutService(db)

	comboID := f.Combo.ID
	_, _, err := svc.resolveDirectSaleItems([]NewOrderItem{{
		ProductID: &comboID, Quantity: 1, ComboJSON: "",
	}})
	if err == nil || !strings.Contains(err.Error(), "debe elegir una opción") {
		t.Fatalf("esperaba rechazo por no elegir bebida, got %v", err)
	}
}

// TestCheckoutDirect_CreatesNoSessionNorComandas: el corazón del cambio. Una venta directa
// no debe dejar sesión, pedido ni comandas: era trabajo que se creaba para borrarlo.
func TestCheckoutDirect_CreatesNoSessionNorComandas(t *testing.T) {
	db, f := setupDirectCheckoutDB(t)
	if err := db.AutoMigrate(&database.TenantSale{}, &database.TenantSaleItem{}); err != nil {
		t.Fatal(err)
	}
	// La caja y los métodos de pago ya vienen sembrados por el fixture de mesas.
	// SaleService exige facturación electrónica habilitada para emitir boleta (03).
	if err := db.Create(&database.TenantCompanyConfig{SunatEnabled: true}).Error; err != nil {
		t.Fatal(err)
	}
	series := database.TenantDocumentSeries{
		BranchID: 1, DocType: "Boleta", SunatCode: "03", Series: "B001", Correlative: 1, Active: true,
	}
	if err := db.Create(&series).Error; err != nil {
		t.Fatal(err)
	}

	svc := NewRestaurantPOSCheckoutService(db)
	polloID := f.Pollo.ID
	in := directCheckoutInput(f, []NewOrderItem{{ProductID: &polloID, Quantity: 1}}, series.ID)

	if !isDirectSaleCheckout(in) {
		t.Fatal("el fixture debe ser una venta directa")
	}

	sale, err := svc.Checkout(in, tax.DefaultConfig())
	if err != nil {
		t.Fatalf("Checkout: %v", err)
	}
	if sale == nil || sale.ID == 0 {
		t.Fatal("esperaba una venta creada")
	}
	if sale.Number == "" {
		t.Error("la venta debe llevar número de comprobante")
	}

	// Lo que motivó el cambio: nada de este andamiaje se crea ya.
	var sessions, orders, comandas int64
	db.Model(&database.TenantTableSession{}).Count(&sessions)
	db.Model(&database.TenantTableOrder{}).Count(&orders)
	db.Model(&database.TenantComanda{}).Count(&comandas)
	if sessions != 0 {
		t.Errorf("una venta directa no debe crear sesión de mesa, got %d", sessions)
	}
	if orders != 0 {
		t.Errorf("una venta directa no debe crear pedido, got %d", orders)
	}
	if comandas != 0 {
		t.Errorf("una venta directa no debe crear comandas, got %d", comandas)
	}

	var itemCount int64
	db.Model(&database.TenantSaleItem{}).Where("sale_id = ?", sale.ID).Count(&itemCount)
	if itemCount != 1 {
		t.Errorf("esperaba 1 línea de venta, got %d", itemCount)
	}
}
