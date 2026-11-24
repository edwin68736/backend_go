# F0a-1 — Reconciliador de solo lectura: informe de entrega

Fecha: 25-nov-2026 · Alcance autorizado: **F0a-1 exclusivamente** (comando CLI de solo lectura). No se implementó F0a-2 ni F0a-3, no se consultó producción, no se ejecutaron migraciones, no se modificó ningún dato de inventario, ventas, compras ni traslados, y no se desplegó nada.

## 1. Archivos

| Archivo | Cambio |
|---|---|
| `internal/inventory/reconcile/reconcile.go` | **Nuevo.** Lógica de auditoría (solo `SELECT`), testeable sin CLI |
| `internal/inventory/reconcile/reconcile_test.go` | **Nuevo.** 12 pruebas (SQLite y MySQL/MariaDB) |
| `pkg/cmd/audit_stock_reconcile.go` | **Nuevo.** Comando `audit-stock-reconcile` |
| `pkg/cmd/cli.go` | **Modificado** (+6 líneas): despacho del comando **antes** de `InitDatabase` y línea de ayuda |
| `docs/recetas/F0a-1_INFORME.md`, `docs/recetas/baseline/F0a-1_linea_base_local.json` | Informe y línea base local |

No se tocó ningún archivo de ventas, compras, traslados, inventario (servicios), migraciones ni modelos.

## 2. Funcionamiento del comando

```
go run . audit-stock-reconcile [--slug=] [--limit=] [--product=] [--branch=] [--max-detail=20] [--json=archivo] [--fail-on-diff] [--active-only=true]
```

Por cada tenant detecta, **sin corregir nada**:
1. **Duplicados** de saldo: más de una fila por `(product_id, branch_id)` en `tenant_product_stocks` o por `(presentation_id, branch_id)` en `tenant_product_presentation_stocks`; informa ids, cantidades, suma, **la fila que escribe `RecordMovementTx`** (la de menor id) y el saldo del kardex.
2. **Saldo vs kardex**: compara el saldo de la fila que escribe con el `Balance` del último movimiento de esa clave (`DIFERENCIA`), `STOCK_SIN_KARDEX`, `KARDEX_SIN_STOCK` y `SALDO_NEGATIVO`.
3. **Cadena de saldos rota**: recorre el kardex en orden de `id` y comprueba que cada `Balance` sea el anterior ± la cantidad. Es la huella de una **actualización perdida** (dos movimientos que leyeron el mismo saldo). Se cuenta **una sola vez** por divergencia (continúa desde el saldo guardado).
4. **Filas con clave nula o 0**.

**Movimientos `adjustment`**: su `Quantity` es el **saldo absoluto**, no un delta; la cadena se reinicia ahí (`adjustment` → esperado = cantidad). `adjustment_in`/`adjustment_out` suman/restan. Un primer movimiento que no parte de saldo 0 se informa como `inicio_sin_saldo_previo` (stock anterior al kardex): es **informativo**, no un error. Los tipos desconocidos no se evalúan y se cuentan.

**Salida**: línea por tenant, detalle acotado por `--max-detail` (los conteos del resumen son siempre exactos), totales y, con `--json`, el informe completo por tenant/producto/sucursal (archivo con permisos 0600). Con `--fail-on-diff` termina con código 2 si hay hallazgos.

**Garantías de solo lectura (verificadas):**
- Solo ejecuta `SELECT`.
- Fija **una conexión** con `SET SESSION TRANSACTION READ ONLY` (el servidor rechaza cualquier escritura; hay una prueba que lo demuestra) y una lectura consistente `START TRANSACTION WITH CONSISTENT SNAPSHOT`, de modo que saldos y kardex salen de la misma foto y no hay diferencias falsas por ventas en curso. No toma locks.
- **No pasa por `InitDatabase`**: no ejecuta `EnsureCentralFleetSchema` (AutoMigrate de las tablas de flota de la BD central) ni crea nada. Comprobado: la tabla `fleet_migration_states` no existe en la BD central local y sigue sin existir.
- **Se niega a correr contra una BD no local** (y contra entorno de producción) salvo `--allow-remote`, que requiere autorización expresa; probado con `DB_HOST` remoto: aborta sin conectar.
- Checksums (`CHECKSUM TABLE`) de las tablas de stock, movimientos, productos y ventas del tenant, y de las tablas centrales de tenants, **idénticos antes y después** de ejecutarlo.

## 3. Pruebas ejecutadas

| Prueba | Resultado |
|---|---|
| `go build ./...`, `go vet` de los paquetes tocados | OK |
| `internal/inventory/reconcile` (12 pruebas) en **SQLite** | OK |
| Las mismas 12 en **MariaDB 10.4.32 con `sql_mode=STRICT_ALL_TABLES`** (BD desechable `tukifac_f0a_test`, creada y eliminada para la prueba) | OK |
| Regresión `internal/inventory/...`, `pkg/database/...` (incluye `tenantmigrations`, `engine`, `tenantbackfills`) | OK |

Cobertura de las 12: tenant coherente sin hallazgos; duplicados detectados **sin consolidar** (la fila que escribe es la de menor id; los datos no cambian); diferencia saldo/kardex; `adjustment` como saldo absoluto sin falsos positivos (incluido ajuste a cero); cadena rota contada una vez; inicio sin saldo previo informativo; presentaciones con clave propia; sin kardex / sin stock / negativos / clave 0; filtros por producto y sucursal; `--max-detail` trunca el detalle pero no los conteos; tablas ausentes se omiten sin error; **solo lectura** (con la sesión en modo solo lectura funciona y no cambia ningún dato; en MariaDB un `UPDATE` es rechazado por el servidor).

**Hallazgo durante las pruebas (corregido en esta misma fase):** `db.Migrator().HasTable` **entra en pánico** (nil pointer) con una conexión fijada en solo lectura en MariaDB 10.4. Lo habría sufrido la CLI real; se reemplazó por una sonda `SELECT 1 FROM tabla LIMIT 1`.

No se pudo probar contra **MySQL 8.0.46** (el motor de producción): no hay instancia local. `SET SESSION TRANSACTION READ ONLY` y `START TRANSACTION WITH CONSISTENT SNAPSHOT` existen en MySQL 8, pero hay que repetir las pruebas allí.

## 4. Informe de duplicados detectados (BD local)

Tenants locales: **1** (`demo`, el único activo en la BD central local).

| Tabla | Filas | Grupos duplicados | Filas sobrantes | Claves nulas o 0 |
|---|---:|---:|---:|---:|
| `tenant_product_stocks` | 320 | **0** | 0 | 0 |
| `tenant_product_presentation_stocks` | 15 | **0** | 0 | 0 |

No hay duplicados reales para validar la consolidación: el comportamiento ante duplicados está cubierto **solo por pruebas sintéticas**. La cantidad real en producción es **desconocida** (no se consultó).

## 5. Resultado de la reconciliación (BD local, tenant `demo`)

Movimientos analizados: 723. Hallazgos: **2 diferencias saldo ↔ kardex, 2 cadenas rotas**, 4 inicios sin saldo previo (informativos), 0 negativos, 0 stock sin kardex, 0 kardex sin stock.

| Producto (sucursal 1) | Saldo | Kardex | Delta | Cadena rota |
|---|---:|---:|---:|---|
| 302 | 9,000 | 42,000 | −33,000 | mov 737 (`VENTA/NV001-00000206`, 13-nov): previo 9, esperado −15, guardado 45 |
| 309 | 1,000 | 6,000 | −5,000 | mov 642 (`VENTA/NV001-00000164`, 10-nov): previo 0, esperado −1, guardado 4 |

Lectura: son datos de pruebas locales (ventas y anulaciones de notas de venta del 10 y 13-nov). El saldo guardado y el saldo del kardex divergieron en un movimiento y no se reconciliaron después; es **compatible con** un saldo leído sin bloqueo o con una escritura fuera de la cadena, pero **no prueba la causa** (no se reprodujo). No se corrigió nada. Informe completo: `baseline/F0a-1_linea_base_local.json`.

## 6. Limitaciones y riesgos

1. **Base local de un solo tenant**: la línea base es pequeña y no representa la flota. Los duplicados y rupturas reales de producción son desconocidos hasta que autorices una corrida en solo lectura (con `--allow-remote` y el usuario de solo lectura).
2. **MySQL 8.0.46 no probado** (solo MariaDB 10.4 y SQLite).
3. **Causa no determinada**: el reconciliador detecta divergencias; no dice por qué ocurrieron. Un movimiento con `Balance` inconsistente puede ser lectura sin lock, escritura fuera de `RecordMovementTx` (hay código que lo hace) o datos de pruebas manuales.
4. **Productos con variantes**: si un producto con presentaciones tiene movimientos sin `presentation_id`, se auditan como clave de producto; puede aparecer como `STOCK_SIN_KARDEX`/`KARDEX_SIN_STOCK` sin ser un error (a revisar caso por caso).
5. **Traslados pendientes** (stock ya salido del origen y aún no entrado al destino) no se muestran como cuenta aparte; los movimientos existen y la cadena es coherente.
6. **Rendimiento en producción**: una pasada ordenada por `id` sobre `tenant_stock_movements` (memoria proporcional a las claves, no a los movimientos) y una transacción de lectura consistente por tenant, secuencial. En tenants con millones de movimientos puede tardar; se recomienda `--slug`/`--limit` y horario de baja carga. Una lectura consistente larga mantiene versiones antiguas (undo) mientras dura: **no ejecutarla sobre toda la flota sin medir antes**.
7. **Precisión de la comparación**: tolerancia 0,0005 (los campos son `decimal(15,3)`).
8. La BD de pruebas `tukifac_f0a_test` se creó solo para validar en MariaDB y se eliminó.
9. Los datos locales de `demo` ya traían inconsistencias antes de esta fase; el comando no las provocó (checksums idénticos antes/después).

## 7. Qué NO se hizo (a propósito)
F0a-2 (UNIQUE, `unit_cost` de documentos), F0a-3 (bloqueo, reintentos, `AllowNegative`, desempate del kardex), consolidación de duplicados, consulta a producción, corrección de saldos, endpoint, migraciones y despliegue.
