package service

import (
	"testing"

	"tukifac/pkg/database"
	"tukifac/pkg/dbtest"
	"tukifac/pkg/tax"
)

// Cobrar en restaurante debe usar una sola conexión: la de su propia transacción (ver pkg/dbtest).

func TestUnaConexion_CobroDeMesa(t *testing.T) {
	f := newBillFixture(t, 2)
	if err := dbtest.WithSingleConnection(t, f.db, func() error {
		_, err := f.svc.BillTable(f.input("una-conexion", 20), tax.DefaultConfig())
		return err
	}); err != nil {
		t.Fatalf("cobro: %v", err)
	}
}

func TestUnaConexion_RegistrarPagos(t *testing.T) {
	f := newBillFixture(t, 2)
	if _, err := f.svc.BillTable(f.input("primero", 20), tax.DefaultConfig()); err != nil {
		t.Fatalf("preparar la venta: %v", err)
	}
	var sale database.TenantSale
	if err := f.db.Order("id DESC").First(&sale).Error; err != nil {
		t.Fatal(err)
	}
	if err := dbtest.WithSingleConnection(t, f.db, func() error {
		return f.svc.RegisterPayments(sale.ID, []PaymentInput{{Method: "cash", Amount: 20}}, 1)
	}); err != nil {
		t.Fatalf("registrar pagos: %v", err)
	}
}
