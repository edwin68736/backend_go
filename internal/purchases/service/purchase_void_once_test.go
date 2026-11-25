package service

import (
	"strings"
	"testing"
	"time"

	"tukifac/pkg/database"
	"tukifac/pkg/tax"
)

// Una compra se anula UNA sola vez: antes, dos anulaciones que leían «vigente» a la vez revertían dos veces el stock y la caja.
// Aquí la segunda se cuela justo entre la lectura y la transacción de la primera (la simultaneidad real se prueba en MariaDB, ver
// internal/restaurant/supply_purchases_test.go).
func TestPurchaseVoid_dosALaVezRevierteUnaSolaVez(t *testing.T) {
	db := setupPurchaseServiceTestDB(t)
	svc := NewPurchaseService(db)
	newOpenCashSession(t, db, 1, 1)
	// Sin el método «efectivo» registrado la compra no mueve caja y la prueba no vería una doble devolución.
	if err := db.Where(database.TenantPaymentMethod{Code: "cash"}).
		Attrs(database.TenantPaymentMethod{Name: "Efectivo", Active: true, DestinationType: "cash"}).
		FirstOrCreate(&database.TenantPaymentMethod{}).Error; err != nil {
		t.Fatal(err)
	}
	product := database.TenantProduct{Code: "V1", Name: "Harina", Type: "product", Unit: "NIU", ManageStock: true, IgvAffectationType: "10", Active: true}
	if err := db.Create(&product).Error; err != nil {
		t.Fatal(err)
	}
	var purchases []*database.TenantPurchase
	for _, number := range []string{"1", "2"} { // la segunda deja stock de sobra: sin protección habría de dónde restar dos veces
		pid := product.ID
		p, err := svc.Create(CreatePurchaseInput{BranchID: 1, UserID: 1, DocType: "FACTURA", Series: "F001", Number: number,
			IssueDate: time.Now(), PaymentMethod: "cash", TaxConfig: tax.Config{TaxRate: 18},
			Items: []PurchaseItemInput{{ProductID: &pid, Description: "Harina", Quantity: 10, UnitCost: 5, IgvAffectationType: "10"}}})
		if err != nil {
			t.Fatal(err)
		}
		purchases = append(purchases, p)
	}
	target := purchases[0]

	entered := false
	voidBeforeTxHook = func() {
		if entered {
			return
		}
		entered = true
		if err := NewPurchaseService(db).Void(target.ID, 1); err != nil {
			t.Fatalf("la anulación intercalada: %v", err)
		}
	}
	t.Cleanup(func() { voidBeforeTxHook = nil })

	err := svc.Void(target.ID, 1)
	if err == nil || !strings.Contains(err.Error(), "ya está anulada") {
		t.Fatalf("la segunda anulación debe fallar: %v", err)
	}
	var stock database.TenantProductStock
	if err := db.Where("product_id = ? AND branch_id = ?", product.ID, 1).First(&stock).Error; err != nil {
		t.Fatal(err)
	}
	if stock.Quantity != 10 {
		t.Fatalf("stock = %v, quería 10 (revertido una vez)", stock.Quantity)
	}
	var reversals int64
	if err := db.Model(&database.TenantStockMovement{}).Where("reference = ?", "ANULACION COMPRA/F001-1").Count(&reversals).Error; err != nil {
		t.Fatal(err)
	}
	var incomes int64
	if err := db.Model(&database.TenantCashMovement{}).Where("type = ?", "income").Count(&incomes).Error; err != nil {
		t.Fatal(err)
	}
	if reversals != 1 || incomes != 1 {
		t.Fatalf("reversas de stock=%d de caja=%d, quería 1 y 1", reversals, incomes)
	}
}
