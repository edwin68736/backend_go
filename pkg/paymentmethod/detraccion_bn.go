package paymentmethod

import "strings"

const (
	CodeDetraccionBN      = "detraccion_bn"
	NameDetraccionBN      = "Detracción BN (SPOT)"
	DestinationDetraction = "detraction"
)

// IsDetractionCode indica si el código corresponde al método interno de detracción BN.
func IsDetractionCode(code string) bool {
	return strings.EqualFold(strings.TrimSpace(code), CodeDetraccionBN)
}
