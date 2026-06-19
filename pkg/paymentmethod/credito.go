package paymentmethod

import "strings"

const (
	CodeCredito            = "credito"
	NameCredito            = "Crédito"
	DestinationReceivable  = "receivable"
)

// IsReceivableCode indica método interno de crédito / CxC (sin movimiento de caja).
func IsReceivableCode(code string) bool {
	c := strings.TrimSpace(strings.ToLower(code))
	return c == CodeCredito || c == "credit"
}
