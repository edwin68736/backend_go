package service

import (
	"testing"
	"time"

	"tukifac/pkg/database"

	"gorm.io/gorm"
)

// seedSessionWithActivity crea una caja con efectivo, un movimiento no efectivo, uno bancario y (opcional) una venta.
func seedSessionWithActivity(t *testing.T, db *gorm.DB, accountID uint, opening float64, withSale bool) *database.TenantCashSession {
	t.Helper()
	st := newSession(t, db, opening)
	db.Create(&database.TenantCashMovement{CashSessionID: st.ID, Type: "income", Amount: 20.10, PaymentMethod: "efectivo"})
	db.Create(&database.TenantCashMovement{CashSessionID: st.ID, Type: "expense", Amount: 5.05, PaymentMethod: "cash"})
	db.Create(&database.TenantCashMovement{CashSessionID: st.ID, Type: "expense", Amount: 3, PaymentMethod: "transferencia"})
	db.Create(&database.TenantBankMovement{BankAccountID: accountID, Type: "credit", Amount: 50.20, CashSessionID: &st.ID, Date: time.Now(), UserID: 1})
	if withSale {
		sid := st.ID
		db.Create(&database.TenantSale{BranchID: 1, UserID: 7, CashSessionID: &sid, Number: "NV001-" + time.Now().Format("150405.000000000")})
	}
	return st
}

func seedWalletAccount(t *testing.T, db *gorm.DB) uint {
	t.Helper()
	acc := &database.TenantBankAccount{Name: "Yape", Type: "wallet", PaymentMethod: "yape", Active: true}
	if err := db.Create(acc).Error; err != nil {
		t.Fatal(err)
	}
	return acc.ID
}

// El historial por lotes debe dar exactamente los mismos números que el cálculo sesión por sesión (detalle y arqueo).
func TestListSessionsEnriched_IgualQuePorSesion(t *testing.T) {
	db := setupSessionAdminDB(t)
	svc := NewCashBankService(db)
	closer := &database.TenantUser{Name: "Quien cierra", Email: "cierra@test.local"}
	opener := &database.TenantUser{ID: 7, Name: "Quien abre", Email: "abre@test.local"}
	db.Create(opener)
	db.Create(closer)
	acc := seedWalletAccount(t, db)

	a := seedSessionWithActivity(t, db, acc, 100, true)
	db.Model(a).Update("closed_by", closer.ID)
	seedSessionWithActivity(t, db, acc, 0.30, false)
	newSession(t, db, 15) // vacía
	soloVenta := newSession(t, db, 0)
	sid := soloVenta.ID
	db.Create(&database.TenantSale{BranchID: 1, UserID: 7, CashSessionID: &sid, Number: "NV001-SOLO"})

	items, _, err := svc.ListSessionsEnriched(SessionListParams{BranchID: 1})
	if err != nil {
		t.Fatalf("ListSessionsEnriched: %v", err)
	}
	if len(items) != 4 {
		t.Fatalf("filas = %d, se esperaban 4", len(items))
	}
	for _, it := range items {
		income, expense := svc.sessionMovementTotals(it.ID)
		summary, err := svc.GetSessionBalanceSummary(it.ID)
		if err != nil {
			t.Fatal(err)
		}
		usage, err := svc.SessionUsageOf(it.ID)
		if err != nil {
			t.Fatal(err)
		}
		if it.TotalIncome != income || it.TotalExpense != expense || it.Total != summary.Total || it.Empty != usage.Empty() {
			t.Errorf("sesión %d: lote (%v, %v, %v, %v) != por sesión (%v, %v, %v, %v)", it.ID,
				it.TotalIncome, it.TotalExpense, it.Total, it.Empty, income, expense, summary.Total, usage.Empty())
		}
		if it.OpenedByName != "Quien abre" {
			t.Errorf("sesión %d: opened_by_name = %q", it.ID, it.OpenedByName)
		}
		wantCloser := ""
		if it.ID == a.ID {
			wantCloser = "Quien cierra"
		}
		if it.ClosedByName != wantCloser {
			t.Errorf("sesión %d: closed_by_name = %q, want %q", it.ID, it.ClosedByName, wantCloser)
		}
	}
}

// Las consultas no deben crecer con la cantidad de sesiones listadas.
func TestListSessionsEnriched_ConsultasNoCrecenPorSesion(t *testing.T) {
	db := setupSessionAdminDB(t)
	svc := NewCashBankService(db)
	acc := seedWalletAccount(t, db)

	queries := 0
	count := func(*gorm.DB) { queries++ }
	if err := db.Callback().Query().After("gorm:query").Register("test:count_query", count); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Row().After("gorm:row").Register("test:count_row", count); err != nil {
		t.Fatal(err)
	}
	measure := func() int {
		queries = 0
		if _, _, err := svc.ListSessionsEnriched(SessionListParams{BranchID: 1}); err != nil {
			t.Fatal(err)
		}
		return queries
	}

	seedSessionWithActivity(t, db, acc, 10, true)
	one := measure()
	for i := 0; i < 20; i++ {
		seedSessionWithActivity(t, db, acc, 10, true)
	}
	many := measure()
	t.Logf("consultas: 1 sesión = %d, 21 sesiones = %d", one, many)
	if one == 0 || many != one {
		t.Fatalf("consultas con 1 sesión = %d, con 21 = %d: debían ser iguales", one, many)
	}
}
