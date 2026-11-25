# F0a-2 — Índice único de stock y `unit_cost` de documentos: informe de entrega

Fecha: 25-nov-2026 · Alcance autorizado: **F0a-2, solo en local**. No se desplegó, no se consultó ni modificó producción, no se implementó F0a-3 y no se consolidó ni modificó ningún saldo.

## 1. Archivos

| Archivo | Cambio |
|---|---|
| `pkg/database/tenantmigrations/stock_unique_index.go` | **Nuevo.** Revisa duplicados y crea (u omite) el índice; usado por la migración y por el comando |
| `pkg/database/tenantmigrations/v193_stock_unique_indexes.go` | **Nuevo.** V193 |
| `pkg/database/tenantmigrations/v194_inventory_document_unit_cost.go` | **Nuevo.** V194 |
| `pkg/database/tenantmigrations/registry.go` | Registra V193 y V194 (la versión objetivo sale del registro) |
| `pkg/database/migrations.go` | `TenantInventoryDocumentDetail.unit_cost` a `decimal(15,6)` |
| `pkg/cmd/stock_unique_index.go`, `pkg/cmd/cli.go` | **Nuevo** comando `stock-unique-index` |
| `pkg/database/tenantmigrations/v193_v194_test.go` | **Nuevo.** 5 pruebas |

No se tocó ventas, compras, traslados, ni la lógica de inventario (`RecordMovementTx` y demás).

## 2. Comportamiento

**V193.** Índices `uk_tenant_product_stocks_product_branch` y `uk_tenant_pres_stocks_presentation_branch`.
- Sin duplicados: los crea. Idempotente.
- Con duplicados: **omite el índice de esa tabla**, registra en el log (`[v193] … ÍNDICE OMITIDO …`, número de grupos y hasta 10 ejemplos con sus ids) y termina sin error. La otra tabla recibe su índice si está limpia.
- Si se inserta un duplicado justo mientras se crea (error 1062), se trata como duplicado.

**Desviación respecto a la propuesta:** el índice **no** se declaró en los tags de los modelos. Cualquier `AutoMigrate` sobre un tenant con duplicados habría fallado. Los tenants nuevos lo reciben igual porque el alta ejecuta todas las migraciones versionadas.

**V194.** `decimal(15,2)` → `decimal(15,6)` NULL. Si la columna ya tiene 6 decimales no repite el `ALTER`.

**Comando** (para crear el índice después de limpiar duplicados, porque V193 corre una sola vez):
```
go run . stock-unique-index [--slug=] [--limit=] [--active-only=true]          # solo consulta
go run . stock-unique-index --apply                                            # crea los que falten donde no haya duplicados
```
Sin `--apply` no escribe. Con `--apply` solo crea índices. No pasa por `InitDatabase` y rechaza una BD no local salvo `--allow-remote`.

## 3. Pruebas

| Prueba | Resultado |
|---|---|
| 5 pruebas nuevas en SQLite | OK (la de `ALTER` se omite: solo aplica en MySQL/MariaDB) |
| 5 pruebas en **MariaDB 10.4 estricto** | OK |
| V193+V194 dos veces sobre una **copia** de la BD local `demo` (320 saldos, 15 de presentación, 9 líneas de documento) | Índices creados, `unit_cost` con 6 decimales, conteos y sumas idénticos antes y después |
| `stock-unique-index` (consulta) sobre `demo` original | 0 duplicados, índices aún no creados; no escribió |
| `go test ./...` | 100 paquetes OK, sin fallos |

Cubren: creación e idempotencia; rechazo posterior de filas repetidas; omisión con duplicados con motivo en el log, sin tocar saldos y con el tenant operando; consulta que no crea y `--apply` que crea tras limpiar; tablas ausentes; `0,0042` antes truncado a `0,00` y conservado después, con NULL y valores existentes intactos.

**No probado:** MySQL 8.0.46; tiempos del `ALTER` y del índice con tablas grandes; la migración a través del fleet.

## 4. Condiciones para desplegar (importante)

1. **No desplegar F0a-2 sin F0a-3.** `RecordMovementTx`, `EnsureProductBranchLink` y `linkProductsToBranchStock` leen y luego insertan sin protección. Hoy una carrera crea una fila duplicada en silencio; con el índice **esa operación fallaría con error 1062** (una venta, traslado o alta podría mostrar error). La creación segura es parte de F0a-3.
2. **Esperar a que se resuelva la red entre los VPS** (ver `docs/arquitectura/DIAGNOSTICO_LENTITUD_20261125.md`): V193/V194 recorren los 849 tenants y `migrate-fleet` ya se ha colgado con la red degradada.
3. Antes del despliegue conviene correr `audit-stock-reconcile` en producción (solo lectura, con autorización) para saber en cuántos tenants se omitirá el índice.
4. `V194` puede copiar la tabla (bloqueo de metadatos); la conexión de migración limita la espera a 20 s.
5. Un usuario con sucursales restringidas cuya lectura de stock quede filtrada por sucursal podría hoy crear filas duplicadas; con el índice recibiría error. No se verificó si existe ese camino en la práctica; F0a-3 debe cubrirlo.

## 5. Reversión
Los índices se pueden eliminar sin pérdida de datos (`DROP INDEX …`). El ensanche de `unit_cost` no pierde datos y no necesita revertirse.
