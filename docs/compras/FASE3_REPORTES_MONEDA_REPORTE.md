# Fase 3 — Reportes de compras por moneda: reporte (11-nov-2026)

Rama `feature/orden-compra` (sin push/deploy). Criterio: el mismo ya decidido para ventas — **no mezclar dólares con soles**; se muestra cada moneda y un consolidado en soles con el TC de cada compra.

## Qué se hizo
- **Reporte de compras** (`/reports/purchases`): `GET /api/purchases` devuelve además `totals_by_currency` (por moneda: comprobantes, subtotal, IGV, total y `total_pen`; **excluye anuladas**, respeta los mismos filtros y la sucursal). La pantalla muestra una tarjeta por moneda y, si hay más de una, el consolidado en soles. La tabla y las exportaciones PDF/Excel traen columnas **Moneda**, **TC**, importes con su símbolo y **Total en soles** (antes todo salía con "S/" fijo).
- **Retención desde una compra USD**: el prefill usa el **TC guardado de la compra** (`tipo_cambio` USD→PEN, fecha del comprobante) en vez de consultar uno por fecha, que podía diferir del real del documento. Compras en soles no cambian.
- Ya hechos en la Fase 2: dashboard, contabilidad (registro de compras, libro mayor/CxP) y Cuentas por pagar.

## Pruebas
- Go: `go test ./...` completo en verde. Nuevos: totales por moneda (PEN/USD separados, anulada excluida, otra sucursal vacía) y prefill de retención (USD con TC / PEN sin TC).
- Front: `tsc` limpio, vitest 101 ✔. En vivo local: tarjetas "Compras en soles 118.00", "Compras en dólares US$ 59.00 ≈ S/ 201.78" y consolidado S/ 319.78 (118 + 59 × 3.42).

## Decisión pendiente (no implementado): caja y movimientos de caja/bancos
Los reportes de **caja por sesión** y de **movimientos** (`cashbank`) suman importes de movimientos que **no tienen moneda** (ni en el sistema antiguo ni en las ventas en USD de Tukifac): una compra en USD pagada en efectivo resta "59" de la caja única. Con el modelo elegido (caja única, pago en la moneda del documento) es una limitación conocida, no un bug nuevo; arreglarla de verdad exige **moneda por movimiento y arqueo por moneda** (campo `currency` en `tenant_cash_movements`/`tenant_bank_movements`, totales de sesión y PDF/Excel por moneda), que es una iniciativa aparte y afecta también a las ventas en USD. Recomendación: no mezclarla aquí; mientras tanto, **pagar las compras en USD con cuentas bancarias en dólares** (o por CxP) y registrar en caja solo soles. Si se aprueba, se propone como "Caja multimoneda" con su propio diseño.

## Pendiente (menor)
- Advertencia en el formulario al elegir método/cuenta con moneda distinta a la del documento (hoy solo hay texto informativo).
- Correo de la orden de compra (Fase 1.2).
- Otros resúmenes que sumen compras no revisados exhaustivamente (p. ej. reportes de utilidad usan costos del kardex en soles: sin cambio).
