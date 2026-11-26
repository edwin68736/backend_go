package service

import (
	"testing"
	"time"

	"tukifac/pkg/dbtest"
	"tukifac/pkg/tax"
)

// Registrar una compra al contado debe usar una sola conexión dentro de su transacción (ver pkg/dbtest).
func TestUnaConexion_Compra(t *testing.T) {
	db := setupPurchaseServiceTestDB(t)
	seedCashPaymentMethod(t, db)
	newOpenCashSession(t, db, 1, 1)
	pid := newPurchaseProduct(t, db, "P-UNA-CONEXION")

	if err := dbtest.WithSingleConnection(t, db, func() error {
		_, err := NewPurchaseService(db).Create(CreatePurchaseInput{
			BranchID: 1, UserID: 1, DocType: "FACTURA", Series: "F001", Number: "1",
			IssueDate: time.Now(), PaymentMethod: "cash",
			Items:     []PurchaseItemInput{{ProductID: &pid, Description: "item", Quantity: 1, UnitCost: 100, IgvAffectationType: "10"}},
			TaxConfig: tax.Config{TaxRate: 18},
		})
		return err
	}); err != nil {
		t.Fatalf("compra: %v", err)
	}
}
