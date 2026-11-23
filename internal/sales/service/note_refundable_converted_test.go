package service

import (
	"testing"
	"time"

	"tukifac/pkg/database"

	"gorm.io/gorm"
)

// Regresión P0-0: sobre una venta convertida (NV → comprobante) los pagos que la conversión copia al comprobante son informativos
// (cash_session_id NULL): el dinero real entró en la NV. NoteRefundableAmount contaba copia + original, o sea el cobrado dos veces.
// Estos tests usan el flujo real (IssueElectronicFromNota), que es el que crea las copias; el test anterior sembraba la hija a mano.

// convertedNC convierte la NV sembrada y emite una nota de crédito aceptada de `total` sobre el comprobante (la hija).
func convertedNC(t *testing.T, db *gorm.DB, notaID, seriesID uint, total float64) (child database.TenantSale, nc database.TenantSale) {
	t.Helper()
	if err := db.AutoMigrate(&database.TenantSaleDetraccion{}); err != nil {
		t.Fatal(err)
	}
	c, err := NewSaleService(db).IssueElectronicFromNota(notaID, seriesID, 1, "", 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	oid := c.ID
	n := database.TenantSale{DocType: "NOTA_CREDITO", Number: "BC01-1", Total: total, Subtotal: total, Status: "paid", BillingStatus: "accepted",
		NoteReasonCode: "09", OriginalSaleID: &oid, BranchID: 1, IssueDate: time.Now(), PaymentMethod: "efectivo"}
	if err := db.Create(&n).Error; err != nil {
		t.Fatal(err)
	}
	return *c, n
}

func TestNoteRefundable_ConvertidaCobradaEnParteNoCuentaLaCopiaDosVeces(t *testing.T) {
	db := setupIssueElectronicFromNotaTestDB(t)
	// NV de 118 a crédito con un abono real de 40.
	notaID, seriesID := seedNotaCredito(t, db, []database.TenantSaleCreditInstallment{
		{InstallmentNo: 1, DueDate: time.Now().AddDate(0, 0, 10), Amount: 118, PaidAmount: 40, Currency: "PEN", Status: "partial"},
	}, []database.TenantSalePayment{{Method: "efectivo", Amount: 40}})
	child, nc := convertedNC(t, db, notaID, seriesID, 60)

	// Precondición: la conversión sí copió el abono (el origen del defecto).
	var copies int64
	db.Model(&database.TenantSalePayment{}).Where("sale_id = ?", child.ID).Count(&copies)
	if copies != 1 {
		t.Fatalf("la conversión debía copiar 1 pago a la hija, copió %d", copies)
	}
	// Cobrado real 40; tras la nota de 60 se debe 58: no hay exceso que devolver. (Antes: 80 cobrado → devolvía 22.)
	if got := refundOf(t, db, nc); got != 0 {
		t.Fatalf("cobrado real 40, debe 58 tras la nota: devolución %.2f, quería 0", got)
	}
}

func TestNoteRefundable_ConvertidaCobradaCompletaDevuelveLaNota(t *testing.T) {
	db := setupIssueElectronicFromNotaTestDB(t)
	notaID, seriesID := seedNotaCredito(t, db, []database.TenantSaleCreditInstallment{
		{InstallmentNo: 1, DueDate: time.Now().AddDate(0, 0, 10), Amount: 118, PaidAmount: 118, Currency: "PEN", Status: "paid"},
	}, []database.TenantSalePayment{{Method: "efectivo", Amount: 118}})
	_, nc := convertedNC(t, db, notaID, seriesID, 60)
	if got := refundOf(t, db, nc); !close2(got, 60) {
		t.Fatalf("cobrada completa: devolución %.2f, quería 60", got)
	}
}

func TestNoteRefundable_ConvertidaCuentaCobroPosteriorSobreLaHija(t *testing.T) {
	db := setupIssueElectronicFromNotaTestDB(t)
	notaID, seriesID := seedNotaCredito(t, db, []database.TenantSaleCreditInstallment{
		{InstallmentNo: 1, DueDate: time.Now().AddDate(0, 0, 10), Amount: 118, PaidAmount: 40, Currency: "PEN", Status: "partial"},
	}, []database.TenantSalePayment{{Method: "efectivo", Amount: 40}})
	child, nc := convertedNC(t, db, notaID, seriesID, 60)
	// Cobro posterior real sobre el comprobante: lleva sesión de caja (a diferencia de la copia).
	sess := uint(7)
	if err := db.Create(&database.TenantSalePayment{SaleID: child.ID, Method: "efectivo", Amount: 30, CashSessionID: &sess}).Error; err != nil {
		t.Fatal(err)
	}
	// Real cobrado 40 + 30 = 70; tras la nota se debe 58 → exceso 12.
	if got := refundOf(t, db, nc); !close2(got, 12) {
		t.Fatalf("cobrado real 70, debe 58: devolución %.2f, quería 12", got)
	}
}
