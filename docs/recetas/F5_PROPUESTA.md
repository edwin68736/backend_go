# F5 — Descuento de insumos al cobrar y su reversa: diagnóstico y propuesta

Fecha: 25-nov-2026 · Estado: **PROPUESTA; no se modificó código.** Espera autorización expresa.
Marco: `PROPUESTA_INSUMOS_RECETAS_TUKICHEF.md` §9 y §20; recetas de F4 (incluidas las de presentación, que la v3.1 dejaba para V2).

Es la fase de más riesgo: **toca la venta de todos los clientes**, aunque el descuento nace apagado.

## 1. Diagnóstico (leído en el código)
| Punto | Hoy |
|---|---|
| Venta por POS rápido y Tukifac | `SaleService.Create`: valida stock, y dentro de la transacción descuenta cada línea (`VENTA/<nro>`) y los componentes de combo (agrupados, **sin saber de qué línea salieron**) |
| Cobro de mesa, para llevar y delivery | `BillTable`: **otra implementación** del mismo descuento; los componentes salen de las comandas |
| NV → comprobante | no descuenta (correcto) |
| Anulación (panel, NC total aceptada, rechazada por SUNAT) | todas pasan por `Cancel` o `CancelNotaVenta`: **solo dos puntos donde enganchar la reversa** |
| NC parcial | repone en una transacción aparte, después de la aceptación de SUNAT; si falla solo queda en el log; **no es idempotente** (defecto previo) |
| NC parcial sobre comprobante nacido de una NV | no repone stock (hueco previo) |
| Agregar pedido | `checkOrderItemStock` rechaza un plato con control de stock sin saldo |
| Idempotencia | Tukichef envía la clave en POS y en mesa |
| Cantidades de stock | 3 decimales: un insumo en kg se descuenta de a 1 g |

## 2. Qué hará
Con el descuento **encendido**, al cobrar un plato con receta salen sus insumos (según la presentación vendida, con merma y rendimiento) en la
misma transacción de la venta: o se graba todo o nada. Anular o devolver repone **lo que salió, a su costo**. Apagado, todo queda como hoy.

Reglas:
1. Un plato con receta **ya no descuenta su propio stock** (aprobado, D5). Esto aplica a la venta, a la validación previa y al agregar pedido.
2. `cantidad = redondeo a 3 decimales (vendido × cantidad servida ÷ (1 − merma) ÷ rendimiento)`, sobre la línea completa. Si da 0, se
   registra sin movimiento.
3. Combos: salen las recetas de los componentes **realmente elegidos**. Un componente sin receta sigue como hoy.
4. El costo queda guardado en la venta (`recipe_cost`); NV → comprobante lo copia. No se muestra en esta fase.
5. Reversa siempre desde el registro de consumos, aunque el descuento esté apagado. NC que no devuelve mercadería (04/05/09) no repone.
6. Bonificaciones y cortesías consumen. Un pedido anulado antes de cobrar no consume (la merma se registra a mano).
7. Extras y modificadores no consumen (V2). Ventas hechas con el descuento apagado **no se descuentan después**.

## 3. Cambios
### 3.1 Base de datos — V199 (aditiva)
- `tenant_recipe_consumptions` (venta, línea, sucursal, insumo, receta y versión, cantidades, costo, movimiento), **única** por línea +
  insumo + origen.
- `tenant_recipe_consumption_reversals`, **única** por consumo + referencia (anulación o NC), con tope: nunca se repone más de lo consumido.
- `tenant_sale_items.recipe_cost` y `recipe_cost_total` (vacío = sin receta o costo desconocido).
- Configuración: `recipes_consume` (apagado) y `recipes_stock_policy`.
- Tipos de movimiento «Consumo de receta» y «Reversa de receta».

### 3.2 Código
| Archivo | Cambio |
|---|---|
| `internal/recipes/service` | `ConsumeForSaleTx`, `ReverseForSaleTx`, `ReversePartialTx` (solo usan la transacción recibida) |
| `sales/service/sale_service.go`, `sale_combos.go` | enganche; los componentes conservan de qué línea salen |
| `restaurant/service/restaurant_service.go`, `restaurant_combos.go`, `restaurant_order_stock.go` | mismo enganche en mesa; omitir stock directo del plato con receta |
| anulación y `billing/service/fiscal_post_accept.go` | reversa total y parcial |
| restaurante: configuración | leer/encender el descuento y la política (`s.m`) |

Los insumos se bloquean **en el mismo paso** que el resto del stock, en orden estable, para no crear bloqueos mutuos.

**No cambian** importes, comprobantes, XML, caja ni SUNAT.

## 4. Entrega
- **F5a** backend (lo de arriba).
- **F5b** Tukichef: interruptor del descuento con lista de verificación previa (platos sin receta, insumos sin stock o sin costo),
  política, aviso de insumo insuficiente al pedir y porciones disponibles en el POS.

## 5. Pruebas
- **Prueba dorada:** con el descuento apagado (y en un tenant sin V199), la venta y sus movimientos son idénticos a hoy.
- Por los flujos reales: POS, mesa total/parcial/dividida, combos, presentaciones, NV → comprobante, anulación, NC parcial, total y total
  tras parciales, reintento con la misma clave, NC procesada dos veces.
- El comprobante (`payload_json`) es el mismo con y sin descuento.
- MariaDB estricto con simultaneidad: cobros a la vez del mismo insumo (saldo exacto), anulaciones a la vez, sin bloqueos mutuos.
- Propiedad: tras ventas y anulaciones al azar, consumos − reversas = variación del stock.
- Suite completa.

## 6. Riesgos
1. Un error en el descuento **impide cobrar**. Salida: apagar el descuento (efecto inmediato).
2. Con política «bloquear», el cobro falla con la comida ya servida.
3. Una receta o una unidad mal cargada descuenta mal desde el primer cobro; se corrige con anulación o ajuste, no se recalcula.
4. Cobros simultáneos de platos que comparten insumo se esperan entre sí; en producción la red lenta alargaría esa espera.
5. Defecto previo sin tocar: el cobro pide una segunda conexión a la base dentro de la transacción; con muchos cobros puede agotar el
   límite. El descuento no lo empeora en conexiones, pero sí alarga cada transacción.
6. Sigue sin respaldo: todo existe solo en esta PC. MySQL 8 (producción) no está probado en ninguna fase.

## 7. Decisiones
1. **F5a y F5b por separado.** Recomendado: sí.
2. **Quién enciende el descuento:** el administrador del restaurante, desde Tukichef, con la lista de verificación. Recomendado: sí.
3. **Insumo insuficiente:** por defecto **vender y dejar el saldo en negativo** (con aviso); «bloquear» como opción. Recomendado: sí.
4. **Combo mixto** (algún componente sin receta): el costo de esa línea queda vacío. Recomendado: sí.
5. **NC parcial sobre un comprobante que nació de una NV:** no repone insumos, igual que hoy con los productos; se documenta.
   Recomendado: sí, y corregirlo aparte para ambos.
6. **Utilidad por venta (reporte de Tukifac) con costo de receta:** dejarlo para F6; F5 solo guarda el dato. Recomendado: sí.

## 8. No verificado
Solo lectura de código. No se probó el comportamiento actual de combos con presentación, ni cuántos clientes tienen platos con control de
stock directo (a quienes les cambiaría el descuento al crear una receta y encender el consumo).
