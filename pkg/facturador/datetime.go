package facturador

import "time"

// FiscalDateTimeLayout es el formato exigido por facturador_lycet / JMS (Y-m-d\TH:i:sP).
const FiscalDateTimeLayout = "2006-01-02T15:04:05-07:00"

func limaLocation() *time.Location {
	loc, err := time.LoadLocation("America/Lima")
	if err != nil {
		return time.FixedZone("America/Lima", -5*3600)
	}
	return loc
}

// FormatFiscalDateTime formatea una fecha/hora para payloads SUNAT/PSE (fecha calendario Perú al mediodía).
func FormatFiscalDateTime(t time.Time) string {
	loc := limaLocation()
	d := t.In(loc)
	normalized := time.Date(d.Year(), d.Month(), d.Day(), 12, 0, 0, 0, loc)
	return normalized.Format(FiscalDateTimeLayout)
}
