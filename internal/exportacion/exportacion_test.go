package exportacion

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"tukifac/pkg/database"
	"tukifac/pkg/facturador"
	"tukifac/pkg/salecurrency"
)

func d(s string) *time.Time { t, _ := time.Parse("2006-01-02", s); return &t }

func basePayload(tipoDoc string) *facturador.InvoicePayload {
	return &facturador.InvoicePayload{
		TipoDoc: tipoDoc,
		Client:  facturador.InvoiceClient{TipoDoc: "6", NumDoc: "00000000000", RznSocial: "CLIENTES VARIOS", Address: &facturador.InvoiceAddress{Direccion: "x"}},
		Details: []facturador.InvoiceDetail{{}, {}},
	}
}

func TestApply_factura0201_adquirenteConGuionYPaisDeUso(t *testing.T) {
	p := basePayload("01")
	ApplyToInvoicePayload(p, &database.TenantSaleExport{OperationTypeCode: salecurrency.OpExportServicios, CountryUse: "US"})
	if p.Client.TipoDoc != "-" || p.Client.NumDoc != "-" || p.Client.Address != nil {
		t.Fatalf("adquirente = %+v, want «-» sin dirección", p.Client)
	}
	if len(p.Details[0].Atributos) != 1 || p.Details[0].Atributos[0].Code != AttrCountryUse || p.Details[0].Atributos[0].Value != "US" {
		t.Fatalf("país de uso: %+v", p.Details[0].Atributos)
	}
	if len(p.Details[1].Atributos) != 0 {
		t.Fatal("el país de uso va una sola vez (en la primera línea)")
	}
}

func TestApply_boleta_noCambiaElAdquirente(t *testing.T) {
	p := basePayload("03")
	ApplyToInvoicePayload(p, &database.TenantSaleExport{OperationTypeCode: salecurrency.OpExportServicios, CountryUse: "US"})
	if p.Client.TipoDoc != "6" {
		t.Fatalf("la boleta conserva su identificación: %+v", p.Client)
	}
}

func TestApply_0202_atributosDelCatalogo55YLeyenda(t *testing.T) {
	p := basePayload("01")
	row := &database.TenantSaleExport{
		OperationTypeCode: salecurrency.OpExportHospedaje, GuestName: "John Smith", GuestDocType: "7", GuestDocNumber: "X1234567",
		PassportCountry: "US", ResidenceCountry: "CA", EntryDate: d("2026-11-01"), CheckIn: d("2026-11-02"), CheckOut: d("2026-11-05"), StayDays: 3,
	}
	ApplyToInvoicePayload(p, row)
	if p.Client.RznSocial != "John Smith" {
		t.Fatalf("razón social = %q, want el nombre del huésped", p.Client.RznSocial)
	}
	for i, det := range p.Details {
		got := map[string]facturador.DetailAttribute{}
		for _, a := range det.Atributos {
			got[a.Code] = a
		}
		for _, code := range []string{"4000", "4001", "4002", "4003", "4004", "4005", "4006", "4007", "4008", "4009"} {
			if _, ok := got[code]; !ok {
				t.Errorf("línea %d sin el atributo %s", i, code)
			}
		}
		if got["4000"].Value != "US" || got["4001"].Value != "CA" || got["4009"].Value != "X1234567" || got["4005"].Duracion != 3 {
			t.Errorf("línea %d valores incorrectos: %+v", i, got)
		}
		if got["4003"].FecInicio == nil || got["4004"].FecInicio == nil {
			t.Errorf("línea %d fechas faltantes", i)
		}
	}
	found := false
	for _, e := range p.Parameters.User.Extras {
		if e.Name == "Leyenda" && e.Value == LeyendaHospedaje {
			found = true
		}
	}
	if !found {
		t.Fatalf("falta la leyenda del DL 919 en el PDF: %+v", p.Parameters)
	}
}

func TestNormalize_reglasDeHospedaje(t *testing.T) {
	ok := SaleInput{GuestName: "Ana Perez", GuestDocNumber: "P-998877", PassportCountry: "ar", ResidenceCountry: "ar", EntryDate: "2026-10-01", CheckIn: "2026-10-01", CheckOut: "2026-11-29"}
	row, err := Normalize(salecurrency.OpExportHospedaje, &ok)
	if err != nil || row.StayDays != 59 || row.PassportCountry != "AR" {
		t.Fatalf("59 días desde el ingreso debe calificar: %+v err=%v", row, err)
	}
	over := ok
	over.CheckOut = "2026-12-01" // 61 días desde el ingreso al país
	if _, err := Normalize(salecurrency.OpExportHospedaje, &over); err == nil {
		t.Fatal("61 días debió rechazarse")
	}
	badOrder := ok
	badOrder.CheckIn, badOrder.CheckOut = "2026-10-10", "2026-10-09"
	if _, err := Normalize(salecurrency.OpExportHospedaje, &badOrder); err == nil {
		t.Fatal("salida anterior al ingreso debió rechazarse")
	}
	beforeEntry := ok
	beforeEntry.CheckIn = "2026-09-30"
	if _, err := Normalize(salecurrency.OpExportHospedaje, &beforeEntry); err == nil {
		t.Fatal("ingreso al hotel anterior al ingreso al país debió rechazarse")
	}
	sameDay := ok
	sameDay.CheckOut = "2026-10-01"
	if r, err := Normalize(salecurrency.OpExportHospedaje, &sameDay); err != nil || r.StayDays != 1 {
		t.Fatalf("mismo día cuenta 1 día: %+v err=%v", r, err)
	}
}

func TestNormalize_0208ExigePaisDeUso(t *testing.T) {
	if _, err := Normalize(salecurrency.OpExportParcial, &SaleInput{}); err == nil {
		t.Fatal("0208 sin país debió rechazarse")
	}
	if _, err := Normalize(salecurrency.OpVentaInterna, &SaleInput{CountryUse: "US"}); err == nil {
		t.Fatal("una operación que no es exportación debió rechazarse")
	}
}

// Las fechas de la estadía viajan a mediodía hora Perú: así el día no cambia al convertir de zona horaria al armar el XML.
func TestApply_0202_fechasAMediodiaPeru(t *testing.T) {
	p := basePayload("01")
	ApplyToInvoicePayload(p, &database.TenantSaleExport{
		OperationTypeCode: salecurrency.OpExportHospedaje, GuestName: "John Smith", GuestDocType: "7", GuestDocNumber: "X1234567",
		PassportCountry: "US", ResidenceCountry: "CA", EntryDate: d("2026-11-01"), CheckIn: d("2026-11-02"), CheckOut: d("2026-11-05"), StayDays: 3,
	})
	raw, err := json.Marshal(p.Details[0].Atributos)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"2026-11-01T12:00:00-05:00"`, `"2026-11-02T12:00:00-05:00"`, `"2026-11-05T12:00:00-05:00"`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("falta %s en %s", want, raw)
		}
	}
}
