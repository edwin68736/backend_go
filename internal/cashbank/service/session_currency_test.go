package service

import (
	"testing"
	"time"

	"tukifac/pkg/database"
)

// Cierre/arqueo en soles: el efectivo de una venta en USD entra a la sesión con su equivalente (monto × TC del documento).
func TestSessionClose_UsesSolesEquivalentForUSDDocuments(t *testing.T) {
	db := setupCashReportPhysicalBalanceTestDB(t)
	svc := NewCashBankService(db)
	seedReportPaymentMethods(t, db)

	session := &database.TenantCashSession{BranchID: 1, UserID: 1, OpenedBy: 1, Status: "open", OpeningBalance: 100, OpenedAt: time.Now()}
	if err := db.Create(session).Error; err != nil {
		t.Fatal(err)
	}
	rate := 4.0
	markUSD := func(saleID uint) {
		if err := db.Model(&database.TenantSale{}).Where("id = ?", saleID).Updates(map[string]interface{}{"currency": "USD", "exchange_rate": rate}).Error; err != nil {
			t.Fatal(err)
		}
	}
	createReportSale(t, db, svc, 1, session.ID, map[string]float64{"cash": 27}) // US$ 27 → S/ 108 en efectivo
	markUSD(1)
	createReportSale(t, db, svc, 2, session.ID, map[string]float64{"cash": 50}) // S/ 50 en efectivo
	createReportSale(t, db, svc, 3, session.ID, map[string]float64{"yape": 10}) // US$ 10 → S/ 40 por Yape
	markUSD(3)

	// Esperado del arqueo: 100 + 108 + 50 = 258 (antes: 100 + 27 + 50 = 177, mezclando dólares como soles).
	if got := svc.getExpectedBalance(session.ID); got != 258 {
		t.Fatalf("esperado = %v, quiere 258", got)
	}
	sum, err := svc.GetSessionBalanceSummary(session.ID)
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]MethodBalance{}
	for _, m := range sum.ByMethod {
		by[m.Method] = m
	}
	if by["efectivo"].Income != 158 || by["efectivo"].Net != 258 || by["yape"].Income != 40 || sum.Total != 298 || sum.CashExpected != 258 {
		t.Errorf("resumen en soles: efectivo=%+v yape=%+v total=%v esperado=%v", by["efectivo"], by["yape"], sum.Total, sum.CashExpected)
	}
	if !sum.HasForeign || sum.RateMissingCount != 0 {
		t.Errorf("debe avisar de documentos en dólares: %+v", sum)
	}
	rep, err := svc.GetSessionReport(session.ID)
	if err != nil {
		t.Fatal(err)
	}
	c := rep.Currency
	if !c.HasForeign || c.PhysicalBalancePEN != 258 || c.TotalIncomePEN != 198 || c.CashIncomePEN != 158 {
		t.Errorf("bloque de moneda: %+v", c)
	}
	by2 := map[string]MovementCurrencyTotal{}
	for _, x := range c.ByCurrency {
		by2[x.Currency] = x
	}
	if by2["USD"].SumIncome != 37 || by2["USD"].SumIncomePEN != 148 || by2["PEN"].SumIncome != 50 {
		t.Errorf("por moneda: %+v", c.ByCurrency)
	}

	// Cierre: lo contado en soles se compara con el esperado en soles.
	if err := svc.CloseSession(session.ID, 1, 258, "", nil, true); err != nil {
		t.Fatal(err)
	}
	var closed database.TenantCashSession
	db.First(&closed, session.ID)
	if closed.ExpectedBalance == nil || *closed.ExpectedBalance != 258 || closed.Difference == nil || *closed.Difference != 0 {
		t.Errorf("cierre: esperado=%v diferencia=%v", closed.ExpectedBalance, closed.Difference)
	}
}

// Una sesión solo en soles queda idéntica y sin el bloque de moneda extranjera.
func TestSessionClose_PENOnlySessionUnchanged(t *testing.T) {
	db := setupCashReportPhysicalBalanceTestDB(t)
	svc := NewCashBankService(db)
	seedReportPaymentMethods(t, db)
	session := &database.TenantCashSession{BranchID: 1, UserID: 1, OpenedBy: 1, Status: "open", OpeningBalance: 50, OpenedAt: time.Now()}
	db.Create(session)
	createReportSale(t, db, svc, 1, session.ID, map[string]float64{"cash": 200})
	if got := svc.getExpectedBalance(session.ID); got != 250 {
		t.Errorf("esperado = %v, quiere 250", got)
	}
	rep, _ := svc.GetSessionReport(session.ID)
	if rep.Currency.HasForeign {
		t.Errorf("sin dólares no debe haber bloque de moneda extranjera: %+v", rep.Currency)
	}
	if sum, _ := svc.GetSessionBalanceSummary(session.ID); sum.HasForeign {
		t.Error("el resumen no debe marcar moneda extranjera")
	}
}
