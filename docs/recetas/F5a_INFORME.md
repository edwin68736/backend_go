# F5a — Descuento de insumos al vender (backend): informe

Fecha: 26-nov-2026 · Rama `insumos-recetas` · Sin desplegar. Propuesta: `F5_PROPUESTA.md` (decisiones aceptadas como se recomiendan).

## Qué hace
El descuento **nace apagado**. Con el módulo activado (superadmin) y el descuento encendido (administrador del restaurante), al cobrar un
plato con receta salen sus insumos dentro de la misma transacción de la venta, y anular o devolver los repone desde el registro de
consumos, a lo que costaron. Apagado, la venta queda como antes.

- Venta rápida y «Nueva venta» (`SaleService.Create`), cobro de mesa, para llevar y delivery (`BillTable`).
- Un plato con receta no descuenta ni exige su propio stock (venta, validación previa y agregar pedido).
- Combos: salen las recetas de los componentes elegidos; el costo de la línea queda vacío si alguno no tiene receta.
- Costo en `tenant_sale_items.recipe_cost` / `recipe_cost_total` (no viaja en ninguna respuesta de la API todavía). El comprobante emitido
  desde una nota de venta lo copia y no vuelve a consumir.
- Anulación de nota de venta o comprobante: repone todo lo pendiente. Nota de crédito parcial: repone en proporción, una sola vez por
  nota, y varias notas que devuelven toda la línea reponen exactamente lo consumido. Motivos 04/05/09 no reponen.
- Insumo insuficiente: por defecto vende y queda en negativo; con «bloquear», el cobro falla nombrando plato e insumo.

## Configuración
`GET` / `PUT /api/restaurant/recipe-consumption` → `{"consume": bool, "stock_policy": "allow" | "block"}`. Requiere el permiso `s.m`
(configuración del restaurante). Emergencia: variable de entorno `RECIPES_CONSUMPTION=off` (hay que recrear el contenedor); apagar el
descuento desde la configuración tiene efecto inmediato.

## Migración V199 (aditiva, repetible)
Tipos de movimiento «Consumo de receta» (SUNAT 10, salida a producción) y «Reversa de receta» (99); `recipes_consume` y
`recipes_stock_policy`; `recipe_cost` y `recipe_cost_total`; tablas `tenant_recipe_consumptions` y
`tenant_recipe_consumption_reversals` (únicas).

## Decisiones tomadas al implementar
- Los movimientos usan la referencia `RECETA/<número>` (y `REVERSA RECETA/<número>`), no `VENTA/`: así la reversa de stock que ya existía
  no los toca.
- El costo es el promedio de la sucursal al momento de vender, aunque la configuración de recetas use «último costo» para sugerir precios.
- La reversa funciona aunque el descuento se haya apagado o la receta haya cambiado.

## Probado
Pruebas por los flujos reales en `internal/billing/service/recipe_consumption_flow_test.go`: apagado (3 formas), venta rápida, venta como
en Tukifac con unidad de venta, mesa dividida, combo, presentaciones, nota de venta → boleta, anulación, nota de crédito parcial repetida,
parcial + total, descuento sin devolución, insumo insuficiente, agregar pedido, ventas y anulaciones al azar, una sola conexión y el
documento enviado a un facturador de prueba (**idéntico con y sin descuento**). Además `TestRecipeConsumptionSettings` (permisos).

- SQLite: batería completa sin `pkg/saas`. Fallan 7 que ya fallaban (6 `TestGreenterXML_*` en copia temporal y
  `TestRunUserRoleMigration_ConcurrentRuns_OnlyOneExecutes`).
- MySQL 8.0.30 local, estricto: inventario, restaurante, insumos, compras, cuentas por cobrar y pagar y las pruebas nuevas, incluidas
  12 ventas a la vez con 3 conexiones y cada una anulada dos veces a la vez. En `sales/service` fallan dos de anulación si comparten la
  base (pasan solas en una base limpia, como antes).
- Se actualizaron a 22 los conteos de tipos de movimiento en las pruebas de V083–V085.

## No probado
- MySQL 8.0.46 (producción), MariaDB, y la migración sobre empresas reales.
- Nada en pantalla: faltan las pantallas (F5b).
- Envío a SUNAT (beta o producción): solo se comparó el documento enviado a un facturador de prueba.
- Tienda en línea y conversión de cotizaciones (usan `SaleService.Create`, sin prueba propia).
- Cuánto alarga el cobro en producción, con la latencia entre servidores: cada insumo agrega varias consultas dentro de la transacción.

## A tener en cuenta
- Apagado, cada venta y cobro hace una consulta más (leer la configuración).
- Las pantallas actuales pueden seguir mostrando o bloqueando por el stock del plato; se resuelve en F5b.
- Una nota de devolución total emitida **sin elegir ítems** no repone productos hoy (leído en el código, no probado); tampoco insumos.
- Pendiente aparte: la segunda conexión en la emisión de la nota de crédito.
