# Implementación: TC guardado todos los días + operaciones con su TC + reportes de ventas y caja con valor original y equivalente en soles (11-nov-2026)

Implementa lo pedido: «el tipo de cambio siempre debe guardarse todos los días y las operaciones se guardan con ese tipo de cambio»; y los reportes de ventas y de caja con el valor original por moneda y, al final, la conversión a soles de todo. Propuesta de origen: `PROPUESTA_REPORTES_VENTAS_Y_CAJA_MONEDA.md`.

## 1. TC guardado todos los días
- El módulo `exchangerate` ya guardaba cada día consultado (confirmado por SUNAT o propagado del último confirmado). Faltaba **garantizar un registro por cada día** aunque nadie consulte (servidor caído, fines de semana, proveedor sin respuesta).
- Nuevo `EnsureRecentDays(n)`: completa los días sin TC de los últimos n días y hoy (idempotente; no reconsulta los ya guardados).
- Cron: al arrancar completa los últimos 45 días; **a las 00:10 (Lima)** completa los últimos 5 (el día nuevo ya tiene fila antes de la primera operación); a las 09:05 el prefetch existente lo confirma con el TC publicado. Un solo nodo a la vez (`cronlock`).

## 2. Las operaciones se guardan con ese TC
- Nuevo `pkg/dailyfx` (acceso al TC guardado del día) y `salecurrency.ResolveDocumentRate`: toda operación **en USD** queda registrada con el TC; si el cliente no lo envía, el **servidor lo completa con el TC guardado del día de emisión** (hora de Lima); si el usuario lo edita, se respeta; sin ningún TC se rechaza con mensaje claro. En soles no lleva TC.
- Aplicado a **ventas** y **cotizaciones** (en `main`). *(Compras y órdenes de compra: se aplica al fusionar la rama `feature/orden-compra`; ver §5.)*
- Antes una venta en USD podía quedar con `exchange_rate = NULL` (2 de las 14 de producción).

## 3. Reporte de ventas (`/reports/sales`)
- Backend: cada venta del listado trae `total_pen` (equivalente en soles), `rate_effective` y `rate_estimated`; el resumen trae `sum_active_pen` (consolidado), por moneda y los conteos de ventas con TC estimado / sin TC. Las ventas en USD sin TC guardado usan el TC del día de emisión (`*`); sin ninguno, quedan fuera del consolidado y se avisa.
- Pantalla, PDF y Excel: columnas **Moneda | Total facturado (valor original) | TC | Equivalente S/**; filas de total por moneda (como ya existían) y, con dólares, la fila final **TOTAL CONSOLIDADO EN SOLES**; tarjeta «Total consolidado en soles». Las anuladas no suman y las notas de crédito/débito no suman (reglas de siempre).

## 4. Reporte de caja (`/reports/cash`)
- Backend: cada movimiento trae `currency`, `exchange_rate`, `amount_pen`, `rate_estimated`, `rate_missing`, derivados de la **venta o compra de origen** (los manuales son en soles). El resumen trae totales por moneda, el consolidado en soles y los avisos de TC. **El saldo físico de caja se calcula en soles** (antes sumaba montos en dólares como soles).
- Pantalla, PDF y Excel: columnas **Moneda | Monto (original) | TC | Equivalente S/**, tarjetas de ingresos/egresos/neto en soles con el desglose por moneda y filas finales de neto por moneda + **TOTAL NETO EN SOLES**.

## 5. Pendiente / siguiente
- **Compras y órdenes**: en la rama `feature/orden-compra` aplicar `ResolveDocumentRate` y usar el TC guardado de la compra en el reporte de caja (hoy en `main` las compras no guardan TC, por eso aparecen con TC estimado `*`).
- **Cierre/arqueo de sesión de caja** (`CashReportsPage`, `GetSessionReport`) y su PDF: siguen sumando montos sin convertir (paso 3 de la propuesta).
- Snapshot del equivalente en cada movimiento (migración V182) — Fase 3 del programa multimoneda; los reportes no cambian de aspecto.
- Dashboard y Cuentas por cobrar con el mismo criterio.

## 6. Pruebas
- Go: tests nuevos de `ResolveDocumentRate` (completa con el TC del día, respeta el del usuario, día de Lima, sin TC falla, sin proveedor conserva el comportamiento anterior), `EnsureRecentDays` (completa todos los días; con el proveedor caído propaga el último confirmado), listado de ventas con equivalente por fila y consolidado (TC propio, TC del día, sin TC, anulada), movimientos de caja con equivalente, saldo físico en soles y totales por moneda. `go test ./...` completo en verde; front `tsc` limpio y vitest 101 ✔.
- En vivo (tenant demo local): reporte de ventas con `US$ 27.00 · TC 4.000 · S/ 108.00`, tarjeta «Total consolidado en soles S/ 795.55» (= 687.55 + 27 × 4); reporte de caja con ingresos `S/ 722.39` (`S/ 607.55 + US$ 29.00`), neto con aviso de TC estimado y columnas Moneda/Monto original/TC/Equivalente.
