// Package exportacion implementa la exportación de servicios (operaciones 0201, 0202 y 0208 del catálogo 51 de SUNAT): ventas a un cliente
// del exterior que no pagan IGV (afectación 40). Pide solo los datos que la norma exige para cada operación y los envía al facturador.
package exportacion

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"tukifac/pkg/database"
	"tukifac/pkg/facturador"
	"tukifac/pkg/salecurrency"

	"gorm.io/gorm"
)

// LeyendaHospedaje leyenda obligatoria en la factura de hospedaje a no domiciliados (DS 122-2001-EF, art. 6).
const LeyendaHospedaje = "EXPORTACIÓN DE SERVICIOS - DECRETO LEGISLATIVO Nº 919"

// AttrCountryUse código reservado que el backend envía en details[0].atributos y que el facturador convierte en
// cac:Delivery/cac:DeliveryLocation/cac:Address/cac:Country (país del uso, explotación o aprovechamiento del servicio). No es del catálogo 55.
const AttrCountryUse = "EXP_PAIS_USO"

// MaxStayDays permanencia máxima desde el ingreso al país para que el hospedaje cuente como exportación (Ley del IGV, art. 33.4).
const MaxStayDays = 60

// SaleInput lo que el cajero completa en el comprobante. Fechas en formato YYYY-MM-DD.
type SaleInput struct {
	// 0201 y 0208: país donde se usa el servicio (ISO alfa-2, p. ej. US).
	CountryUse string `json:"country_use"`
	// 0202: datos del huésped y su estadía.
	GuestName        string `json:"guest_name"`
	GuestDocType     string `json:"guest_doc_type"` // 7 pasaporte (por defecto), 4 carné de extranjería, B/G/...
	GuestDocNumber   string `json:"guest_doc_number"`
	PassportCountry  string `json:"passport_country"`
	ResidenceCountry string `json:"residence_country"`
	EntryDate        string `json:"entry_date"` // ingreso al país
	CheckIn          string `json:"check_in"`
	CheckOut         string `json:"check_out"`
}

var countryRe = regexp.MustCompile(`^[A-Z]{2}$`)

func normCountry(raw string) string { return strings.ToUpper(strings.TrimSpace(raw)) }

func parseDate(label, raw string) (*time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("indique %s", label)
	}
	t, err := time.Parse("2006-01-02", raw)
	if err != nil {
		return nil, fmt.Errorf("%s no es una fecha válida (use AAAA-MM-DD)", label)
	}
	return &t, nil
}

func daysBetween(from, to time.Time) int { return int(to.Sub(from).Hours() / 24) }

// Normalize valida los datos según la operación y devuelve la fila a guardar (sin SaleID). Los mensajes están pensados para el cajero.
func Normalize(opCode string, in *SaleInput) (*database.TenantSaleExport, error) {
	if !salecurrency.IsExportService(opCode) {
		return nil, fmt.Errorf("la operación %s no es una exportación de servicios", opCode)
	}
	if in == nil {
		return nil, errors.New("complete los datos de la exportación")
	}
	row := &database.TenantSaleExport{OperationTypeCode: opCode}

	if opCode != salecurrency.OpExportHospedaje {
		c := normCountry(in.CountryUse)
		if !countryRe.MatchString(c) {
			return nil, errors.New("indique el país donde se usa el servicio")
		}
		if c == "PE" {
			return nil, errors.New("en una exportación el servicio se usa fuera del Perú: elija un país del exterior")
		}
		row.CountryUse = c
		return row, nil
	}

	// 0202: hospedaje a no domiciliados.
	row.GuestName = strings.Join(strings.Fields(in.GuestName), " ")
	if len([]rune(row.GuestName)) < 3 {
		return nil, errors.New("indique el nombre del huésped")
	}
	row.GuestDocType = strings.ToUpper(strings.TrimSpace(in.GuestDocType))
	if row.GuestDocType == "" {
		row.GuestDocType = "7"
	}
	row.GuestDocNumber = strings.TrimSpace(in.GuestDocNumber)
	if len(row.GuestDocNumber) < 4 {
		return nil, errors.New("indique el número de pasaporte (o documento) del huésped")
	}
	row.PassportCountry = normCountry(in.PassportCountry)
	if !countryRe.MatchString(row.PassportCountry) {
		return nil, errors.New("indique el país que emitió el pasaporte")
	}
	row.ResidenceCountry = normCountry(in.ResidenceCountry)
	if !countryRe.MatchString(row.ResidenceCountry) {
		return nil, errors.New("indique el país de residencia del huésped")
	}
	if row.ResidenceCountry == "PE" {
		return nil, errors.New("el huésped reside en el Perú: no es un no domiciliado, así que el hospedaje no es una exportación (emita una factura o boleta normal con IGV)")
	}
	entry, err := parseDate("la fecha de ingreso al país", in.EntryDate)
	if err != nil {
		return nil, err
	}
	checkIn, err := parseDate("la fecha de ingreso al hotel", in.CheckIn)
	if err != nil {
		return nil, err
	}
	checkOut, err := parseDate("la fecha de salida del hotel", in.CheckOut)
	if err != nil {
		return nil, err
	}
	if checkIn.Before(*entry) {
		return nil, errors.New("la fecha de ingreso al hotel no puede ser anterior al ingreso al país")
	}
	if checkOut.Before(*checkIn) {
		return nil, errors.New("la fecha de salida no puede ser anterior a la de ingreso al hotel")
	}
	if daysBetween(*entry, *checkOut) > MaxStayDays {
		return nil, fmt.Errorf("el huésped supera los %d días desde su ingreso al país: ya no califica como exportación (emita una factura o boleta normal con IGV)", MaxStayDays)
	}
	row.EntryDate, row.CheckIn, row.CheckOut = entry, checkIn, checkOut
	row.StayDays = daysBetween(*checkIn, *checkOut)
	if row.StayDays < 1 {
		row.StayDays = 1
	}
	return row, nil
}

// Persist guarda los datos de la exportación de la venta. Idempotente por venta.
func Persist(tx *gorm.DB, saleID uint, row *database.TenantSaleExport) error {
	if row == nil {
		return nil
	}
	row.SaleID = saleID
	return tx.Save(row).Error
}

// LoadBySaleID devuelve los datos de exportación de la venta, o nil si no es una exportación.
func LoadBySaleID(db *gorm.DB, saleID uint) (*database.TenantSaleExport, error) {
	var row database.TenantSaleExport
	if err := db.Where("sale_id = ?", saleID).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &row, nil
}

// ApplyToInvoicePayload completa el payload del facturador: adquirente con guion (factura), datos del huésped por línea (0202) o país de
// uso (0201/0208), y la leyenda en el PDF. Las líneas ya vienen con afectación 40 (IGV 0) desde la venta.
func ApplyToInvoicePayload(payload *facturador.InvoicePayload, row *database.TenantSaleExport) {
	if payload == nil || row == nil {
		return
	}
	// RS 097-2012 art. 17.2 (anexo RS 123-2022): en exportaciones a no domiciliados la factura identifica al adquirente con «-».
	if payload.TipoDoc == "01" {
		payload.Client.TipoDoc, payload.Client.NumDoc = "-", "-"
		payload.Client.Address = nil
	}
	extras := []facturador.InvoicePDFExtra{{Name: "Tipo operación", Value: OperationLabel(row.OperationTypeCode)}}

	switch row.OperationTypeCode {
	case salecurrency.OpExportHospedaje:
		if row.GuestName != "" {
			payload.Client.RznSocial = row.GuestName
		}
		attrs := hospedajeAttributes(row)
		for i := range payload.Details {
			payload.Details[i].Atributos = append(payload.Details[i].Atributos, attrs...)
		}
		extras = append(extras, facturador.InvoicePDFExtra{Name: "Leyenda", Value: LeyendaHospedaje})
	default:
		if row.CountryUse != "" && len(payload.Details) > 0 {
			payload.Details[0].Atributos = append(payload.Details[0].Atributos,
				facturador.DetailAttribute{Code: AttrCountryUse, Name: "País del uso del servicio", Value: row.CountryUse})
		}
	}
	mergePDFExtras(payload, extras)
}

// peruZone UTC-5 fijo (Perú no tiene horario de verano). Las fechas de calendario (ingreso, salida) viajan al facturador a mediodía en
// esta zona: así el día no se corre a otro al convertir de zona horaria al armar el XML (medianoche UTC sería el día anterior en Lima).
var peruZone = time.FixedZone("UTC-5", -5*3600)

func noonPeru(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	y, m, d := t.Date()
	n := time.Date(y, m, d, 12, 0, 0, 0, peruZone)
	return &n
}

// hospedajeAttributes propiedades del catálogo 55 «Beneficio Hospedajes» (4000–4009) que SUNAT exige con la operación 0202.
func hospedajeAttributes(r *database.TenantSaleExport) []facturador.DetailAttribute {
	str := func(code, name, v string) facturador.DetailAttribute {
		return facturador.DetailAttribute{Code: code, Name: name, Value: v}
	}
	out := []facturador.DetailAttribute{
		str("4000", "Beneficio Hospedajes-Paquete turístico: Código de país de emisión del pasaporte", r.PassportCountry),
		str("4001", "Beneficio Hospedajes: Código de país de residencia del sujeto no domiciliado", r.ResidenceCountry),
	}
	if r.EntryDate != nil {
		out = append(out, facturador.DetailAttribute{Code: "4002", Name: "Beneficio Hospedajes: Fecha de ingreso al país", FecInicio: noonPeru(r.EntryDate)})
	}
	if r.CheckIn != nil {
		out = append(out, facturador.DetailAttribute{Code: "4003", Name: "Beneficio Hospedajes: Fecha de Ingreso al Establecimiento", FecInicio: noonPeru(r.CheckIn)})
	}
	if r.CheckOut != nil {
		out = append(out, facturador.DetailAttribute{Code: "4004", Name: "Beneficio Hospedajes: Fecha de Salida del Establecimiento", FecFin: noonPeru(r.CheckOut)})
	}
	days := r.StayDays
	out = append(out, facturador.DetailAttribute{Code: "4005", Name: "Beneficio Hospedajes: Número de Días de Permanencia", Duracion: days})
	if r.CheckIn != nil {
		out = append(out, facturador.DetailAttribute{Code: "4006", Name: "Beneficio Hospedajes: Fecha de Consumo", FecInicio: noonPeru(r.CheckIn)})
	}
	return append(out,
		str("4007", "Beneficio Hospedajes-Paquete turístico: Nombres y apellidos del huesped", r.GuestName),
		str("4008", "Beneficio Hospedajes-Paquete turístico: Tipo de documento de identidad del huesped", r.GuestDocType),
		str("4009", "Beneficio Hospedajes-Paquete turístico: Número de documento de identidad del huesped", r.GuestDocNumber),
	)
}

// OperationLabel nombre corto de la operación para el comprobante.
func OperationLabel(code string) string {
	switch code {
	case salecurrency.OpExportHospedaje:
		return "Exportación de servicios - Hospedaje a no domiciliados (0202)"
	case salecurrency.OpExportParcial:
		return "Exportación de servicios - Realizados parcialmente en el extranjero (0208)"
	default:
		return "Exportación de servicios (0201)"
	}
}

func mergePDFExtras(payload *facturador.InvoicePayload, extras []facturador.InvoicePDFExtra) {
	if payload.Parameters == nil {
		payload.Parameters = &facturador.InvoicePDFParameters{}
	}
	for _, e := range extras {
		dup := false
		for _, cur := range payload.Parameters.User.Extras {
			if strings.EqualFold(strings.TrimSpace(cur.Name), e.Name) {
				dup = true
				break
			}
		}
		if !dup {
			payload.Parameters.User.Extras = append(payload.Parameters.User.Extras, e)
		}
	}
}
