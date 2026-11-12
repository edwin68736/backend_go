package tax

import "tukifac/pkg/money"

// Recargo al consumo (DL 25988, 5.ª disposición complementaria). Lo fija el establecimiento de hospedaje o de expendio de comidas y bebidas
// en acuerdo con sus trabajadores, hasta 13 % del valor de los servicios; no integra la base imponible del IGV y no es ingreso del negocio.
const (
	// MaxServiceChargeRate tope legal (porcentaje).
	MaxServiceChargeRate = 13.0
	// DefaultServiceChargeRate porcentaje que se propone al activarlo (práctica habitual; el tenant lo cambia).
	DefaultServiceChargeRate = 10.0
	// ServiceChargeCode código del catálogo 53 de SUNAT: «Recargo al consumo y/o propinas» (cargo global que no afecta la base del IGV).
	ServiceChargeCode = "46"
)

// ValidServiceChargeRate indica si el porcentaje está dentro de lo permitido (0 < tasa <= 13).
func ValidServiceChargeRate(rate float64) bool {
	return rate > 0 && rate <= MaxServiceChargeRate
}

// ServiceChargeAmount recargo sobre la base indicada (valor de venta sin IGV, ya con el descuento global aplicado), redondeado a céntimos (es un monto cobrado al cliente y va en el XML con 2 decimales).
// Devuelve 0 si la tasa no es válida o la base no es positiva: nunca cobra por encima del tope legal.
func ServiceChargeAmount(base, rate float64) float64 {
	if base <= 0 || !ValidServiceChargeRate(rate) {
		return 0
	}
	return money.RoundDisplay(base * rate / 100)
}
