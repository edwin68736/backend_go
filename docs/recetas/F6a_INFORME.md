# F6a: reportes en el backend (informe)

27-nov-2026. Solo backend. Sin push ni despliegue.

## Qué se hizo

| Ruta (`GET /api/restaurant/...`) | Reporte | Permiso |
|---|---|---|
| `/reports/supply-stock` | R1 existencias y valor | insumos (costos solo con permiso de verlos) |
| `/reports/supply-purchases?from&to` | R2 compras, en unidad base y sin IGV | insumos |
| `/reports/supply-consumption?from&to` | R3 consumo neto, ordenado por mayor costo | insumos |
| `/reports/supply-waste?from&to` | R4 mermas y desperdicios | insumos |
| `/reports/dish-profitability?from&to` | R6 rentabilidad de platos vendidos | ver costos |
| `/recipes` (ya existía) | R5 costos de la carta | recetas |

- Siempre de una sucursal. Período obligatorio y de un año como máximo. R2 y R4 paginan.
- Utilidad de Tukifac (`/api/sales/profit-detail`): un plato con receta usa el costo de sus insumos al venderse.
- **V200**: índices en las tablas de consumos y reposiciones.
- Las reglas de nota de crédito pasaron a `pkg/sunatnote` (facturación usa las mismas funciones).

## Cambio respecto de lo aprobado (decisión 4)

La propuesta decía que la utilidad era visible para cualquiera que viera ventas y recomendaba exigir «ver costos». Era inexacto: la
**pantalla** ya exige `reports.profit`; lo abierto era la ruta del servidor. Se hizo que el servidor exija **`reports.profit`**: nadie
pierde lo que hoy ve. Exigir «ver costos» habría quitado el reporte a roles que tienen el permiso de utilidades.

## Reglas de cálculo

- Rentabilidad: ventas vigentes emitidas en el período (una nota de venta convertida cuenta una vez); sin anuladas; venta sin IGV y en
  soles. Las notas de crédito parciales restan solo cuando están aceptadas; las de precio no devuelven unidades; las rebajas por
  importe se atribuyen al plato. Lo vendido sin costo se muestra aparte y no entra al margen.
- Consumo: lo que salió y lo que se repuso **en el período**; cuadra con el kardex.
- Mermas: salidas por merma o desmedro; las de documentos anulados no cuentan.

## Probado

- Flujo real (POS, nota de crédito aceptada y pendiente, anulación, emisión desde nota de venta, combo, plato sin receta) con cifras
  calculadas a mano; consumo contra el kardex insumo por insumo; utilidad.
- Rutas: compras (con una anulada), merma y desperdicio (con uno anulado), paginado, valor de existencias, permisos, fechas, costos
  ocultos, índices de V200 repetibles, permiso de utilidad.
- **MySQL 8.0.30 con el `sql_mode` de producción** (incluye `ONLY_FULL_GROUP_BY`): todo el módulo pasa. En ventas, una prueba de
  anulación falla si comparte base con otra; sola pasa (ya conocido).
- Suite completa en SQLite: fallan las 6 pruebas que leen el facturador desde una ruta relativa (la copia temporal no lo tiene) y
  `pkg/saas`, excluido como siempre.

## Encontrado al probar

- Una consulta nueva fallaba con `ONLY_FULL_GROUP_BY`. La receta de pruebas del plan fijaba un `sql_mode` que lo desactivaba:
  **desde ahora hay que usar el de producción** (corregido en el plan).
- Dos consultas recorrían tablas completas; corregidas.

## Tiempos (MySQL local; un año sembrado: 120 mil líneas vendidas, 80 mil movimientos)

| Reporte | Un mes | Un año |
|---|---|---|
| Rentabilidad | 0,39 s | 2,5 s |
| Consumo | 0,03 s | 0,25 s |
| Mermas | 0,02 s | <0,01 s |
| Utilidad de Tukifac | 0,25 s | no medido |

La rentabilidad de un año lee todas las líneas, como los reportes de ventas actuales. En producción, con latencia entre servidores,
será más lento. En F6b el período por defecto debe ser el mes.

## No probado

- MySQL 8.0.46, la migración V200 sobre empresas reales, nada en pantalla (F6b), tiempos en producción.
- Ventas en dólares en el reporte de rentabilidad (se reutiliza la conversión de la utilidad, sin prueba propia).

## Pendiente

F6b: seis pantallas en Tukichef con Excel y PDF.
