package service

import (
	"testing"

	salessvc "tukifac/internal/sales/service"
	"tukifac/pkg/dbtest"
)

// Cobrar una cuenta pendiente debe usar una sola conexión dentro de su transacción (ver pkg/dbtest).
func TestUnaConexion_Cobro(t *testing.T) {
	db := setupReceivableCollectionSessionTestDB(t)
	sale := seedCreditSaleForCollection(t, db, 1, 25, 1000)
	openSessionFor(t, db, 30, 1, 2)
	session := uint(30)

	if err := dbtest.WithSingleConnection(t, db, func() error {
		return NewReceivableService(db).Collect(sale.ID, CollectPaymentInput{
			Payments:      []salessvc.PaymentInput{{Method: "cash", Amount: 100}, {Method: "yape", Amount: 50}},
			CashSessionID: &session,
			UserID:        2,
		})
	}); err != nil {
		t.Fatalf("cobro: %v", err)
	}
}
