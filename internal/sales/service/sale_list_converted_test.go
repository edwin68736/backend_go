package service

import (
	"testing"
	"time"

	"tukifac/pkg/database"
)

// Tukichef/Tukifac: un comprobante emitido desde una nota de venta se marca (is_converted + número de la NV de origen) y el resumen del
// listado informa aparte cuánto de lo vigente es convertido, para que el usuario no lo cuente dos veces. Flujo real de conversión.

func TestSaleList_marcaLosComprobantesConvertidosYSuSubtotal(t *testing.T) {
	f := newNVInSession(t, nvSpec{total: 118}) // NV de contado por 118
	child := f.convert(t)

	// Una venta directa (boleta normal) de 50 en la misma sesión: no es convertida.
	sid := f.session
	direct := database.TenantSale{BranchID: 1, UserID: 1, SeriesID: f.seriesID, DocType: "BOLETA", Series: "B001", Correlative: 90, Number: "B001-00000090",
		IssueDate: time.Now(), Subtotal: 42.37, TaxAmount: 7.63, Total: 50, Currency: "PEN", CashSessionID: &sid, Status: "paid", CreatedAt: time.Now()}
	if err := f.db.Create(&direct).Error; err != nil {
		t.Fatal(err)
	}

	svc := NewSaleService(f.db)
	// Reporte comercial (Tukichef «Reporte de ventas»): cuenta el comprobante y omite la NV de la que salió.
	rows, _, sum, err := svc.List(SaleListParams{CommercialReport: true})
	if err != nil {
		t.Fatal(err)
	}
	var gotChild, gotDirect bool
	for _, r := range rows {
		switch r.ID {
		case child.ID:
			gotChild = true
			if !r.IsConverted || r.ConvertedFromNumber != f.nvNumber {
				t.Fatalf("el convertido debe marcarse con su NV de origen: is_converted=%v desde=%q (quería %q)", r.IsConverted, r.ConvertedFromNumber, f.nvNumber)
			}
		case direct.ID:
			gotDirect = true
			if r.IsConverted || r.ConvertedFromNumber != "" {
				t.Fatalf("una venta directa no es convertida: %+v", r.IsConverted)
			}
		case f.nvID:
			t.Fatal("la NV con comprobante vigente no debe aparecer en el reporte comercial")
		}
	}
	if !gotChild || !gotDirect {
		t.Fatalf("faltan filas: convertido=%v directa=%v", gotChild, gotDirect)
	}
	// Total vigente = 118 (convertido) + 50 (directa); de ellos 118 son convertidos y 1 comprobante.
	if sum.SumActive != 168 || sum.CountActive != 2 {
		t.Fatalf("total vigente = %.2f en %d, quería 168.00 en 2", sum.SumActive, sum.CountActive)
	}
	if sum.SumConverted != 118 || sum.CountConverted != 1 {
		t.Fatalf("convertidos = %.2f en %d, quería 118.00 en 1", sum.SumConverted, sum.CountConverted)
	}

	// Listado crudo de facturas/boletas (pantalla Ventas de Tukichef): el comprobante convertido es una fila más y también se marca.
	rows, _, sum, err = svc.List(SaleListParams{SunatCodes: []string{"01", "03"}})
	if err != nil {
		t.Fatal(err)
	}
	if sum.SumConverted != 118 || sum.CountConverted != 1 {
		t.Fatalf("listado crudo: convertidos = %.2f en %d, quería 118.00 en 1", sum.SumConverted, sum.CountConverted)
	}
	for _, r := range rows {
		if r.ID == child.ID && !r.IsConverted {
			t.Fatal("en el listado crudo el convertido también debe marcarse")
		}
	}
}

func TestSaleList_sinConversionesNoMarcaNada(t *testing.T) {
	f := newNVInSession(t, nvSpec{total: 118}) // NV sin convertir
	rows, _, sum, err := NewSaleService(f.db).List(SaleListParams{})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.IsConverted || r.ConvertedFromNumber != "" {
			t.Fatalf("nada debe marcarse sin conversiones: %+v", r.IsConverted)
		}
	}
	if sum.SumConverted != 0 || sum.CountConverted != 0 {
		t.Fatalf("subtotal convertido debe ser 0: %.2f/%d", sum.SumConverted, sum.CountConverted)
	}
}
