package service

import (
	"strings"
	"testing"

	"tukifac/pkg/database"
)

// F0b: una venta se anula UNA sola vez: dos anulaciones a la vez no devuelven dos veces el dinero ni el stock.

func cancelFixture(t *testing.T) (*nvFixture, uint) {
	t.Helper()
	f := newNVInSession(t, nvSpec{total: 118})
	for _, m := range []interface{}{
		&database.TenantProduct{}, &database.TenantProductStock{}, &database.TenantStockMovement{}, &database.TenantProductSerial{},
		&database.TenantInventoryOperationType{}, &database.TenantProductPresentation{}, &database.TenantSalePrepaymentVoucher{},
		&database.TenantProductBranch{}, &database.TenantCategory{},
	} {
		if err := f.db.AutoMigrate(m); err != nil {
			t.Fatal(err)
		}
	}
	if err := database.SeedInventoryOperationTypes(f.db); err != nil {
		t.Fatal(err)
	}
	p := database.TenantProduct{Code: "P1", Name: "Producto", Type: "product", Unit: "NIU", ManageStock: true, BranchID: 1, Active: true}
	if err := f.db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	// La venta ya descontó 1 unidad (de 10 quedan 9), con su movimiento de kardex.
	for _, row := range []interface{}{
		&database.TenantProductStock{ProductID: p.ID, BranchID: 1, Quantity: 9},
		&database.TenantStockMovement{ProductID: p.ID, BranchID: 1, Type: "out", Quantity: 1, Balance: 9, Reference: "VENTA/" + f.nvNumber, UserID: 1},
	} {
		if err := f.db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	return f, p.ID
}

func assertSingleReversal(t *testing.T, f *nvFixture, productID uint) {
	t.Helper()
	var stock database.TenantProductStock
	f.db.Where("product_id = ?", productID).First(&stock)
	if stock.Quantity != 10 {
		t.Fatalf("stock = %.0f, quería 10 (repuesto una sola vez)", stock.Quantity)
	}
	var restored int64
	f.db.Model(&database.TenantStockMovement{}).Where("reference = ?", "ANULACION VENTA/"+f.nvNumber).Count(&restored)
	if restored != 1 {
		t.Fatalf("movimientos de reposición = %d, quería 1", restored)
	}
	var expenses float64
	f.db.Model(&database.TenantCashMovement{}).Where("type = ?", "expense").Select("COALESCE(SUM(amount),0)").Scan(&expenses)
	if expenses != 118 {
		t.Fatalf("devolución en caja = %.2f, quería 118 una sola vez", expenses)
	}
}

func duringCancel(t *testing.T, other func()) {
	t.Helper()
	done := false
	cancelBeforeTxHook = func() {
		if done {
			return
		}
		done = true
		other()
	}
	t.Cleanup(func() { cancelBeforeTxHook = nil })
}

func TestCancel_dobleAnulacionDeNotaDeVenta_unaSolaReversa(t *testing.T) {
	f, pid := cancelFixture(t)
	duringCancel(t, func() {
		if err := NewSaleService(f.db).CancelNotaVenta(f.nvID, 1, "otra caja"); err != nil {
			t.Fatal(err)
		}
	})
	err := NewSaleService(f.db).CancelNotaVenta(f.nvID, 1, "esta caja")
	if err == nil || !strings.Contains(err.Error(), "ya está cancelada") {
		t.Fatalf("la segunda debe fallar con el mensaje de siempre: %v", err)
	}
	assertSingleReversal(t, f, pid)
}

func TestCancel_dobleAnulacionInterna_unaSolaReversa(t *testing.T) {
	f, pid := cancelFixture(t)
	duringCancel(t, func() {
		if err := NewSaleService(f.db).Cancel(f.nvID, 1, "nota de crédito aceptada"); err != nil {
			t.Fatal(err)
		}
	})
	err := NewSaleService(f.db).Cancel(f.nvID, 1, "la misma por otra vía")
	if err == nil || !strings.Contains(err.Error(), "ya está cancelada") {
		t.Fatalf("la segunda debe fallar: %v", err)
	}
	assertSingleReversal(t, f, pid)
}

func TestCancel_anulacionNormalSigueIgual(t *testing.T) {
	f, pid := cancelFixture(t)
	if err := NewSaleService(f.db).CancelNotaVenta(f.nvID, 1, "motivo"); err != nil {
		t.Fatal(err)
	}
	var sale database.TenantSale
	f.db.First(&sale, f.nvID)
	if sale.Status != "cancelled" || !strings.Contains(sale.Notes, "ANULADA: motivo") {
		t.Fatalf("estado=%s notas=%q", sale.Status, sale.Notes)
	}
	assertSingleReversal(t, f, pid)
}
