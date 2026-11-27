package service

import (
	"testing"

	"tukifac/pkg/dbtest"
)

// Pagar a un proveedor debe usar una sola conexión dentro de su transacción (ver pkg/dbtest).
func TestUnaConexion_PagoProveedor(t *testing.T) {
	for _, method := range []string{"cash", "yape"} {
		t.Run(method, func(t *testing.T) {
			db := setupPayableServiceTestDB(t)
			openPayableSession(t, db, 20, 1, 1)
			purchase := seedCreditPurchase(t, db, 1, 20, 500)

			if err := dbtest.WithSingleConnection(t, db, func() error {
				return NewPayableService(db).Pay(purchase.ID, PayInput{Amount: 100, Method: method, UserID: 1})
			}); err != nil {
				t.Fatalf("pago: %v", err)
			}
		})
	}
}
