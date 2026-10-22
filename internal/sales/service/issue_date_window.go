package service

import (
	"fmt"
	"strings"
	"time"

	"tukifac/pkg/datespe"
)

// ElectronicIssueMaxBackdateDays: cuántos días hacia atrás se permite fechar una factura o boleta
// emitida desde una nota de venta (SUNAT no admite emitir con fechas más antiguas).
const ElectronicIssueMaxBackdateDays = 3

// resolveElectronicIssueDate valida la fecha de emisión elegida al convertir una nota de venta en
// factura/boleta: calendario de Perú, nunca futura y con a lo sumo ElectronicIssueMaxBackdateDays
// días de antigüedad. Vacía: la fecha de la nota si cae dentro de la ventana, y si no hoy (una nota
// vieja no puede arrastrar su fecha a un comprobante que SUNAT rechazaría).
func resolveElectronicIssueDate(raw string, notaDate, now time.Time) (time.Time, error) {
	loc := datespe.Location()
	nowLima := now.In(loc)
	today := time.Date(nowLima.Year(), nowLima.Month(), nowLima.Day(), 0, 0, 0, 0, loc)
	oldest := today.AddDate(0, 0, -ElectronicIssueMaxBackdateDays)

	raw = strings.TrimSpace(raw)
	if raw == "" {
		nd := notaDate.In(loc)
		ndDay := time.Date(nd.Year(), nd.Month(), nd.Day(), 0, 0, 0, 0, loc)
		if ndDay.Before(oldest) || ndDay.After(today) {
			return now, nil
		}
		return notaDate, nil
	}
	day, err := time.ParseInLocation("2006-01-02", raw, loc)
	if err != nil {
		return time.Time{}, fmt.Errorf("fecha de emisión inválida %q (use AAAA-MM-DD)", raw)
	}
	if day.After(today) {
		return time.Time{}, fmt.Errorf("la fecha de emisión no puede ser futura")
	}
	if day.Before(oldest) {
		return time.Time{}, fmt.Errorf("la fecha de emisión no puede ser anterior a %d días (mínimo %s)",
			ElectronicIssueMaxBackdateDays, oldest.Format("02/01/2006"))
	}
	if day.Equal(today) {
		return now, nil
	}
	return time.Date(day.Year(), day.Month(), day.Day(), 12, 0, 0, 0, loc), nil
}
