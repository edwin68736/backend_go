# F6: reportes de insumos y recetas (propuesta)

27-nov-2026. **Pendiente de aprobación; no hay código.**

## 1. Cómo está hoy (revisado en la rama)

- Tukichef ya tiene un marco de reportes (filtros, paginado, Excel y PDF) y la página «Reportes» solo la ven administrador y
  supervisor. Se reutiliza; cada reporte nuevo es una definición más.
- No existe ningún endpoint de reportes en `/api/restaurant`.
- Los datos ya se guardan: consumos y reposiciones (`tenant_recipe_consumptions`), costo de receta por línea vendida
  (`recipe_cost`, `recipe_cost_total`), mermas y desperdicios como movimientos de inventario (`SHRINKAGE`, `WASTE`) y compras
  de insumos.
- **El reporte de utilidad de Tukifac (`/api/sales/profit-detail`) no usa `recipe_cost`:** toma `purchase_price`, que en un
  plato preparado suele ser 0. Hoy mostraría como ganancia casi todo el precio de un plato con receta.
- Ese reporte excluye las notas de crédito, así que una devolución parcial no le resta nada (ya era así antes del módulo).
- `tenant_recipe_consumptions` no tiene índice por sucursal y fecha.

## 2. Qué se entrega

Los nueve temas de la propuesta original, agrupados en seis reportes para no llenar la pantalla de pestañas.

| # | Reporte | Columnas | Cubre |
|---|---|---|---|
| R1 | Existencias de insumos | stock, mínimo, costo promedio, **valor**, último costo, aviso (bajo mínimo o negativo); total valorizado | existencias y valorización |
| R2 | Compras de insumos | fecha, proveedor, comprobante, insumo, cantidad en unidad base, costo unitario, importe; sin compras anuladas | historial de compras |
| R3 | Consumo de insumos | insumo, cantidad consumida (menos lo repuesto), costo, consumos sin costo; ordenado por mayor consumo | consumo e insumos de mayor consumo |
| R4 | Mermas y desperdicios | fecha, insumo, tipo, cantidad, valor, usuario, motivo | mermas |
| R5 | Costos de la carta (hoy) | plato, costo por porción, precio sin IGV, food cost, margen, porciones disponibles, incompleto | costo por plato, mayor costo, food cost, disponibilidad |
| R6 | Rentabilidad de platos vendidos (período) | plato, unidades netas, venta sin IGV, costo de receta, margen, food cost real, líneas sin costo | margen y food cost histórico |

Además, en Tukifac: el reporte de utilidad usa `COALESCE(recipe_cost, purchase_price, costo del producto)`.

## 3. Reglas de cálculo

- **Costos:** los de insumos van sin IGV. El margen de R5 y R6 se calcula sobre el precio **sin IGV**, igual que la pantalla de
  recetas.
- **Costo desconocido no es cero:** una línea sin costo se cuenta aparte y queda fuera del porcentaje de margen.
- **R3 y R6, anulaciones y notas de crédito:** una venta anulada no cuenta; en una devolución parcial se restan las unidades
  devueltas y lo que se repuso.
- **R6, nota de venta convertida en factura o boleta:** cuenta una sola vez, con la regla de fecha de emisión que ya usan los
  reportes de ventas (`pkg/salescope`).
- **R6, combos:** tienen costo solo si todos sus componentes tienen receta; si no, salen como «sin costo».
- **R6, ventas en dólares:** se convierten a soles con el tipo de cambio de la venta, como el reporte de utilidad actual.
- **R1, stock negativo:** se muestra y se marca; no resta del total valorizado.
- **Merma de la receta** (el porcentaje por ingrediente) ya está dentro del consumo (R3). R4 solo muestra mermas y desperdicios
  registrados a mano.
- **R6 empieza cuando se encendió el descuento:** las ventas anteriores no tienen costo de receta.

## 4. Permisos

- Las rutas nuevas exigen el módulo activo. Sucursal validada en el servidor.
- R1 a R4: permiso de insumos; sin `recipes.view_cost` no viajan costos ni valores.
- R5 y R6: `recipes.view_cost`.
- En Tukichef la página «Reportes» sigue siendo de administrador y supervisor.

## 5. Archivos

- Backend: `internal/restaurant/service/restaurant_reports*.go`, su handler y rutas `GET /api/restaurant/reports/...`;
  `internal/sales/service/sale_service.go` (`ProfitDetail`); migración **V200** con un índice `(branch_id, created_at)` en
  `tenant_recipe_consumptions`.
- Tukichef: `src/reports/definitions/` (seis definiciones), `src/reports/registry`, `src/services/`.

## 6. Riesgos

- **Consultas pesadas:** producción ya tuvo episodios de lentitud. Se agrega en la base de datos (sin traer filas para sumarlas
  en memoria), se pagina, el rango de fechas se limita a un año y no se hacen consultas por fila.
- **Índice nuevo:** la tabla de consumos nace vacía con el módulo, así que crearlo no bloquea datos existentes.
- **Reporte de utilidad:** es el único cambio que toca algo que hoy usa todo el mundo. Para productos sin receta
  (`recipe_cost` vacío) el resultado debe ser idéntico; se fija con una prueba.
- **Cifras que no cuadran:** R3 debe sumar lo mismo que el kardex; R6, lo mismo que las ventas del período.

## 7. Fases

1. **F6a, backend:** endpoints, V200 y utilidad. Pruebas con ventas, anulaciones, notas de crédito parciales y conversión de
   nota de venta por el flujo real; cuadre contra kardex y ventas; permisos y aislamiento por sucursal; MySQL estricto; tiempos
   con un volumen grande sembrado.
2. **F6b, Tukichef:** las seis pantallas con Excel y PDF, probadas en pantalla en local.

Cada una con su informe y commit propio, en la rama `insumos-recetas`.

## 8. Fuera de F6

Consumo teórico contra real (necesita conteos de inventario, V2), gráficos, consolidado de todas las sucursales y programación de
envíos.

## 9. Decisiones que necesito

1. **Los seis reportes** como están en la tabla. Recomendado: sí.
2. **Margen sobre precio sin IGV** en R5 y R6. Recomendado: sí.
3. **Utilidad de Tukifac:** usar el costo de receta. Recomendado: sí.
4. **Quién ve la utilidad de Tukifac:** hoy cualquiera que pueda ver ventas. Recomendado: exigir el permiso de ver costos que ya
   existe (`products.view_cost`). Quien no lo tenga dejará de ver ese reporte.
5. **Sucursal:** una a la vez, elegible en el filtro, sin consolidado. Recomendado: sí.

Nota: el reporte de utilidad de Tukifac compara el precio de la línea (con IGV si el precio lo incluye) contra el costo. F6 no
cambia esa forma de calcular; solo de dónde sale el costo de un plato.
