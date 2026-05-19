package numeroletras

import (
	"fmt"
	"math"
	"strings"
)

// MontoEnLetras devuelve el monto en texto para la leyenda SUNAT (código 1000).
// Formato según comprobantes aceptados por SUNAT: "VEINTE CON 00/100 SOLES".
func MontoEnLetras(monto float64, tipoMoneda string) string {
	monto = math.Round(monto*100) / 100
	if monto < 0 {
		monto = 0
	}
	entero := int64(monto)
	centavos := int64(math.Round((monto - float64(entero)) * 100))
	if centavos < 0 {
		centavos = 0
	}
	if centavos > 99 {
		entero += centavos / 100
		centavos = centavos % 100
	}
	parteEntera := NumeroALetras(entero)
	if parteEntera == "" {
		parteEntera = "CERO"
	}
	return fmt.Sprintf("%s CON %02d/100 %s", strings.TrimSpace(parteEntera), centavos, nombreMoneda(tipoMoneda))
}

func nombreMoneda(tipoMoneda string) string {
	switch strings.ToUpper(strings.TrimSpace(tipoMoneda)) {
	case "USD":
		return "DOLARES AMERICANOS"
	case "EUR":
		return "EUROS"
	default:
		return "SOLES"
	}
}

var (
	unidades = []string{"", "UNO", "DOS", "TRES", "CUATRO", "CINCO", "SEIS", "SIETE", "OCHO", "NUEVE"}
	diez     = []string{"", "DIEZ", "VEINTE", "TREINTA", "CUARENTA", "CINCUENTA", "SESENTA", "SETENTA", "OCHENTA", "NOVENTA"}
	especial = []string{"DIEZ", "ONCE", "DOCE", "TRECE", "CATORCE", "QUINCE", "DIECISÉIS", "DIECISIETE", "DIECIOCHO", "DIECINUEVE"}
	veinti   = []string{"", "VEINTIUNO", "VEINTIDÓS", "VEINTITRÉS", "VEINTICUATRO", "VEINTICINCO", "VEINTISÉIS", "VEINTISIETE", "VEINTIOCHO", "VEINTINUEVE"}
	cientos  = []string{"", "CIENTO", "DOSCIENTOS", "TRESCIENTOS", "CUATROCIENTOS", "QUINIENTOS", "SEISCIENTOS", "SETECIENTOS", "OCHOCIENTOS", "NOVECIENTOS"}
)

// NumeroALetras convierte un entero (0 a 999.999.999) a letras en español.
func NumeroALetras(n int64) string {
	if n < 0 {
		n = -n
	}
	if n == 0 {
		return "CERO"
	}
	if n > 999999999 {
		return "NOVECIENTOS NOVENTA Y NUEVE MILLONES NOVECIENTOS NOVENTA Y NUEVE MIL NOVECIENTOS NOVENTA Y NUEVE"
	}
	var parts []string
	// Millones
	if n >= 1000000 {
		mill := n / 1000000
		n = n % 1000000
		if mill == 1 {
			parts = append(parts, "UN MILLÓN")
		} else {
			parts = append(parts, centenasMillones(int(mill)), "MILLONES")
		}
	}
	// Miles
	if n >= 1000 {
		mil := n / 1000
		n = n % 1000
		if mil == 1 {
			parts = append(parts, "MIL")
		} else {
			parts = append(parts, centenasMil(int(mil)), "MIL")
		}
	}
	// Unidades
	if n > 0 {
		parts = append(parts, centenas(int(n)))
	}
	return strings.TrimSpace(strings.Join(parts, " "))
}

func centenas(n int) string {
	if n == 0 {
		return ""
	}
	if n == 100 {
		return "CIEN"
	}
	if n < 100 {
		return decenas(n)
	}
	c := n / 100
	r := n % 100
	if r == 0 {
		return cientos[c]
	}
	return strings.TrimSpace(cientos[c] + " " + decenas(r))
}

func decenas(n int) string {
	if n < 10 {
		return unidades[n]
	}
	if n < 20 {
		return especial[n-10]
	}
	if n == 20 {
		return "VEINTE"
	}
	if n < 30 {
		return veinti[n-20]
	}
	d := n / 10
	u := n % 10
	if u == 0 {
		return diez[d]
	}
	return diez[d] + " Y " + unidades[u]
}

// centenasMil: para miles usar "UN" en 1 (UN MIL), "VEINTIÚN" en 21, etc.
func centenasMil(n int) string {
	if n == 1 {
		return "UN"
	}
	return centenas(n)
}

func centenasMillones(n int) string {
	return centenas(n)
}
