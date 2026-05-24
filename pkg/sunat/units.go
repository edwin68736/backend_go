package sunat

import "strings"

// NormalizeUnit devuelve código catálogo SUNAT 03 válido para comprobantes.
// itemType: "product", "service" o vacío (infiera por unidad ZZ).
func NormalizeUnit(unit, itemType string) string {
	t := strings.ToLower(strings.TrimSpace(itemType))
	if t == "service" {
		return "ZZ"
	}
	u := strings.ToUpper(strings.TrimSpace(unit))
	if t == "" && u == "ZZ" {
		return "ZZ"
	}
	switch u {
	case "", "UND", "UNIDAD", "UNIDADES", "UNIT", "UNITS", "U", "UN":
		return "NIU"
	case "SERVICIO", "SERVICIOS", "SERVICE", "SRV":
		return "ZZ"
	}
	if t == "service" {
		return "ZZ"
	}
	return u
}
