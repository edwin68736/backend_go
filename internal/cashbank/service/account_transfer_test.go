package service

import (
	"strings"
	"testing"
	"time"

	"tukifac/pkg/dailyfx"
	"tukifac/pkg/database"
)

func setBalance(t *testing.T, svc *CashBankService, id uint, bal float64, cur string) {
	t.Helper()
	if err := svc.db.Model(&database.TenantBankAccount{}).Where("id = ?", id).Updates(map[string]interface{}{"balance": bal, "currency": cur}).Error; err != nil {
		t.Fatal(err)
	}
}

func TestTransfer_BankToBank_MovesBothBalancesAndIsVoidable(t *testing.T) {
	svc, _, ids := destSetup(t)
	y, b := ids["Yape"], ids["BCP"]
	setBalance(t, svc, y, 500, "PEN")
	tr, err := svc.CreateTransfer(TransferInput{From: TransferEndpoint{Kind: "bank", AccountID: y}, To: TransferEndpoint{Kind: "bank", AccountID: b}, Amount: 200, UserID: 1, BranchID: 1})
	if err != nil {
		t.Fatal(err)
	}
	if balanceOf(t, svc, y) != 300 || balanceOf(t, svc, b) != 200 {
		t.Fatalf("saldos tras transferir: yape=%v bcp=%v", balanceOf(t, svc, y), balanceOf(t, svc, b))
	}
	var n int64
	svc.db.Model(&database.TenantBankMovement{}).Where("transfer_id = ?", tr.ID).Count(&n)
	if n != 2 {
		t.Fatalf("la transferencia debe dejar 2 movimientos enlazados, hay %d", n)
	}
	if err := svc.VoidTransfer(tr.ID, 1, 1, "error de digitación"); err != nil {
		t.Fatal(err)
	}
	if balanceOf(t, svc, y) != 500 || balanceOf(t, svc, b) != 0 {
		t.Fatalf("la anulación debe devolver los saldos: yape=%v bcp=%v", balanceOf(t, svc, y), balanceOf(t, svc, b))
	}
	if err := svc.VoidTransfer(tr.ID, 1, 1, "otra vez"); err == nil {
		t.Error("anular dos veces debe rechazarse")
	}
}

func TestTransfer_Validations(t *testing.T) {
	svc, _, ids := destSetup(t)
	y, b := ids["Yape"], ids["BCP"]
	setBalance(t, svc, y, 100, "PEN")
	bank := func(id uint) TransferEndpoint { return TransferEndpoint{Kind: "bank", AccountID: id} }
	if _, err := svc.CreateTransfer(TransferInput{From: bank(y), To: bank(y), Amount: 10, UserID: 1, BranchID: 1}); err == nil {
		t.Error("origen = destino debe rechazarse")
	}
	if _, err := svc.CreateTransfer(TransferInput{From: bank(y), To: bank(b), Amount: 500, UserID: 1, BranchID: 1}); err == nil || !strings.Contains(err.Error(), "saldo suficiente") {
		t.Errorf("sin saldo debe rechazarse, err=%v", err)
	}
	if _, err := svc.CreateTransfer(TransferInput{From: bank(y), To: bank(b), Amount: 0, UserID: 1, BranchID: 1}); err == nil {
		t.Error("monto cero debe rechazarse")
	}
	svc.db.Model(&database.TenantBankAccount{}).Where("id = ?", b).Update("active", false)
	if _, err := svc.CreateTransfer(TransferInput{From: bank(y), To: bank(b), Amount: 10, UserID: 1, BranchID: 1}); err == nil || !strings.Contains(err.Error(), "inactiva") {
		t.Errorf("destino inactivo debe rechazarse, err=%v", err)
	}
	if balanceOf(t, svc, y) != 100 {
		t.Errorf("un rechazo no debe mover saldos")
	}
}

func TestTransfer_BetweenCurrencies_UsesExchangeRate(t *testing.T) {
	svc, _, ids := destSetup(t)
	pen, usd := ids["Yape"], ids["BCP"]
	setBalance(t, svc, pen, 1000, "PEN")
	setBalance(t, svc, usd, 0, "USD")
	rate := 3.5
	// Sin TC y sin proveedor de TC: se exige
	if _, err := svc.CreateTransfer(TransferInput{From: TransferEndpoint{"bank", pen}, To: TransferEndpoint{"bank", usd}, Amount: 350, UserID: 1, BranchID: 1}); err == nil || !strings.Contains(err.Error(), "tipo de cambio") {
		t.Fatalf("monedas distintas sin TC deben exigirlo, err=%v", err)
	}
	// PEN → USD: S/ 350 ÷ 3.5 = US$ 100
	tr, err := svc.CreateTransfer(TransferInput{From: TransferEndpoint{"bank", pen}, To: TransferEndpoint{"bank", usd}, Amount: 350, ExchangeRate: &rate, UserID: 1, BranchID: 1})
	if err != nil {
		t.Fatal(err)
	}
	if balanceOf(t, svc, pen) != 650 || balanceOf(t, svc, usd) != 100 || tr.AmountTo != 100 || tr.CurrencyTo != "USD" {
		t.Fatalf("PEN→USD: pen=%v usd=%v tr=%+v", balanceOf(t, svc, pen), balanceOf(t, svc, usd), tr)
	}
	// USD → PEN: US$ 40 × 3.5 = S/ 140
	if _, err := svc.CreateTransfer(TransferInput{From: TransferEndpoint{"bank", usd}, To: TransferEndpoint{"bank", pen}, Amount: 40, ExchangeRate: &rate, UserID: 1, BranchID: 1}); err != nil {
		t.Fatal(err)
	}
	if balanceOf(t, svc, pen) != 790 || balanceOf(t, svc, usd) != 60 {
		t.Fatalf("USD→PEN: pen=%v usd=%v", balanceOf(t, svc, pen), balanceOf(t, svc, usd))
	}
	// El movimiento de la cuenta en USD guarda su equivalente en soles
	var mv database.TenantBankMovement
	svc.db.Where("bank_account_id = ? AND type = ?", usd, "credit").First(&mv)
	if mv.Currency != "USD" || mv.AmountPEN == nil || *mv.AmountPEN != 350 || mv.AccountAmount == nil || *mv.AccountAmount != 100 {
		t.Errorf("snapshot del movimiento en la cuenta USD: %+v", mv)
	}
}

func TestTransfer_CashToBank_AffectsSessionAndNeedsFunds(t *testing.T) {
	svc, ses, ids := destSetup(t)
	y := ids["Yape"]
	svc.db.Model(ses).Update("opening_balance", 300)
	bank := TransferEndpoint{Kind: "bank", AccountID: y}
	cash := TransferEndpoint{Kind: "cash"}
	if _, err := svc.CreateTransfer(TransferInput{From: cash, To: bank, Amount: 500, UserID: 1, BranchID: 1}); err == nil || !strings.Contains(err.Error(), "efectivo suficiente") {
		t.Fatalf("sin efectivo suficiente debe rechazarse, err=%v", err)
	}
	if _, err := svc.CreateTransfer(TransferInput{From: cash, To: bank, Amount: 200, UserID: 1, BranchID: 1}); err != nil {
		t.Fatal(err)
	}
	if balanceOf(t, svc, y) != 200 || svc.getExpectedBalance(ses.ID) != 100 {
		t.Fatalf("depósito de caja a banco: banco=%v caja esperada=%v", balanceOf(t, svc, y), svc.getExpectedBalance(ses.ID))
	}
	// Sin sesión abierta no se puede
	svc.db.Model(&database.TenantCashSession{}).Where("id = ?", ses.ID).Update("status", "closed")
	if _, err := svc.CreateTransfer(TransferInput{From: cash, To: bank, Amount: 10, UserID: 1, BranchID: 1}); err == nil {
		t.Error("sin sesión de caja abierta debe rechazarse")
	}
}

// Un cobro en soles hacia una cuenta en dólares se convierte con el TC oficial del día; el saldo de la cuenta va en dólares.
func TestAccountInUSD_ReceivesPENPaymentConverted(t *testing.T) {
	old := dailyfx.Lookup
	defer func() { dailyfx.Lookup = old }()
	dailyfx.Lookup = func(day string) (float64, bool, bool) { return 3.5, false, true }
	svc, ses, ids := destSetup(t)
	usd := ids["BCP"]
	setBalance(t, svc, usd, 0, "USD")
	sid := ses.ID
	bcp := usd
	// Venta en soles (documento en PEN) cobrada a la cuenta en dólares: S/ 350 → US$ 100
	sale := database.TenantSale{BranchID: 1, Currency: "PEN", Total: 350, IssueDate: time.Now(), Number: "NV-USD1"}
	if err := svc.db.Create(&sale).Error; err != nil {
		t.Skipf("esquema de ventas reducido en pruebas: %v", err)
	}
	if err := svc.RecordPaymentToDestination(nil, "yape", 350, &sid, "NV-USD1", "Venta", &sale.ID, 1, &bcp); err != nil {
		t.Fatal(err)
	}
	if balanceOf(t, svc, usd) != 100 {
		t.Fatalf("saldo cuenta USD = %v, quiere 100 (S/350 ÷ 3.5)", balanceOf(t, svc, usd))
	}
}
