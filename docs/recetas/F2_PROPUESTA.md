# F2 — Costo promedio de insumos: diagnóstico y propuesta

Fecha: 25-nov-2026 · Estado: **PROPUESTA; no se modificó código.** Espera autorización expresa.
Marco: `PROPUESTA_INSUMOS_RECETAS_TUKICHEF.md` §5, §20.2 y §20.8; `F0a_DIAGNOSTICO_Y_PROPUESTA.md` §3. Basado en lectura del código.

## 1. Qué verá el usuario
- En **Insumos**: «Costo promedio» (por sucursal) además del «Último costo».
- En el kardex del insumo: las salidas (traslado, egreso, merma) con su valor, no en cero.
- Los productos normales **no cambian en nada**.

No incluye: compras desde Tukichef ni carga inicial (F3), recetas (F4), descuento al cobrar y reversas de venta (F5). Como un insumo no
se puede vender, F2 no toca el motor de ventas.

## 2. Estado actual
1. No hay promedio guardado. `WeightedAverageUnitCosts` lo recalcula leyendo todo el kardex, solo para el ajuste por Excel, y si no hay
   costos usa `purchase_price`.
2. La compra pasa al kardex el costo **tal como se digitó** (con o sin IGV) y lo copia en `purchase_price`.
3. Salidas, traslados y ajustes mandan costo 0. La entrada de un ingreso usa el costo de la línea.
4. La anulación de compra sale al costo original (correcto) pero no habría cómo devolver el promedio anterior.
5. Anular un documento de inventario usa el costo del detalle, no el del movimiento.
6. **Traslados (afecta a todos los productos, no solo insumos):** `Transfer` hace salida y entrada en dos transacciones; confirmar y
   cancelar leen el estado fuera de la transacción. Dos confirmaciones a la vez suman dos veces al destino; confirmar y cancelar a la vez
   duplica stock. Detectado leyendo el código; no reproducido.
7. Los costos viajan sin filtro en `GET /inventory/movements` (lo puede leer un mozo o cocinero), en documentos de inventario y en compras.

## 3. Reglas (solo insumos, por producto y sucursal)
`Q` saldo antes, `A` promedio (vacío = nunca hubo costo), `q` cantidad, `c` costo de la entrada.

| Movimiento | Efecto en `A` | Costo que queda en el movimiento |
|---|---|---|
| Compra | `A' = (max(Q,0)·A + q·c) / (max(Q,0) + q)`; si `Q ≤ 0` o `A` vacío, `A' = c` | `c` |
| Ingreso con costo > 0 | igual que la compra | `c` |
| Entrada sin costo (ajuste, Excel) | no cambia | `A` |
| Salida (egreso, merma, traslado) | no cambia | `A` (ignora lo digitado) |
| Ajuste a un saldo fijo | no cambia | `A` |
| Anular compra | vuelve al `A` de antes si no hubo otra entrada con costo después; si la hubo, no cambia | el original |
| Anular documento | como la operación inversa, con el costo del movimiento original | el original |
| Traslado: crear | no cambia en origen | `A` del origen, congelado |
| Traslado: confirmar / cancelar | pondera en destino / origen con el costo congelado | el congelado |

Sin `A`, la salida queda en 0 **marcada como costo desconocido**. Anular una compra ya consumida sigue fallando por falta de stock.

## 4. Cambios

### 4.1 Base de datos (migración V196, aditiva)
| Columna | Tipo |
|---|---|
| `tenant_product_stocks.avg_unit_cost` | `decimal(15,6)`, admite vacío |
| `tenant_stock_movements.avg_before` | `decimal(15,6)`, admite vacío |
| `tenant_stock_movements.cost_unknown` | bool, por defecto `false` |
| `tenant_transfer_logs.unit_cost` | `decimal(15,6)`, admite vacío |

- Las cuatro serán **de solo lectura para GORM** y se escriben aparte, solo para insumos. Si fueran campos normales, un tenant sin migrar
  no podría registrar **ningún** movimiento de stock, ventas incluidas.
- La migración recalcula el promedio de los insumos que ya tengan movimientos (hoy solo existen en la BD local).

### 4.2 Código
- `internal/inventory/service`: el cálculo dentro de `RecordMovementTx`, bajo el bloqueo de fila de F0a; transferencias.
- `internal/purchases/service`: costo neto para insumos y restauración al anular.
- `inventory_document_service.go`, `inventory_import_adjustment.go`: reglas de §3 (el Excel deja de imponer su promedio calculado fuera
  de la transacción).
- `pkg/middleware`: ocultar costos de insumos en los endpoints antiguos.
- Tukichef: mostrar «Costo promedio» y «Último costo». Tukifac: sin cambios de pantalla.
- Cálculo con `math/big` de la librería estándar, sin dependencias nuevas; se redondea a 6 decimales solo al guardar.

## 5. Entrega en dos partes
| Parte | Contenido | Alcance |
|---|---|---|
| **F2a** | Traslados atómicos, sin doble confirmación ni cancelación | **Todos los tenants**; sin migración; se puede desplegar sola |
| **F2b** | Costo promedio (V196, reglas, permisos de costo, pantalla) | Solo insumos |

F2a empieza reproduciendo el defecto en MariaDB; si no se reproduce, te lo informo antes de cambiar nada.

## 6. Pruebas
- Una prueba por cada fila de §3, con los casos límite: saldo negativo, entrada sin costo, saldo en cero, comprar-anular,
  comprar-comprar-anular la primera, compra en saco, en USD, con y sin IGV.
- Traslados: doble confirmación y confirmar contra cancelar en simultáneo.
- **Prueba dorada:** movimientos y saldos de productos normales idénticos a hoy (compra, venta, anulación, traslado, ajuste).
- Tenant sin migrar: todo funciona como antes.
- SQLite y MariaDB estricto. MySQL 8 sigue sin probarse mientras no haya una instancia.

## 7. Riesgos
- `RecordMovementTx` es el paso por donde pasa **todo** el stock: un error ahí afecta ventas. Se mitiga dejando la rama de insumos
  aparte, con la prueba dorada.
- El promedio no se reescribe hacia atrás al anular una compra antigua: es una aproximación documentada.
- Un insumo sin ninguna entrada con costo se muestra sin promedio.
- **Orden de despliegue:** nada de F0a a F1b está desplegado. F2 se apila encima; conviene no seguir acumulando sin desplegar al menos
  F0a, porque cada fase hace más grande la primera salida a producción.

## 8. Decisiones que necesito
1. **Separar F2a (traslados) de F2b (costo).** Recomendado: sí.
2. **Mantener `decimal(15,6)`** y no pasar a `(18,8)`. Con costos reales por gramo (arroz a S/ 3.50 el kilo = 0.0035) sobran cifras, y
   cambiar el tipo obligaría a reescribir la tabla de kardex en cientos de empresas. Recomendado: sí, mantener.
3. **Costo sin IGV** para insumos (promedio y último costo); en empresas del **Nuevo RUS**, que no recuperan el IGV, con IGV.
   Recomendado: así. Esto agrega la excepción del RUS a lo aprobado en la v3.1.
4. **El costo escrito a mano en Insumos no crea promedio**: queda como «Último costo»; el promedio nace con la primera entrada.
   Recomendado: sí.
5. **Quién ve costos de insumos en las pantallas antiguas:** solo quien tiene `products.view_cost` (Tukifac) o `rc.c` (Tukichef).
   Recomendado: sí.

## 9. No verificado
No se ejecutó nada. El defecto de traslados se dedujo del código. No se revisó qué pantallas de Tukifac usan el traslado directo y
cuáles el de crear y confirmar. No se midió el costo de recalcular el promedio en la migración (en producción no hay insumos).
