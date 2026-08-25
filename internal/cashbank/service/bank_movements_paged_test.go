package service

import (
	"testing"
	"time"

	"tukifac/pkg/database"
)

func TestListBankMovementsPaged_FiltersPaginationAndSummary(t *testing.T) {
	db := setupFinancialReversalTestDB(t)
	svc := NewCashBankService(db)

	acc := &database.TenantBankAccount{Name: "BBVA", PaymentMethod: "transferencia", Balance: 0, Active: true}
	if err := db.Create(acc).Error; err != nil {
		t.Fatal(err)
	}
	// Otra cuenta, para confirmar que el filtro por accountID no mezcla movimientos.
	otherAcc := &database.TenantBankAccount{Name: "BCP", PaymentMethod: "yape", Balance: 0, Active: true}
	if err := db.Create(otherAcc).Error; err != nil {
		t.Fatal(err)
	}

	mk := func(accID uint, typ string, amount float64, day int) {
		if err := db.Create(&database.TenantBankMovement{
			BankAccountID: accID, Type: typ, Amount: amount,
			Date: time.Date(2026, 8, day, 12, 0, 0, 0, time.Local), UserID: 1,
		}).Error; err != nil {
			t.Fatal(err)
		}
	}
	mk(acc.ID, "credit", 100, 1)
	mk(acc.ID, "credit", 50, 5)
	mk(acc.ID, "debit", 30, 10)
	mk(acc.ID, "credit", 20, 15)
	mk(otherAcc.ID, "credit", 999, 1) // no debe aparecer

	// Sin filtros: 4 filas de `acc`, resumen sum_credit=170, sum_debit=30.
	rows, total, summary, err := svc.ListBankMovementsPaged(acc.ID, BankMovementListParams{Page: 1, PerPage: 2})
	if err != nil {
		t.Fatal(err)
	}
	if total != 4 {
		t.Fatalf("total: got %d want 4", total)
	}
	if len(rows) != 2 {
		t.Fatalf("page size: got %d want 2", len(rows))
	}
	if summary.SumCredit != 170 || summary.SumDebit != 30 {
		t.Fatalf("summary: credit=%.2f debit=%.2f want 170/30", summary.SumCredit, summary.SumDebit)
	}

	// Página 2: la fila restante (orden desc por fecha: día 1 queda último).
	rows2, _, _, err := svc.ListBankMovementsPaged(acc.ID, BankMovementListParams{Page: 2, PerPage: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows2) != 2 {
		t.Fatalf("page 2 size: got %d want 2", len(rows2))
	}

	// Filtro por tipo: solo credit → 3 filas, no afecta el resumen (resumen es del período, no de la página).
	creditRows, creditTotal, creditSummary, err := svc.ListBankMovementsPaged(acc.ID, BankMovementListParams{Type: "credit", Page: 1, PerPage: 50})
	if err != nil {
		t.Fatal(err)
	}
	if creditTotal != 3 || len(creditRows) != 3 {
		t.Fatalf("credit filter: total=%d rows=%d want 3/3", creditTotal, len(creditRows))
	}
	if creditSummary.SumDebit != 0 {
		t.Fatalf("credit filter summary.SumDebit: got %.2f want 0", creditSummary.SumDebit)
	}

	// Filtro por fecha: solo desde el día 10 → 2 filas (débito 30 + crédito 20).
	from := time.Date(2026, 8, 10, 0, 0, 0, 0, time.Local)
	dateRows, dateTotal, _, err := svc.ListBankMovementsPaged(acc.ID, BankMovementListParams{DateFrom: &from, Page: 1, PerPage: 50})
	if err != nil {
		t.Fatal(err)
	}
	if dateTotal != 2 || len(dateRows) != 2 {
		t.Fatalf("date filter: total=%d rows=%d want 2/2", dateTotal, len(dateRows))
	}
}
