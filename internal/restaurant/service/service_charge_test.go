package service

import (
	"errors"
	"math"
	"testing"
	"time"

	"tukifac/pkg/database"
	"tukifac/pkg/tax"
)

// Cobro de mesa con recargo al consumo: la comanda es S/ 30 con IGV incluido (base 25.42 + IGV 4.58); el recargo es 10 % de la base (2.54) y el total a pagar sube a 32.54.
func TestBillTable_RecargoAlConsumo(t *testing.T) {
	db, table := setupTableSessionTestDB(t)
	if err := db.AutoMigrate(&database.TenantCompanyConfig{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&database.TenantCompanyConfig{ID: 1, TaxRate: 18, ServiceChargeEnabled: true, ServiceChargeRate: 10}).Error; err != nil {
		t.Fatal(err)
	}
	svc := New(db)
	sess, err := svc.OpenTableExtended(openInput(table.ID))
	if err != nil {
		t.Fatal(err)
	}
	c1 := database.TenantComanda{SessionID: sess.ID, ProductCode: "P1", ProductName: "Lomo", Quantity: 1, UnitPrice: 30, Status: "pendiente", IgvAffectationType: "10"}
	if err := db.Create(&c1).Error; err != nil {
		t.Fatal(err)
	}
	series := seedSplitBillSeries(t, db)

	// Pagar solo lo de la cuenta (sin recargo) no alcanza.
	if _, err := svc.BillTable(BillInput{SessionID: sess.ID, UserID: 1, SeriesID: series.ID, DocType: "03", IssueDate: time.Now(),
		ApplyServiceCharge: true, Payments: []PaymentInput{{Method: "cash", Amount: 30}}}, tax.DefaultConfig()); err == nil {
		t.Fatal("con recargo el total es 32.54: 30.00 no debe alcanzar")
	}

	res, err := svc.BillTable(BillInput{SessionID: sess.ID, UserID: 1, SeriesID: series.ID, DocType: "03", IssueDate: time.Now(),
		ApplyServiceCharge: true, Payments: []PaymentInput{{Method: "cash", Amount: 32.54}}}, tax.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	var sale database.TenantSale
	if err := db.Where("number = ?", res.Number).First(&sale).Error; err != nil {
		t.Fatal(err)
	}
	near := func(a, b float64) bool { return math.Abs(a-b) < 0.005 }
	if !near(sale.Subtotal, 25.42) || !near(sale.TaxAmount, 4.58) || !near(sale.ServiceChargeAmount, 2.54) || !near(sale.ServiceChargeRate, 10) || !near(sale.Total, 32.54) {
		t.Errorf("venta con recargo: %+v", sale)
	}
}

func TestBillTable_RecargoConTenantApagado_SeRechaza(t *testing.T) {
	db, table := setupTableSessionTestDB(t)
	if err := db.AutoMigrate(&database.TenantCompanyConfig{}); err != nil {
		t.Fatal(err)
	}
	svc := New(db)
	sess, err := svc.OpenTableExtended(openInput(table.ID))
	if err != nil {
		t.Fatal(err)
	}
	c1 := database.TenantComanda{SessionID: sess.ID, ProductCode: "P1", ProductName: "Lomo", Quantity: 1, UnitPrice: 30, Status: "pendiente", IgvAffectationType: "10"}
	db.Create(&c1)
	series := seedSplitBillSeries(t, db)
	_, err = svc.BillTable(BillInput{SessionID: sess.ID, UserID: 1, SeriesID: series.ID, DocType: "03", IssueDate: time.Now(),
		ApplyServiceCharge: true, Payments: []PaymentInput{{Method: "cash", Amount: 40}}}, tax.DefaultConfig())
	if !errors.Is(err, tax.ErrServiceChargeDisabled) {
		t.Fatalf("esperaba ErrServiceChargeDisabled, got %v", err)
	}
}
