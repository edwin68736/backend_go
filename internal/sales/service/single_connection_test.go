package service

import (
	"testing"
	"time"

	"tukifac/pkg/database"
	"tukifac/pkg/dbtest"
)

// Vender y anular deben usar una sola conexión: la de su propia transacción (ver pkg/dbtest).

func TestUnaConexion_Venta(t *testing.T) {
	db, seriesID := setupIdempotencyDB(t)
	svc := NewSaleService(db)
	if err := dbtest.WithSingleConnection(t, db, func() error {
		_, err := svc.Create(idemInput(seriesID, 1, "una-conexion"))
		return err
	}); err != nil {
		t.Fatalf("venta: %v", err)
	}
}

func TestUnaConexion_AnularVentaConCajaYBanco(t *testing.T) {
	db := setupCancelDB(t)
	sale := seedSaleWithStockOut(t, db, "open", nil)
	acc := database.TenantBankAccount{Name: "Yape", Type: "wallet", PaymentMethod: "yape", Active: true}
	db.Create(&acc)
	db.Create(&database.TenantBankMovement{BankAccountID: acc.ID, Type: "credit", Amount: 30, SaleID: &sale.ID, Reference: sale.Number, Date: time.Now(), UserID: 1})

	if err := dbtest.WithSingleConnection(t, db, func() error { return NewSaleService(db).Cancel(sale.ID, 1, "prueba") }); err != nil {
		t.Fatalf("anular: %v", err)
	}
}

// Ventas antiguas sin movimiento ligado: la reversa se reconstruye desde los pagos.
func TestUnaConexion_AnularVentaAntigua(t *testing.T) {
	db := setupCancelDB(t)
	sale := seedSaleWithStockOut(t, db, "open", nil)
	db.Where("sale_id = ?", sale.ID).Delete(&database.TenantCashMovement{})
	db.Create(&database.TenantSalePayment{SaleID: sale.ID, Method: "cash", Amount: 100})

	if err := dbtest.WithSingleConnection(t, db, func() error { return NewSaleService(db).Cancel(sale.ID, 1, "prueba") }); err != nil {
		t.Fatalf("anular: %v", err)
	}
}

func TestUnaConexion_AnularNotaDeVenta(t *testing.T) {
	db := setupCancelDB(t)
	series := database.TenantDocumentSeries{BranchID: 1, DocType: "Nota de Venta", SunatCode: "00", Series: "NV01", Correlative: 2, Active: true}
	db.Create(&series)
	sale := seedSaleWithStockOut(t, db, "open", nil)
	db.Model(sale).Update("series_id", series.ID)
	db.Where("sale_id = ?", sale.ID).Delete(&database.TenantCashMovement{})
	db.Create(&database.TenantSalePayment{SaleID: sale.ID, Method: "cash", Amount: 100})

	if err := dbtest.WithSingleConnection(t, db, func() error { return NewSaleService(db).CancelNotaVenta(sale.ID, 1, "prueba") }); err != nil {
		t.Fatalf("anular nota de venta: %v", err)
	}
}
