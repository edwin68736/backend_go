# F0a — Endurecer inventario: diagnóstico y propuesta

Fecha: 25-nov-2026 · Estado: **DIAGNÓSTICO Y PROPUESTA; no se implementó nada** (ni código, ni migraciones, ni BD, ni producción). Espera autorización expresa del dueño.
Marco: `PROPUESTA_INSUMOS_RECETAS_TUKICHEF.md` v3.1 (§15, §20). `[V]` verificado leyendo código · `[I]` inferido / conocimiento de InnoDB sin comprobar contra una BD real.

---

## 1. Alcance exacto de F0a (sin ampliarlo)

**Entra en F0a**
1. Bloqueo de fila del saldo (`SELECT … FOR UPDATE`) en `RecordMovementTx`, con creación segura de la fila y **orden estable** de bloqueo.
2. `UNIQUE (product_id, branch_id)` en `tenant_product_stocks` y `UNIQUE (presentation_id, branch_id)` en `tenant_product_presentation_stocks`, previa detección de duplicados.
3. Reintento por deadlock / lock wait **solo** en operaciones de inventario autocontenidas (ver §6, decisión 4).
4. Opción `AllowNegative` en `MovementInput` (apagada por defecto) y error sentinela `ErrInsufficientStock` con el **mismo texto** actual.
5. `tenant_inventory_document_details.unit_cost` de `decimal(15,2)` a `decimal(15,6)` (nullable).
6. Reconciliador de **solo lectura** stock ↔ kardex.
7. Desempate por `id` en el orden del kardex (`GetKardex`).

**NO entra en F0a** (se documenta y se asigna a otra fase)
- Costo promedio `avg_unit_cost`, `CostMode`, `MovementResult`, columnas `cost_unknown` / `avg_before`, `unit_cost` en salidas, costo sin IGV, costo en traslados → **F2**.
- Cobro de mesa y anulación condicional → **F0b** (por separado).
- Carreras propias de traslados (`ConfirmTransfer`/`CancelTransfer`, `Transfer` en dos transacciones) → **decisión del dueño** (§6, decisión 7); no se tocan sin aprobación.
- Código muerto `adjustmentInWithSerials` / `adjustmentOutWithSerials` → no se toca (solo se documenta).
- Nada de `is_supply`, recetas, ledger ni permisos de costo.

---

## 2. Archivos y componentes que se modificarían

| Archivo / componente | Cambio |
|---|---|
| `internal/inventory/service/inventory_service.go` | `RecordMovementTx` (lectura con `FOR UPDATE`, creación segura de fila, `AllowNegative`, sentinela); `EnsureProductBranchLink` (inserción segura); orden de `GetKardex` (`id` como desempate) |
| Nuevo helper `LockStockRowsTx(tx, claves)` en `internal/inventory/service/` | Pre-bloqueo ordenado por `(tipo, branch_id, product_id o presentation_id)` |
| Nuevo helper de reintento (en `pkg/database` o paquete propio) | Reconoce 1213 y 1205 (`*mysql.MySQLError`) y reintenta la transacción externa **solo** en inventario |
| `internal/inventory/service/inventory_document_service.go` | Pre-bloqueo de las líneas del documento; envolver confirmar/anular con reintento |
| `internal/inventory/service/inventory_service.go` (transferencias) | Solo pre-bloqueo ordenado en `CreateTransferWithLines`; **sin** corregir carreras de estado (decisión 7) |
| `internal/sales/service/sale_service.go` (bucle de stock ~960) y `internal/restaurant/service/restaurant_service.go` (~1540) / `restaurant_combos.go` | **Solo** una llamada al pre-bloqueo ordenado antes del bucle (cambio aditivo; requiere tu OK, decisión 5). Sin reintento de la transacción de venta |
| `internal/purchases/service/purchase_service.go`, `sales/service/sale_cancel_stock.go` | Misma llamada de pre-bloqueo (aditiva) |
| `pkg/database/migrations.go` | Tags UNIQUE en los dos structs de stock; `decimal(15,6)` en el detalle de documento |
| `pkg/database/tenantmigrations/` | **V193** (UNIQUE con detección: si hay duplicados, log y omite, estilo V052) y **V194** (`unit_cost`); registro en `registry.go` |
| `internal/products/service/product_service.go` (`linkProductsToBranchStock`) | Inserción segura (sin carrera de doble creación) |
| Nuevo comando `pkg/cmd/audit_stock_reconcile.go` + `cli.go` | Reconciliador y detector de duplicados, solo lectura |
| Tests nuevos | Concurrencia MySQL, migraciones V193/V194, reconciliador, regresión |

---

## 3. Hallazgos de la revisión técnica pedida (puntos 1 a 5)

Columna final: **F0a** = relevante a esta fase · **v3.1** = obliga a actualizar el documento (hecho en §20.8) · **Otra fase** = se resuelve en la fase indicada.

### 3.1 Costo promedio y saldos negativos frente a cada tipo de movimiento
Hallazgos de fondo `[V]`: `RecordMovementTx` no bloquea la fila y descarta el error de lectura; hoy `0` significa "sin costo" (todas las salidas/devoluciones/traslados/ajustes lo mandan) y la regla de §20.2 necesita distinguir "costo fijado por el llamador" de "usar el promedio".

| # | Movimiento | Costo hoy | Riesgo real | Corrección propuesta | Prueba | Dónde |
|---|---|---|---|---|---|---|
| 1 | Compra alta (`purchase_service.go:453-466`) | Costo tal como se digitó (puede incluir IGV) | A quedaría con IGV | Pasar costo **neto** (`tax.CalcItem`, ya disponible) para insumos | Misma compra con y sin IGV da el mismo A | Otra fase (F2); **v3.1** |
| 2 | Anulación de compra (`:685-724`) | Costo original del movimiento | Con "A no se recalcula hacia atrás", anular la última compra sin consumo **deja A mal para siempre** (ej.: Q=10, A=5; compra 10 a 9 → A=7; anulo → Q=10 y A=7) | Guardar `avg_before` en el movimiento y restaurarlo si no hubo otra entrada costeada posterior; si la hubo, aproximación documentada | Comprar+anular (A vuelve); comprar, comprar, anular la primera (A no cambia) | Otra fase (F2); **v3.1** |
| 3 | Anulación de compra tras consumos | Falla por stock insuficiente | Con política `allow` pasaría con Q negativo | **La anulación de compra sigue fallando aunque `allow` exista** (anular no es consumo) | Consumir parcial y anular → error sin cambios en Q, A ni kardex | Otra fase (F2); **v3.1** |
| 4 | Venta / cobro de mesa (`out`) | 0 | El kardex de insumos pasa de 0 a A | Solo `is_supply`; no insumos siguen en 0 | Insumo registra A; producto normal 0 | Otra fase (F2/F5) |
| 5 | Anulación de venta (`sale_cancel_stock.go:67-83`) | 0 (no copia `mv.UnitCost`) | La devolución no conserva el costo | Pasar `mv.UnitCost` con costo fijo | Vender a A=5, comprar a 9, anular: pondera 5 | Otra fase; **v3.1** |
| 6 | NC parcial (`:196-210`) | 0 | Igual; combos sin `sale_item_id` no se reponen (hueco previo) | Pasar `mv.UnitCost` | NC 30 % repone a 5 | Otra fase |
| 7 | Documento de ingreso | `line.UnitCost` **decimal(15,2)** | **Alto:** 0,0042/g → 0,00 (se ignora); 0,0066 → 0,01 (+50 %) | **Ampliar a (15,6)** | Ingreso 0,0042 conserva 0,0042 | **F0a (V194)** |
| 8 | Egreso / merma | Costo digitado por el usuario | El costo digitado se descartaría | Valorizar a A e ignorar lo digitado (insumos) | Merma con 99 guarda A | Otra fase; **v3.1** |
| 9 | Anulación de documento (`:683-747`) | Reusa `line.UnitCost` del detalle, no el del movimiento | Anular un egreso mueve A de forma errónea | Leer el movimiento original y usar su `unit_cost` | Egreso, cambiar A, anular | Otra fase; **v3.1** |
| 10 | Ajuste manual (por documento) | 0 | Sin riesgo | Entrada con costo 0: A no cambia | Ajuste IN no cambia A | Otra fase (nota) |
| 11 | Ajuste absoluto `adjustment` | Quantity = saldo | Si el saldo sube desde 0, valoriza al último A | Documentar; el reconciliador usa `Balance` | A no cambia | **F0a** (reconciliador) / **v3.1** |
| 12 | Ajuste masivo Excel (`inventory_import_adjustment.go`) | Promedio calculado **fuera** de la transacción, con fallback a `purchase_price` (con IGV) y pasando por (15,2) | A = 4,1667 vuelve 4,17 y la entrada "neutral" lo mueve | Para insumos: leer A dentro del lock; IN costo 0, OUT a A | +10 con A=4,1667 no cambia A | Otra fase; **v3.1** |
| 13 | Stock inicial / importación | 0, sin parámetro de costo | Insumo arranca "sin costo" | Parámetro de costo en `RecordInitialStock` | Alta con costo deja A=c | F3; **v3.1** |
| 14 | Series / presentaciones | Pasan por `RecordMovementTx` | Un insumo con series o variantes rompe A | Rechazar `is_supply` con `manage_series` o `has_variants` | Validación | F1; **v3.1** |
| 15 | Escrituras fuera de `RecordMovementTx` | `adjustment*WithSerials` (sin llamadores, escribe sin lock ni chequeo de error); `EnsureProductBranchLink`; `linkProductsToBranchStock` | Saltan el lock y A | Código muerto: documentar; los dos enlaces: **inserción segura** | Test que prohíba escribir `tenant_product_stocks.quantity` fuera de `RecordMovementTx` | **F0a** (enlaces) |
| 16 | Activar `is_supply` en un producto con historial | — | A NULL aunque haya stock y compras | Backfill por producto-sucursal al activar | Activar calcula A | F1/F2; **v3.1** |
| 17 | Compra en USD | `costo × TC` sin redondear; TC sin redondear a 4 decimales pero la compra guarda `decimal(10,4)` | El costo del kardex usa un TC distinto del guardado | Redondear TC a 4 decimales antes de usarlo | TC con 5 decimales | F2; **v3.1** |
| 18 | Compra con unidad de venta | `RoundSunat(c/factor)` a 6 decimales | Pérdida de cifras: 1 sol / 100 kg en gramos = 0,00001 (1 cifra significativa); 0,0000106 → 0,000011 (+4 %) | Calcular sin redondeo intermedio y evaluar `decimal(18,8)` | Factor 3, 24, 100000 | F2; **v3.1** (decisión abierta) |
| 19 | Concurrencia | `First` sin lock; error de BD confundido con "no hay fila" | Lost update de Q y A | Es justo el contenido de F0a: `FOR UPDATE` + UNIQUE + fila pre-creada | Concurrencia MySQL | **F0a** |

### 3.2 Traslados entre sucursales
- **Estado `[V]`:** `Transfer` hace salida y entrada en **dos transacciones** (si falla la segunda, el stock salió y no entró); `CreateTransferWithLines` saca del origen y deja la cabecera `pending`; `ConfirmTransfer` y `CancelTransfer` leen el estado **fuera** de la transacción y actualizan sin condición. Todos mandan `UnitCost = 0` y `tenant_transfer_logs` **no tiene `unit_cost`**: hoy no se puede transportar el costo.
- **Precisión:** `tenant_stock_movements.unit_cost` es `decimal(15,6)`; transportar el costo del origen al destino **no pierde decimales** si el log lo guarda con la misma precisión. El riesgo está en el cálculo (hoy `float64`).
- **Diseño propuesto (F2):** al **crear**, leer A_origen dentro del lock del origen, guardarlo en el movimiento `out` y en `tenant_transfer_logs.unit_cost` (migración aditiva); al **confirmar**, entrada en destino con ese costo congelado, ponderada contra Q y A del destino de ese momento; al **cancelar**, reingreso al origen con el costo original. Si A del origen cambia entre crear y confirmar, no importa: el costo es el de lo despachado.
- **Hallazgos de carrera `[I]`:** doble confirmación concurrente suma dos veces al destino; confirmar+cancelar devuelve y suma a la vez (stock duplicado); `ReverseTransfer` no valida `transfer_id` nulo ni marca `reverted_at` de forma atómica; líneas sin orden estable → deadlocks. **Corrección:** `UPDATE … WHERE status='pending'` con `RowsAffected==1` dentro de la transacción, `Transfer` en una sola transacción, líneas ordenadas.
- **Alcance:** estas correcciones **no se incluyen en F0a** salvo el pre-bloqueo ordenado. Decisión 7 del dueño.

### 3.3 Permisos `recipes.view_cost`: mapa de exposición `[V]`
- `StripProductCost` solo reconoce la clave `purchase_price`; está montado en `/api` (no cubre rutas públicas) y su prueba previa es un `bytes.Contains` literal. La clave `cost` de `GET /products/:id/cost` existe a propósito para escapar del filtro, por lo que las claves nuevas deben ser únicas y no genéricas.
- Hoy viajan **sin filtro** cualquier usuario que pase el permiso de ruta: `unit_cost` en `GET /inventory/movements` (accesible a mozo/cocina/caja por el puente `inventory_access.go`), en documentos de inventario, en compras y órdenes de compra, en el preview del ajuste masivo; `cost`/`profit` en `/dashboard/profit`; y `total_cost`/costos de `/sales/profit-detail`, que está **exento** del filtro y cuya ruta solo exige `sales.view` (el permiso `reports.profit` existe únicamente en el router del frontend: **hueco previo**).
- Excel y PDF los arma el frontend con el JSON del API → negar el dato en JSON basta. No hay sockets; el SSE de facturación no lleva costos.
- **Protección backend propuesta (fases posteriores):** middleware hermano `StripRecipeCost` con **su propio mapa de claves y su propio predicado** (si no, quedaría gobernado por `products.view_cost`); claves propias (`recipe_cost`, `recipe_cost_total`, `avg_unit_cost`, `food_cost_pct`, `margin_pct`…); defensa primaria en handler/servicio (no calcular ni serializar costos sin permiso) y strip como red de seguridad; para claves compartidas (`unit_cost`) filtro por endpoint cruzando con `is_supply`; `purchase_price` de un insumo exige además `recipes.view_cost`; `g.p` / `products.manage` **no** implican `recipes.view_cost`; clave corta `r.vc` en `restaurantperm` y `PermissionSchemaVersion` incrementada; migración de permisos estilo V165 (solo Administrador al inicio); relogin necesario porque los permisos de tenant viajan en el JWT.
- **Relación con F0a:** F0a no puebla costos nuevos; la exposición de `unit_cost` de insumos debe quedar protegida **antes de F1/F2**, no en F0a. Se documenta aquí y en la v3.1.

### 3.4 Campo `recipe_cost` (cálculo y persistencia)
Resumen de la revisión `[V]/[I]` (detalle en v3.1 §20.8):
- Se calcula **dentro de la misma transacción de la venta y bajo el bloqueo de los insumos** (si se calculara antes, el snapshot podría diferir del ledger).
- **Se recomienda guardar además `recipe_cost_total decimal(15,6)`** (fuente exacta): dividir el total entre la cantidad `decimal(15,3)` pierde precisión y `recipe_cost × qty` no reproduce el consumo exacto.
- Cantidad de línea = cantidad **comercial** (con `SaleUnit`, el total se calcula sobre la base convertida); en cobro de mesa y POS directo los platos no llevan unidad de venta.
- Regla `NULL` vs `0`: `NULL` = sin receta, consumo apagado o costo desconocido (cae a `purchase_price`); `0` queda reservado para "costo conocido igual a cero" y nunca se genera automáticamente; `ProfitDetail` debe contar líneas con receta y costo `NULL` como "sin costo" (no basta `<= 0`).
- Ventas parciales y cobro dividido: snapshot por venta con el promedio de ese momento; correcto aunque difiera entre cobros.
- Combos: se necesita vincular componente ↔ línea (hoy los movimientos de combos se agregan por producto); **regla del combo mixto = decisión abierta** (todo-o-nada con `purchase_price` de componentes sin receta, o `NULL`).
- NV → comprobante: el hijo **copia** `recipe_cost` por posición con campos `json:"-"` en `SaleItemInput`; requiere `ORDER BY id` en `GetItems` y prueba con líneas idénticas de costos distintos. `ProfitDetail` cuenta el hijo y omite la NV con hijo vivo, por eso el costo debe viajar al hijo.
- NC: `recipe_cost` no se modifica ni se copia a líneas de NC. `ProfitDetail` hoy **ignora las NC** (no resta devoluciones ni ingreso ni costo): hueco previo que sobrestima la utilidad, a documentar en F5.
- Sucursales: sale del promedio de la sucursal de la venta; `ProfitDetail` filtra por sucursal.
- USD: costo siempre en soles, sin conversión.
- Migración: columna nullable aditiva estilo V145; ADD COLUMN nullable en MySQL 8 es instantáneo `[I]` (confirmar versión antes del despliegue). `purchase_price` no se toca.

### 3.5 Pruebas específicas MySQL / MariaDB para F0a
Local corre MariaDB 10.4 **no estricta**; producción MySQL 8.0.46. Operaciones que exigen pruebas en **ambos** motores (y con `sql_mode` estricto):

| Operación | Qué probar | Por qué difiere o importa |
|---|---|---|
| `SELECT … FOR UPDATE` sobre fila inexistente | N transacciones creando el primer movimiento de un producto/sucursal | Gap locks y deadlock 1213 clásico `[I]`; mitigar con pre-creación (`INSERT … ON DUPLICATE KEY UPDATE id = id`) y luego bloqueo |
| Orden de bloqueo | Dos ventas/documentos con líneas en orden opuesto (A,B vs B,A) | Con pre-bloqueo ordenado no debe haber deadlock |
| Reintento | Reconocer 1213 y 1205 vía `*mysql.MySQLError`; verificar que la transacción reintentada parte de estado limpio | Códigos iguales en ambos, mensajes distintos; 1205 revierte solo la sentencia salvo `innodb_rollback_on_timeout` |
| Aislamiento | Confirmar el nivel real (REPEATABLE READ) y `innodb_lock_wait_timeout`; la variable se llama `transaction_isolation` en MySQL 8 y `tx_isolation` en MariaDB 10.4 | Las consultas de verificación difieren |
| `SKIP LOCKED` | **No usar**: MariaDB lo soporta solo desde 10.6 | Compatibilidad |
| UNIQUE | Creación con y sin duplicados; error 1062 al crear; NULL permitido múltiples veces en ambos | La migración debe detectar antes y omitir con log |
| `ALTER … MODIFY DECIMAL(15,6)` | Idempotencia (consultar `information_schema.columns.numeric_scale`), tiempo en tabla con filas, bloqueo de metadatos | MySQL 8 puede copiar la tabla; el runner usa timeout de 20 s |
| Precisión decimal | Insertar 0,0042 en (15,2) vs (15,6); redondeo vs truncamiento en modo estricto vs no estricto | La BD local no estricta trunca en silencio (regla del proyecto) |
| Savepoints | `inv` construido sobre un `tx` genera SAVEPOINT anidado; un reintento dentro de uno es incorrecto | GORM nested transaction |
| Pool | `SetMaxOpenConns(2)` con más goroutines que conexiones: no debe agotarse ni bloquear login/lecturas | Incidente del pool agotado de `de una empresa` |
| Reconciliador | `LAG`/ventanas (MariaDB ≥10.2, MySQL 8) y tratamiento de `adjustment` (saldo absoluto) | Evitar falsos positivos |
| Métricas | `lock_deadlocks`, `Innodb_row_lock_waits` (nombres/tablas distintas entre motores) | Medición antes/después |

La suite de concurrencia usa `TUKIFAC_TEST_MYSQL_DSN` (patrón de `session_report_converted_test.go`), con salto si no está definida; el de docusage usa `DOCUSAGE_MYSQL_DSN` (no es el mismo). SQLite omite `FOR UPDATE`: **no sirve para probar bloqueos**.

---

## 4. Dependencias
- **El `FOR UPDATE` exige antes el UNIQUE compuesto.** Hoy cada tabla tiene dos índices simples (`product_id`, `branch_id`); sin el compuesto, el bloqueo recorrería uno solo y bloquearía todas las filas del producto en todas las sucursales (o todos los productos de la sucursal). Por eso el esquema (F0a-2) va **antes** que el comportamiento (F0a-3).
- El código **no debe depender** de que el UNIQUE exista (habrá tenants con duplicados donde la migración lo omite).
- La consolidación de duplicados necesita aprobación del dueño porque cambia el stock visible.
- F0a no depende de ninguna otra fase; F0b depende de que F0a esté cerrada y observada.

## 5. Riesgos de F0a para tenants existentes
1. UNIQUE omitido o fallido en tenants con duplicados (el código debe funcionar sin índice).
2. `FOR UPDATE` retiene filas calientes hasta el commit; en venta y cobro de mesa hay pagos, caja y persistencia fiscal **después** del stock.
3. **Deadlocks nuevos** que el lost update ocultaba; sin reintento serían errores 500 al usuario.
4. **Agotamiento del pool** (6 conexiones por tenant) si varias transacciones esperan un mismo lock; acotar el `innodb_lock_wait_timeout` por sesión a pocos segundos.
5. Un reintento mal hecho puede duplicar ventas o series (los closures mutan `sale.ID`, `saleItems`, correlativo): por eso **no se reintenta la venta** en F0a.
6. El stock visible cambia al consolidar duplicados (hoy las lecturas suman filas duplicadas y la cifra puede estar inflada).
7. Tests que creen filas duplicadas fallarían con el UNIQUE en SQLite: correr toda la suite.
8. El texto "stock insuficiente" debe conservarse (tests y frontend dependen).
9. `AssignIfRestricted` y `WidenForBranch` (llamadas dentro de `RecordMovementTx`) pueden tomar locks propios **no auditados**: revisar antes de implementar.
10. Flujos que seguirán sin endurecer (traslados, importación avanzada, código muerto).

## 6. Estrategia de implementación (tres etapas dentro de F0a, cada una con su commit)
1. **F0a-1 (sin cambio de comportamiento):** comando CLI de solo lectura con el reconciliador y el detector de duplicados; línea base sobre la BD **local** (y sobre producción solo con autorización expresa y con el usuario de solo lectura). Cuidado con `adjustment` (saldo absoluto): se valoriza desde el último ajuste.
2. **F0a-2 (esquema aislado):** V193 (UNIQUE con detección previa; si hay duplicados, log y omite) + tags en el struct + V194 (`unit_cost` de documentos a `decimal(15,6)` NULL). Sin cambios de comportamiento.
3. **F0a-3 (comportamiento):** helper de reintento y de pre-bloqueo ordenado → `RecordMovementTx` con `FOR UPDATE` y creación segura → pre-bloqueo en los llamadores multi-línea → `AllowNegative` apagado + sentinela → desempate por `id` en el kardex. Despliegue y observación con las métricas de §8.

## 7. Pruebas
Unitarias del helper de bloqueo y de reintento; integración de documentos, compras, ventas, cobro de mesa, anulación y NC (regresión completa de inventario/ventas/restaurante); concurrencia en MySQL 8 estricto y MariaDB (N goroutines sobre la misma fila con saldo exacto; primer movimiento concurrente; líneas en orden opuesto; pool reducido); migraciones V193/V194 (idempotencia, con y sin duplicados, tabla con filas); reconciliador (falsos positivos con `adjustment`, filas en 0, redondeo 0,0005); prueba dorada: operaciones de inventario y ventas de un tenant sin recetas generan los mismos movimientos y saldos que antes.

## 8. Criterios de aceptación
- Con N hilos sobre un mismo producto/sucursal en MySQL 8 estricto: saldo final exacto, ningún negativo con `AllowNegative=false`, cadena de `balance` estrictamente coherente por `id`.
- Cero deadlocks sin recuperar con líneas en orden opuesto (pre-bloqueo) y reintento solo en inventario.
- Migraciones idempotentes y seguras: tenants sin duplicados reciben el UNIQUE; con duplicados se omite con log y el código sigue funcionando.
- `unit_cost` de documentos conserva 0,0042 sin truncar.
- Reconciliador sin falsos positivos sobre la BD local y informe claro de diferencias reales.
- Regresión completa verde en SQLite, MariaDB y MySQL 8; textos de error intactos.
- Medición antes/después de latencia p50/p95/p99 de `RecordMovementTx`, venta y cobro de mesa, `Innodb_row_lock_waits`, `lock_deadlocks`, `sql.DB.Stats()` y errores 1205/1213, sin degradación material.
- Observación independiente en producción antes de pasar a F0b.

## 9. Plan de reversión
El esquema es aditivo y el UNIQUE se puede quitar sin pérdida; el bloqueo y el reintento viven detrás de un interruptor de configuración (variable de entorno) que vuelve al comportamiento previo sin migrar nada; `AllowNegative` nace apagado.

## 10. Decisiones abiertas para el dueño (con mi recomendación)
1. **Duplicados de stock**: consolidar por el último saldo del kardex (recomendado) frente a sumar frente a revisión manual; quién aprueba el cambio de stock visible.
2. **Migración UNIQUE**: omitir con log en tenants con duplicados (recomendado) o bloquear el migrate-fleet.
3. **Timeout de espera de lock**: valor por sesión (recomiendo 5 s) y confirmar con una consulta **de solo lectura autorizada** el aislamiento real, `innodb_lock_wait_timeout` y `DB_TENANT_MAX_OPEN` de producción.
4. **Reintento**: solo en operaciones de inventario (recomendado); reintentar ventas y cobro de mesa exigiría refactorizar el estado de los closures y se pospone según métricas.
5. **Pre-bloqueo en ventas y cobro de mesa**: una llamada aditiva antes del bucle de stock (recomendado); ¿autorizas tocar `sale_service.go` y `restaurant_service.go` solo para eso?
6. **Alcance de `AllowNegative`**: solo `RecordMovementTx` (recomendado) o también los prechequeos duplicados de ventas, documentos, restaurante y e-commerce.
7. **Carreras de traslados y `Transfer` en dos transacciones**: ¿fase separada (recomendado) o dentro de F0a?
8. **Desempate por `id` en el kardex**: en esta fase (recomendado, cambio mínimo).
9. **Reconciliador**: solo comando CLI en F0a (recomendado); endpoint de superadmin después.
10. **`unit_cost` del detalle**: seguir permitiendo NULL (recomendado) y dejar los índices simples redundantes para limpieza posterior.
11. **Precisión de costos** (para F2, no F0a): evaluar `decimal(18,8)` en `unit_cost`, `avg_unit_cost` y el log de traslados a la vez, por la pérdida de cifras con costos por gramo.

## 11. Qué NO se verificó
- No se ejecutó nada ni se consultó producción. Aislamiento, `innodb_lock_wait_timeout`, `DB_TENANT_MAX_OPEN` reales y la cantidad de duplicados de stock en producción son desconocidos.
- Las carreras y el comportamiento de InnoDB con `FOR UPDATE` sobre filas inexistentes son deducciones.
- No se leyeron en detalle `AssignIfRestricted`/`WidenForBranch`, `schema_probe_cache.go` ni todos los endpoints fuera del grupo `/api`.
- No se verificó si el frontend envía el costo con o sin IGV cuando la compra tiene IGV incluido.
