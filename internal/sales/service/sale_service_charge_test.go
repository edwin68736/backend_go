package service

import (
	"errors"
	"math"
	"testing"

	"tukifac/pkg/database"
	"tukifac/pkg/tax"

	"gorm.io/gorm"
)

func near(a, b float64) bool { return math.Abs(a-b) < 0.005 }

func setServiceCharge(t *testing.T, db *gorm.DB, enabled bool, rate float64) {
	t.Helper()
	if err := db.Model(&database.TenantCompanyConfig{}).Where("id = 1").
		Updates(map[string]interface{}{"service_charge_enabled": enabled, "service_charge_rate": rate}).Error; err != nil {
		t.Fatal(err)
	}
}

// El recargo al consumo se calcula sobre el valor de venta sin IGV, no toca el IGV y suma al total a pagar.
func TestCreate_RecargoAlConsumoSobreValorDeVentaSinIGV(t *testing.T) {
	db := setupMaxMontoTestDB(t)
	svc := NewSaleService(db)
	contactID := seedClienteConRUC(t, db)
	seriesID := seedSeries(t, db, "Factura", "01", "F001")
	setServiceCharge(t, db, true, 10)

	// S/ 118 con IGV = 100 + 18. Recargo 10 % de 100 = 10 → total a pagar 128.
	in := maxMontoSaleInput(contactID, seriesID, "01", 118)
	in.Payments = []PaymentInput{{Method: "cash", Amount: 128}}
	in.ApplyServiceCharge = true
	sale, err := svc.Create(in)
	if err != nil {
		t.Fatalf("venta con recargo: %v", err)
	}
	if !near(sale.Subtotal, 100) || !near(sale.TaxAmount, 18) {
		t.Errorf("el recargo no debe tocar base ni IGV: subtotal=%v igv=%v", sale.Subtotal, sale.TaxAmount)
	}
	if !near(sale.ServiceChargeAmount, 10) || !near(sale.ServiceChargeRate, 10) {
		t.Errorf("recargo: monto=%v tasa=%v", sale.ServiceChargeAmount, sale.ServiceChargeRate)
	}
	if !near(sale.Total, 128) {
		t.Errorf("total a pagar = %v, quiero 128", sale.Total)
	}
}

// El cajero elige cobrarlo o no; sin la marca no hay recargo aunque el tenant lo tenga activado.
func TestCreate_SinMarcaNoCobraRecargo(t *testing.T) {
	db := setupMaxMontoTestDB(t)
	svc := NewSaleService(db)
	contactID := seedClienteConRUC(t, db)
	seriesID := seedSeries(t, db, "Factura", "01", "F001")
	setServiceCharge(t, db, true, 10)

	sale, err := svc.Create(maxMontoSaleInput(contactID, seriesID, "01", 118))
	if err != nil {
		t.Fatal(err)
	}
	if sale.ServiceChargeAmount != 0 || sale.ServiceChargeRate != 0 || !near(sale.Total, 118) {
		t.Errorf("sin marca no hay recargo: %+v", sale)
	}
}

// Pedir recargo cuando el tenant no lo activó se rechaza (una pantalla desactualizada no debe cobrarlo).
func TestCreate_RecargoConTenantApagado_SeRechaza(t *testing.T) {
	db := setupMaxMontoTestDB(t)
	svc := NewSaleService(db)
	contactID := seedClienteConRUC(t, db)
	seriesID := seedSeries(t, db, "Factura", "01", "F001")

	in := maxMontoSaleInput(contactID, seriesID, "01", 118)
	in.ApplyServiceCharge = true
	if _, err := svc.Create(in); !errors.Is(err, tax.ErrServiceChargeDisabled) {
		t.Fatalf("esperaba ErrServiceChargeDisabled, got %v", err)
	}
}

// Un % configurado fuera del tope legal (dato corrupto) nunca se cobra.
func TestCreate_RecargoConPorcentajeFueraDeTope_SeRechaza(t *testing.T) {
	db := setupMaxMontoTestDB(t)
	svc := NewSaleService(db)
	contactID := seedClienteConRUC(t, db)
	seriesID := seedSeries(t, db, "Factura", "01", "F001")
	setServiceCharge(t, db, true, 20)

	in := maxMontoSaleInput(contactID, seriesID, "01", 118)
	in.Payments = []PaymentInput{{Method: "cash", Amount: 200}}
	in.ApplyServiceCharge = true
	if _, err := svc.Create(in); err == nil {
		t.Fatal("un % sobre el tope de 13 debió rechazarse")
	}
}

// La nota de venta (00) también puede llevarlo: es el comprobante interno de muchos restaurantes.
func TestCreate_RecargoEnNotaDeVenta(t *testing.T) {
	db := setupMaxMontoTestDB(t)
	svc := NewSaleService(db)
	contactID := seedClienteSinRUC(t, db)
	seriesID := seedSeries(t, db, "Nota de venta", "00", "NV01")
	setServiceCharge(t, db, true, 10)

	in := maxMontoSaleInput(contactID, seriesID, "00", 118)
	in.Payments = []PaymentInput{{Method: "cash", Amount: 128}}
	in.ApplyServiceCharge = true
	sale, err := svc.Create(in)
	if err != nil {
		t.Fatal(err)
	}
	if !near(sale.ServiceChargeAmount, 10) || !near(sale.Total, 128) {
		t.Errorf("nota de venta con recargo: %+v", sale)
	}
}
