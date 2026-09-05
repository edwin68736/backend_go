package service

import (
	"testing"
	"time"

	"tukifac/pkg/database"
)

// Corrección P0: una venta a crédito sin cobrar NO debe aparecer como ingreso — antes,
// GetSessionReport la clasificaba como "electrónica" (por el fallback que usa sale.PaymentMethod
// cuando no hay TenantSalePayment, y "credito" no se traduce a "efectivo" en
// normalizeReportMethod, así que caía directo en Electronic.TotalSales). Ahora se excluye
// explícitamente y se contabiliza aparte en CreditGenerated, igual que ya se hace con la
// detracción SPOT.
func TestGetSessionReport_ventaACredito_noApareceComoElectronico(t *testing.T) {
	db := setupCashReportPhysicalBalanceTestDB(t)
	svc := NewCashBankService(db)
	seedReportPaymentMethods(t, db)

	session := &database.TenantCashSession{BranchID: 1, UserID: 1, OpenedBy: 1, Status: "open", OpenedAt: time.Now()}
	if err := db.Create(session).Error; err != nil {
		t.Fatal(err)
	}
	// Venta al contado en efectivo, normal.
	createReportSale(t, db, svc, 1, session.ID, map[string]float64{"cash": 200})

	// Venta a crédito sin adelanto: sin ningún TenantSalePayment, PaymentMethod="credito".
	creditSale := &database.TenantSale{
		ID: 2, BranchID: 1, UserID: 1, CashSessionID: &session.ID,
		SeriesID: 1, DocType: "boleta", Series: "B001", Correlative: 2,
		Number: "B001-2", IssueDate: time.Now(),
		Total: 3000, PaymentMethod: "credito", Status: "credit", CreatedAt: time.Now(),
	}
	if err := db.Create(creditSale).Error; err != nil {
		t.Fatal(err)
	}

	report, err := svc.GetSessionReport(session.ID)
	if err != nil {
		t.Fatal(err)
	}

	if report.CreditGenerated.Total != 3000 {
		t.Errorf("CreditGenerated.Total = %v, want 3000", report.CreditGenerated.Total)
	}
	if len(report.CreditGenerated.Sales) != 1 || report.CreditGenerated.Sales[0].Amount != 3000 {
		t.Errorf("CreditGenerated.Sales inesperado: %+v", report.CreditGenerated.Sales)
	}

	// La venta a crédito NO debe inflar ninguno de estos totales — deben reflejar solo la venta
	// en efectivo (200).
	if report.Totals.TotalSales != 200 {
		t.Errorf("Totals.TotalSales = %v, want 200 (la venta a crédito no debe sumarse)", report.Totals.TotalSales)
	}
	if report.Totals.TotalSalesDirect != 200 {
		t.Errorf("Totals.TotalSalesDirect = %v, want 200", report.Totals.TotalSalesDirect)
	}
	if report.Totals.TotalSalesCommercial != 200 {
		t.Errorf("Totals.TotalSalesCommercial = %v, want 200", report.Totals.TotalSalesCommercial)
	}
	if report.Electronic.TotalSales != 0 {
		t.Errorf("Electronic.TotalSales = %v, want 0 (la venta a crédito NO es electrónica)", report.Electronic.TotalSales)
	}
	// Ninguna fila de IncomeDetail debe corresponder a la venta a crédito.
	for _, row := range report.IncomeDetail {
		if row.Amount == 3000 {
			t.Errorf("la venta a crédito no debe aparecer en IncomeDetail: %+v", row)
		}
	}
	// El efectivo físico solo refleja la venta en efectivo — sin cambios.
	if report.CashPhysical.PhysicalBalance != 200 {
		t.Errorf("PhysicalBalance = %v, want 200", report.CashPhysical.PhysicalBalance)
	}
}
