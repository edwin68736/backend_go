package service

import (
	"testing"
	"time"

	"tukifac/pkg/database"
)

// Caso real (tenant guillenbegazo, F001-00000097): NV001-00003492 vendida en efectivo (S/ 25) en la caja 212; al día siguiente se emite la
// factura desde ella. La factura hereda la caja de la NV y copia el pago SIN caja. El efectivo debe salir UNA vez, rotulado con el número del
// comprobante y la nota de venta de la que viene — tanto en el reporte por caja como en el de fechas.
func TestListMovementsReport_ComprobanteDesdeNV_UnaSolaVezYRotulado(t *testing.T) {
	db := setupMovementsReportPurchaseTestDB(t)
	svc := NewCashBankService(db)
	now := time.Now()

	session := &database.TenantCashSession{BranchID: 1, UserID: 3, OpenedBy: 3, Status: "closed", OpenedAt: now.Add(-3 * time.Hour)}
	db.Create(session)
	nv := &database.TenantSale{ID: 10, BranchID: 1, UserID: 3, CashSessionID: &session.ID, SeriesID: 1, DocType: "NOTA DE VENTA", Series: "NV001",
		Correlative: 3492, Number: "NV001-00003492", IssueDate: now, Total: 25, PaymentMethod: "cash", Status: "paid", CreatedAt: now.Add(-2 * time.Hour)}
	db.Create(nv)
	db.Create(&database.TenantSalePayment{SaleID: nv.ID, Method: "cash", Amount: 25, CashSessionID: &session.ID, CreatedAt: now.Add(-2 * time.Hour)})

	nvID := nv.ID
	fac := &database.TenantSale{ID: 11, BranchID: 1, UserID: 1, CashSessionID: &session.ID, SeriesID: 2, DocType: "FACTURA", Series: "F001",
		Correlative: 97, Number: "F001-00000097", IssueDate: now, Total: 25, PaymentMethod: "cash", Status: "paid", IssuedFromNotaSaleID: &nvID, CreatedAt: now.Add(-time.Hour)}
	db.Create(fac)
	// Copia informativa del pago: sin caja.
	db.Create(&database.TenantSalePayment{SaleID: fac.ID, Method: "cash", Amount: 25, CreatedAt: now.Add(-time.Hour)})

	for name, f := range map[string]MovementReportFilters{
		"por caja":  {SessionID: session.ID},
		"por fecha": {},
	} {
		split, err := svc.ListMovementsReport(f)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		var rows []MovementReportRow
		for _, r := range split.Cash.Data {
			if r.Type == "venta" {
				rows = append(rows, r)
			}
		}
		if len(rows) != 1 {
			t.Fatalf("%s: %d filas de venta, quería 1 (el efectivo no debe contarse dos veces): %+v", name, len(rows), rows)
		}
		if rows[0].Amount != 25 {
			t.Errorf("%s: monto %.2f, quería 25", name, rows[0].Amount)
		}
		want := "F001-00000097 (desde NV001-00003492)"
		if rows[0].DocNumber != want {
			t.Errorf("%s: documento %q, quería %q", name, rows[0].DocNumber, want)
		}
		if split.Cash.Summary.SumIncome != 25 {
			t.Errorf("%s: ingresos de efectivo %.2f, quería 25", name, split.Cash.Summary.SumIncome)
		}
	}
}

// NV histórica sin líneas de pago: cuenta por la venta de la NV (camino legacy) y la copia del comprobante no la duplica.
func TestListMovementsReport_ComprobanteDesdeNV_SinPagoEnLaNV_NoSeDuplica(t *testing.T) {
	db := setupMovementsReportPurchaseTestDB(t)
	svc := NewCashBankService(db)
	now := time.Now()
	session := &database.TenantCashSession{BranchID: 1, UserID: 3, OpenedBy: 3, Status: "closed", OpenedAt: now.Add(-3 * time.Hour)}
	db.Create(session)
	nv := &database.TenantSale{ID: 20, BranchID: 1, UserID: 3, CashSessionID: &session.ID, SeriesID: 1, DocType: "NOTA DE VENTA", Series: "NV001",
		Correlative: 1, Number: "NV001-00000001", IssueDate: now, Total: 40, Status: "paid", CreatedAt: now.Add(-2 * time.Hour)}
	db.Create(nv)
	nvID := nv.ID
	fac := &database.TenantSale{ID: 21, BranchID: 1, UserID: 1, CashSessionID: &session.ID, SeriesID: 2, DocType: "FACTURA", Series: "F001",
		Correlative: 1, Number: "F001-00000001", IssueDate: now, Total: 40, PaymentMethod: "cash", Status: "paid", IssuedFromNotaSaleID: &nvID, CreatedAt: now.Add(-time.Hour)}
	db.Create(fac)
	db.Create(&database.TenantSalePayment{SaleID: fac.ID, Method: "cash", Amount: 40, CreatedAt: now.Add(-time.Hour)})

	split, err := svc.ListMovementsReport(MovementReportFilters{})
	if err != nil {
		t.Fatal(err)
	}
	if split.Cash.Summary.SumIncome != 40 {
		t.Fatalf("ingresos %.2f, quería 40 (la NV cuenta por su venta; la copia del comprobante no suma)", split.Cash.Summary.SumIncome)
	}
}

// Un cobro posterior REAL del comprobante (venta a crédito cobrada en una caja) tiene caja y no se descarta.
func TestListMovementsReport_ComprobanteDesdeNV_CobroPosteriorConCajaSiCuenta(t *testing.T) {
	db := setupMovementsReportPurchaseTestDB(t)
	svc := NewCashBankService(db)
	now := time.Now()
	s1 := &database.TenantCashSession{BranchID: 1, UserID: 3, OpenedBy: 3, Status: "closed", OpenedAt: now.Add(-48 * time.Hour)}
	s2 := &database.TenantCashSession{BranchID: 1, UserID: 3, OpenedBy: 3, Status: "open", OpenedAt: now.Add(-2 * time.Hour)}
	db.Create(s1)
	db.Create(s2)
	nv := &database.TenantSale{ID: 30, BranchID: 1, UserID: 3, CashSessionID: &s1.ID, SeriesID: 1, DocType: "NOTA DE VENTA", Series: "NV001", Correlative: 5,
		Number: "NV001-00000005", IssueDate: now, Total: 100, PaymentConditionCode: "credit", Status: "credit", CreatedAt: now.Add(-48 * time.Hour)}
	db.Create(nv)
	db.Create(&database.TenantSalePayment{SaleID: nv.ID, Method: "credito", Amount: 100, CashSessionID: &s1.ID, CreatedAt: now.Add(-48 * time.Hour)})
	nvID := nv.ID
	fac := &database.TenantSale{ID: 31, BranchID: 1, UserID: 1, CashSessionID: &s1.ID, SeriesID: 2, DocType: "FACTURA", Series: "F001", Correlative: 5,
		Number: "F001-00000005", IssueDate: now, Total: 100, Status: "credit", PaymentConditionCode: "credit", IssuedFromNotaSaleID: &nvID, CreatedAt: now.Add(-24 * time.Hour)}
	db.Create(fac)
	// Cobro posterior real de 100 en efectivo, en la caja abierta s2.
	db.Create(&database.TenantSalePayment{SaleID: fac.ID, Method: "cash", Amount: 100, CashSessionID: &s2.ID, CreatedAt: now.Add(-time.Hour)})

	split, err := svc.ListMovementsReport(MovementReportFilters{})
	if err != nil {
		t.Fatal(err)
	}
	if split.Cash.Summary.SumIncome != 100 {
		t.Fatalf("ingresos %.2f, quería 100 (el cobro posterior real se mantiene)", split.Cash.Summary.SumIncome)
	}
}
