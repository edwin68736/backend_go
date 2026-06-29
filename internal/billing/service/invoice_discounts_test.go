package service

import (
	"encoding/json"
	"math"
	"strings"
	"testing"

	"tukifac/pkg/database"
	"tukifac/pkg/facturador"
	"tukifac/pkg/tax"
)

func testNormUnit(u string) string {
	if u == "" {
		return "NIU"
	}
	return u
}

func TestBuildInvoiceDetails_LineDiscountOnly(t *testing.T) {
	// Greenter factura-descuento-linea: bruto 20, neto 16, unitario bruto 2.
	items := []database.TenantSaleItem{{
		Code: "P001", Description: "Prod", Unit: "NIU", Quantity: 10,
		IgvAffectationType: "10", TaxRate: 18,
		Subtotal: 180, TaxAmount: 32.4, Total: 212.4,
		LineDiscountSubtotal: 20,
	}}
	details, err := BuildInvoiceDetailsFromSaleItems(items, 18, testNormUnit)
	if err != nil {
		t.Fatal(err)
	}
	d := details[0]
	if d.MtoValorUnitario != 20 {
		t.Fatalf("mtoValorUnitario=%v want 20 (bruto unitario)", d.MtoValorUnitario)
	}
	if d.MtoValorVenta != 180 || d.MtoBaseIgv != 180 || d.Igv != 32.4 {
		t.Fatalf("post-line: venta=%v base=%v igv=%v", d.MtoValorVenta, d.MtoBaseIgv, d.Igv)
	}
	if d.MtoPrecioUnitario != 21.24 {
		t.Fatalf("mtoPrecioUnitario=%v want 21.24", d.MtoPrecioUnitario)
	}
	if len(d.Descuentos) != 1 || d.Descuentos[0].CodTipo != tax.AllowanceCodeLineDiscountAffectsIGV {
		t.Fatalf("descuentos=%+v", d.Descuentos)
	}
	if d.Descuentos[0].MontoBase != 200 || d.Descuentos[0].Monto != 20 {
		t.Fatalf("allowance=%+v", d.Descuentos[0])
	}
}

func TestBuildInvoiceDetails_GlobalDiscountOnly(t *testing.T) {
	// Caso auditoría: precio 500 IGV incl., desc. global 50.
	items := []database.TenantSaleItem{{
		Code: "P001", Description: "Prod", Unit: "NIU", Quantity: 1,
		IgvAffectationType: "10", TaxRate: 18,
		Subtotal: 373.73, TaxAmount: 67.27, Total: 441,
		GlobalDiscountSubtotal: 50,
	}}
	details, err := BuildInvoiceDetailsFromSaleItems(items, 18, testNormUnit)
	if err != nil {
		t.Fatal(err)
	}
	d := details[0]
	if d.MtoValorVenta != 423.73 || d.MtoBaseIgv != 423.73 {
		t.Fatalf("pre-global venta/base=%v/%v want 423.73", d.MtoValorVenta, d.MtoBaseIgv)
	}
	if d.Igv != 76.27 {
		t.Fatalf("igv=%v want 76.27 (pre-global)", d.Igv)
	}
	if d.MtoPrecioUnitario != 500 {
		t.Fatalf("mtoPrecioUnitario=%v want 500", d.MtoPrecioUnitario)
	}
	if d.MtoValorUnitario != 423.73 {
		t.Fatalf("mtoValorUnitario=%v want 423.73", d.MtoValorUnitario)
	}

	sale := &database.TenantSale{GlobalDiscountAmount: 50}
	charges, sumOtros := BuildGlobalInvoiceDiscounts(sale, items)
	if sumOtros != 0 {
		t.Fatalf("sumOtrosDescuentos=%v want 0 for cod 02", sumOtros)
	}
	if len(charges) != 1 || charges[0].CodTipo != tax.AllowanceCodeGlobalDiscountAffectsIGV {
		t.Fatalf("charges=%+v", charges)
	}
	if charges[0].MontoBase != 423.73 || charges[0].Monto != 50 {
		t.Fatalf("global charge=%+v", charges[0])
	}
}

func TestBuildInvoiceDetails_LineAndGlobalCombined(t *testing.T) {
	items := []database.TenantSaleItem{{
		Code: "P001", Description: "Prod", Unit: "NIU", Quantity: 1,
		IgvAffectationType: "10", TaxRate: 18,
		Subtotal: 81, TaxAmount: 14.58, Total: 95.58,
		LineDiscountSubtotal: 10, GlobalDiscountSubtotal: 9,
	}}
	details, err := BuildInvoiceDetailsFromSaleItems(items, 18, testNormUnit)
	if err != nil {
		t.Fatal(err)
	}
	d := details[0]
	if d.MtoValorUnitario != 100 {
		t.Fatalf("mtoValorUnitario=%v want 100", d.MtoValorUnitario)
	}
	if d.MtoValorVenta != 90 || d.MtoBaseIgv != 90 || d.Igv != 16.2 {
		t.Fatalf("pre-global: venta=%v base=%v igv=%v want 90/90/16.2", d.MtoValorVenta, d.MtoBaseIgv, d.Igv)
	}
	if d.MtoPrecioUnitario != 106.2 {
		t.Fatalf("mtoPrecioUnitario=%v want 106.2", d.MtoPrecioUnitario)
	}
	if len(d.Descuentos) != 1 || d.Descuentos[0].Monto != 10 {
		t.Fatalf("line allowance=%+v", d.Descuentos[0])
	}
}

func TestBuildInvoiceDetails_MixedThreeProducts(t *testing.T) {
	items := []database.TenantSaleItem{
		{
			Code: "A", Description: "Gravado", Unit: "NIU", Quantity: 1,
			IgvAffectationType: "10", TaxRate: 18,
			Subtotal: 83.08, TaxAmount: 14.95, Total: 98.03,
			LineDiscountSubtotal: 10, GlobalDiscountSubtotal: 6.92,
		},
		{
			Code: "B", Description: "Exonerado", Unit: "NIU", Quantity: 2,
			IgvAffectationType: "20", TaxRate: 0,
			Subtotal: 129.23, TaxAmount: 0, Total: 129.23,
			LineDiscountSubtotal: 20, GlobalDiscountSubtotal: 10.77,
		},
		{
			Code: "C", Description: "Gravado IGV incl.", Unit: "NIU", Quantity: 1,
			IgvAffectationType: "10", TaxRate: 18,
			Subtotal: 87.69, TaxAmount: 15.78, Total: 103.47,
			LineDiscountSubtotal: 5, GlobalDiscountSubtotal: 7.31,
		},
	}
	details, err := BuildInvoiceDetailsFromSaleItems(items, 18, testNormUnit)
	if err != nil {
		t.Fatal(err)
	}

	var sumPreGlobal, sumLineIGVPre float64
	want := []struct {
		valorVenta, baseIgv, igv, valorUnit, precioUnit float64
	}{
		{90, 90, 16.2, 100, 106.2},
		{140, 140, 0, 80, 70},
		{95, 95, 17.1, 100, 112.1},
	}
	for i, d := range details {
		w := want[i]
		if d.MtoValorVenta != w.valorVenta || d.MtoBaseIgv != w.baseIgv || d.Igv != w.igv {
			t.Fatalf("L%d pre-global venta/base/igv=%v/%v/%v want %v/%v/%v",
				i+1, d.MtoValorVenta, d.MtoBaseIgv, d.Igv, w.valorVenta, w.baseIgv, w.igv)
		}
		if d.MtoValorUnitario != w.valorUnit || d.MtoPrecioUnitario != w.precioUnit {
			t.Fatalf("L%d unit/precio=%v/%v want %v/%v", i+1, d.MtoValorUnitario, d.MtoPrecioUnitario, w.valorUnit, w.precioUnit)
		}
		sumPreGlobal += d.MtoValorVenta
		sumLineIGVPre += d.Igv
	}

	sale := &database.TenantSale{GlobalDiscountAmount: 25}
	charges, sumOtros := BuildGlobalInvoiceDiscounts(sale, items)
	if sumOtros != 0 {
		t.Fatalf("sumOtros=%v want 0", sumOtros)
	}
	if charges[0].MontoBase != 325 || charges[0].Monto != 25 {
		t.Fatalf("global=%+v want base 325 monto 25", charges[0])
	}

	var sumFinalSub, sumFinalTax float64
	for _, it := range items {
		sumFinalSub += it.Subtotal
		sumFinalTax += it.TaxAmount
	}
	if math.Abs(sumPreGlobal-charges[0].MontoBase) > 0.01 {
		t.Fatalf("sum pre-global=%v base charge=%v", sumPreGlobal, charges[0].MontoBase)
	}
	if math.Abs(sumPreGlobal-charges[0].Monto-sumFinalSub) > 0.02 {
		t.Fatalf("valorVenta doc=%v want %v", sumFinalSub, sumPreGlobal-charges[0].Monto)
	}
	// IGV doc = suma PRE-global − IGV sobre cuotas gravadas del global
	globalIGVRed := round2(6.92*0.18 + 7.31*0.18)
	if math.Abs(sumLineIGVPre-globalIGVRed-sumFinalTax) > 0.02 {
		t.Fatalf("igv doc=%v calc=%v (pre=%v red=%v)", sumFinalTax, sumLineIGVPre-globalIGVRed, sumLineIGVPre, globalIGVRed)
	}
}

func TestBuildGlobalInvoiceDiscounts_NoSumOtrosDescuentosInJSON(t *testing.T) {
	sale := &database.TenantSale{GlobalDiscountAmount: 50}
	items := []database.TenantSaleItem{{
		Subtotal: 373.73, GlobalDiscountSubtotal: 50,
	}}
	_, sumOtros := BuildGlobalInvoiceDiscounts(sale, items)
	if sumOtros != 0 {
		t.Fatalf("sumOtros=%v", sumOtros)
	}
	payload := facturador.InvoicePayload{
		SumOtrosDescuentos: sumOtros,
		MtoImpVenta:        441,
	}
	b, _ := json.Marshal(payload)
	if strings.Contains(string(b), "sumOtrosDescuentos") {
		t.Fatalf("JSON should omit sumOtrosDescuentos: %s", string(b))
	}
}
