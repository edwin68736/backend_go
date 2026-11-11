package handler

import "tukifac/pkg/salecurrency"

// Los totales de ventas del dashboard se suman en SOLES: una venta en USD aporta total × TC de la venta (las ventas en soles,
// igual que siempre). Antes se sumaban los dólares como si fueran soles.
//
//   - salesTotalPEN: sobre la tabla `tenant_sales` (cabecera de venta).
//   - saleLineTotalPEN: sobre una línea `si` de `tenant_sale_items` unida a su venta con alias `s`.
var (
	salesTotalPEN    = salecurrency.TotalPENSQL("tenant_sales", "tenant_sales.total")
	saleLineTotalPEN = salecurrency.TotalPENSQL("s", "si.total")
)

// sumSalesPENSelect devuelve la expresión Select de una suma de ventas en soles.
func sumSalesPENSelect() string { return "COALESCE(SUM(" + salesTotalPEN + "), 0)" }
