package service

import (
	"strings"
	"testing"

	"tukifac/internal/exportacion"
	"tukifac/pkg/database"
	"tukifac/pkg/salecurrency"

	"gorm.io/gorm"
)

func setupExportTestDB(t *testing.T, enabled bool) (*gorm.DB, uint, uint) {
	t.Helper()
	db := setupMaxMontoTestDB(t)
	if err := db.AutoMigrate(&database.TenantSaleExport{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&database.TenantCompanyConfig{}).Where("id = 1").Update("exports_enabled", enabled).Error; err != nil {
		t.Fatal(err)
	}
	return db, seedClienteSinRUC(t, db), seedSeries(t, db, "Factura", "01", "F001")
}

func exportInput(contactID, seriesID uint, op string, ex *exportacion.SaleInput) CreateSaleInput {
	in := facturaOperationTypeInput(contactID, seriesID, op, 118)
	in.Export = ex
	return in
}

// Sin el interruptor de la empresa no se puede exportar.
func TestExport_apagadoPorDefecto(t *testing.T) {
	db, c, s := setupExportTestDB(t, false)
	_, err := NewSaleService(db).Create(exportInput(c, s, salecurrency.OpExportServicios, &exportacion.SaleInput{CountryUse: "US"}))
	if err == nil || !strings.Contains(err.Error(), "habilitada") {
		t.Fatalf("err = %v, want mensaje de exportación no habilitada", err)
	}
}

// 0201: factura a un cliente sin RUC, país de uso obligatorio; las líneas salen sin IGV aunque el cajero las haya dejado gravadas.
func TestExport_0201SinIGVYConDatos(t *testing.T) {
	db, c, s := setupExportTestDB(t, true)
	svc := NewSaleService(db)
	if _, err := svc.Create(exportInput(c, s, salecurrency.OpExportServicios, &exportacion.SaleInput{})); err == nil {
		t.Fatal("0201 sin país de uso debió rechazarse")
	}
	if _, err := svc.Create(exportInput(c, s, salecurrency.OpExportServicios, &exportacion.SaleInput{CountryUse: "PE"})); err == nil {
		t.Fatal("0201 con país Perú debió rechazarse")
	}
	sale, err := svc.Create(exportInput(c, s, salecurrency.OpExportServicios, &exportacion.SaleInput{CountryUse: "us"}))
	if err != nil {
		t.Fatal(err)
	}
	if sale.TaxAmount != 0 || sale.Total != 118 || sale.Subtotal != 118 {
		t.Fatalf("exportación sin IGV: subtotal=%v igv=%v total=%v, want 118/0/118", sale.Subtotal, sale.TaxAmount, sale.Total)
	}
	var it database.TenantSaleItem
	db.Where("sale_id = ?", sale.ID).First(&it)
	if it.IgvAffectationType != "40" {
		t.Fatalf("afectación = %q, want 40", it.IgvAffectationType)
	}
	row, _ := exportacion.LoadBySaleID(db, sale.ID)
	if row == nil || row.CountryUse != "US" || row.OperationTypeCode != "0201" {
		t.Fatalf("datos de exportación no guardados: %+v", row)
	}
}

// 0202: huésped completo; más de 60 días o residente en el Perú no califican.
func TestExport_0202Hospedaje(t *testing.T) {
	db, c, s := setupExportTestDB(t, true)
	svc := NewSaleService(db)
	ok := exportacion.SaleInput{
		GuestName: "John Smith", GuestDocNumber: "X1234567", PassportCountry: "US", ResidenceCountry: "US",
		EntryDate: "2026-11-01", CheckIn: "2026-11-02", CheckOut: "2026-11-05",
	}
	sale, err := svc.Create(exportInput(c, s, salecurrency.OpExportHospedaje, &ok))
	if err != nil {
		t.Fatal(err)
	}
	row, _ := exportacion.LoadBySaleID(db, sale.ID)
	if row == nil || row.StayDays != 3 || row.GuestDocType != "7" {
		t.Fatalf("estadía mal guardada: %+v", row)
	}
	long := ok
	long.CheckOut = "2027-02-01" // > 60 días desde el ingreso al país
	if _, err := svc.Create(exportInput(c, s, salecurrency.OpExportHospedaje, &long)); err == nil || !strings.Contains(err.Error(), "60") {
		t.Fatalf("más de 60 días debió rechazarse con el motivo: %v", err)
	}
	resident := ok
	resident.ResidenceCountry = "PE"
	if _, err := svc.Create(exportInput(c, s, salecurrency.OpExportHospedaje, &resident)); err == nil {
		t.Fatal("huésped residente en el Perú debió rechazarse")
	}
}

// La exportación también se puede emitir en boleta, pero no con otros tipos de documento, ni con datos de exportación en una venta normal.
func TestExport_soloFacturaOBoleta_yNoEnVentaNormal(t *testing.T) {
	db, c, s := setupExportTestDB(t, true)
	svc := NewSaleService(db)
	nv := seedSeries(t, db, "Nota de venta", "00", "NV01")
	if _, err := svc.Create(exportInput(c, nv, salecurrency.OpExportServicios, &exportacion.SaleInput{CountryUse: "US"})); err == nil {
		t.Fatal("exportación en nota de venta debió rechazarse")
	}
	if _, err := svc.Create(exportInput(c, s, salecurrency.OpVentaInterna, &exportacion.SaleInput{CountryUse: "US"})); err == nil {
		t.Fatal("datos de exportación en una venta interna debieron rechazarse")
	}
}
