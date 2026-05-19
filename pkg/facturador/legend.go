package facturador

import "tukifac/pkg/numeroletras"

// SetSUNATLegend1000 envía la leyenda de monto en letras (catálogo 52, código 1000) en legends[].
// Lycet/Greenter la serializan como cbc:Note languageLocaleID="1000" (formato aceptado por SUNAT).
func SetSUNATLegend1000(legends *[]InvoiceLegend, mtoImpVenta float64, tipoMoneda string) {
	if legends == nil || mtoImpVenta <= 0 {
		return
	}
	*legends = []InvoiceLegend{{
		Code:  "1000",
		Value: numeroletras.MontoEnLetras(mtoImpVenta, tipoMoneda),
	}}
}
