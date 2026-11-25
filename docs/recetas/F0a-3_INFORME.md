# F0a-3 — Bloqueo de saldos, orden estable y reintentos: informe de entrega

Fecha: 25-nov-2026 · Alcance autorizado: **F0a-3, solo en local**. No se desplegó, no se consultó ni modificó producción, no se inició F0b ni ninguna otra fase.

## 1. Archivos

| Archivo | Cambio |
|---|---|
| `internal/inventory/service/stock_locks.go` | **Nuevo.** Bloqueo de la fila de saldo, creación segura, pre-bloqueo ordenado, reintento, `ErrInsufficientStock`, interruptor |
| `internal/inventory/service/inventory_service.go` | `RecordMovementTx` usa la fila bloqueada; `AllowNegative` (apagado); `RecordMovement` con reintento; `EnsureProductBranchLink` seguro; pre-bloqueo en `CreateTransferWithLines`; desempate por `id` en `GetKardex` |
| `internal/inventory/service/inventory_document_service.go` | Pre-bloqueo en confirmar/anular; reintento en crear, confirmar, anular y ajuste |
| `internal/sales/service/sale_service.go`, `sale_cancel_stock.go`; `internal/restaurant/service/restaurant_service.go`; `internal/purchases/service/purchase_service.go` | **Solo pre-bloqueo ordenado** antes de mover stock (aditivo; sin cambios de lógica, sin reintento) |
| `internal/products/service/product_service.go` | `linkProductsToBranchStock` ignora el conflicto |
| `pkg/database/tenant.go` | Tiempo de espera de bloqueo **opcional** (`DB_TENANT_LOCK_WAIT_SECONDS`) |
| `internal/inventory/service/stock_locks_test.go`, `internal/sales/service/sale_stock_concurrency_test.go` | **Nuevos.** Pruebas |

## 2. Comportamiento
- **Bloqueo:** `SELECT … FOR UPDATE` sobre la fila de saldo **por su `id`**, así funciona igual en tenants sin índice único (no toma bloqueos de rango). El saldo se lee ya confirmado por quien lo tuviera.
- **Creación segura:** si no hay fila, se bloquea primero el producto (o presentación) y se crea ignorando conflictos. **Corrección respecto al diagnóstico:** sin este bloqueo del padre, un tenant sin índice todavía duplicaba filas en la primera creación simultánea (detectado en las pruebas).
- **Orden estable:** `LockStockRowsTx` bloquea por (tipo, sucursal, entidad); no crea ni cambia datos.
- **Reintento:** hasta 3 intentos ante 1213/1205, **solo** documentos de inventario, ajustes y movimientos manuales, y nunca dentro de otra transacción. Ventas, cobros, compras y traslados **no** se reintentan.
- **`AllowNegative`:** apagado; ningún llamador lo usa. El texto «stock insuficiente…» no cambia.
- **Kardex:** desempate por `id`.
- **Errores de lectura** del saldo ahora se devuelven (antes se ignoraban y se trataba como "sin fila").
- **Tiempo de espera:** sin la variable, rige el del servidor (no cambia la configuración). 5 s es el valor probado.
- **Interruptor de emergencia:** `INVENTORY_ROW_LOCKS=off` vuelve al comportamiento anterior sin migrar nada.

## 3. Pruebas

| Prueba | Resultado |
|---|---|
| Unitarias en SQLite (AllowNegative y texto, secuencia de tipos con y sin interruptor, desempate del kardex, reintento, no reintentar anidado, orden de claves) | OK |
| 7 pruebas de concurrencia en **MariaDB 10.4 estricto**, 5 s de espera, **10 repeticiones** | OK |
| 24 ventas simultáneas por el flujo real (`SaleService.Create`), 2 series, líneas en orden opuesto, 8 repeticiones | OK: stock exacto, kardex coherente, sin duplicados ni deadlocks |
| Pruebas de ventas con flujo real contra MariaDB (listado, cierre de caja, notas) | OK |
| `go test ./...` (SQLite) | 100 paquetes OK |

**Contraprueba (interruptor apagado = comportamiento actual en producción):**
- 30 salidas de 1 sobre 100: saldo **92** en vez de 70 (actualizaciones perdidas).
- 30 salidas sobre 10 unidades: **las 30 aceptadas** (sobreventa).
- Primera creación simultánea: error de clave duplicada (con índice) o filas duplicadas (sin él).
- Ventas simultáneas con líneas en orden opuesto: **deadlock 1213**.

Las pruebas MySQL borran las tablas de su BD (exigen "test" en el nombre): correrlas **por paquete**, no con `go test ./...`.

## 4. Hallazgo fuera de alcance (anterior a F0a, importante)
`SaleService.Create` y `BillTable` piden una **segunda conexión del pool** (`cashbanksvc.NewCashBankService(s.db)`) mientras tienen abierta la transacción de la venta. Con 24 ventas simultáneas y un pool de 8, la prueba terminó en `1205 Lock wait timeout` **con y sin** el bloqueo nuevo; con un pool holgado pasa. **Producción usa 6 conexiones por tenant**: un pico de ventas simultáneas de un mismo tenant puede agotarlo. Es consistente con el incidente de pool agotado ya documentado. Propongo tratarlo como tarea aparte; **no se tocó**.

## 5. No probado
- MySQL 8.0.46 (producción).
- Concurrencia de cobro de mesa, compras, anulaciones, notas de crédito y traslados (el cambio en ellos es solo el pre-bloqueo; pruebas funcionales en verde).
- Efecto en latencia con carga real: los bloqueos duran hasta el fin de la transacción, y en venta y cobro hay pagos y caja después del stock.
- Carreras propias de traslados (fuera de F0a por decisión).

## 6. Despliegue y observación (cuando lo autorices)
1. F0a-2 y F0a-3 se despliegan **juntas** (sin F0a-3, el índice produciría errores en carreras).
2. Esperar a que se resuelva la red entre los VPS: con 100 ms por consulta los bloqueos se retienen mucho más y la contención crecería. Si se despliega antes, usar `INVENTORY_ROW_LOCKS=off`.
3. Antes: `audit-stock-reconcile` en producción (solo lectura, con autorización) como línea base.
4. Observar: errores 1205/1213, `Innodb_row_lock_waits`, latencia de venta y cobro, conexiones por tenant.
5. Reversión: `INVENTORY_ROW_LOCKS=off` y reinicio.
