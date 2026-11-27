package sunatnote

import "strings"

// Efecto de cada motivo de nota de crédito (catálogo SUNAT 09). Lo usan la emisión, la reposición de stock y los reportes.

// partialCreditReasons: devuelven o descuentan bienes concretos. El resto copia toda la venta (anulación, correcciones de datos) o
// ajusta datos de exportación, IVAP o fecha de pago.
var partialCreditReasons = map[string]bool{
	"04": true, // Descuento global
	"05": true, // Descuento por ítem
	"06": true, // Devolución total
	"07": true, // Devolución por ítem
	"08": true, // Bonificación
	"09": true, // Disminución en el valor
	"10": true, // Otros conceptos
}

// priceOnlyCreditReasons: ajustes de precio; el cliente se queda con la mercadería.
var priceOnlyCreditReasons = map[string]bool{"04": true, "05": true, "09": true}

// IsPartialCreditReason: el motivo admite elegir ítems y cantidades o un importe.
func IsPartialCreditReason(code string) bool { return partialCreditReasons[strings.TrimSpace(code)] }

// CreditReasonReturnsStock: false cuando es un ajuste de precio.
func CreditReasonReturnsStock(code string) bool {
	return !priceOnlyCreditReasons[strings.TrimSpace(code)]
}
