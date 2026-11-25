# F2b — Costo promedio de insumos: informe

Fecha: 25-nov-2026 · Estado: **implementado y probado en local; sin push, sin deploy, sin tocar producción.** F3 no se empezó.
Alcance: `F2_PROPUESTA.md`, parte F2b, con las recomendaciones 2 a 5.

## 1. Qué quedó
- **V196** (aditiva): `avg_unit_cost`, `avg_before`, `cost_applied`, `cost_unknown` y `tenant_transfer_logs.unit_cost`. De solo lectura
  en los modelos; un tenant sin migrar compra y mueve como antes. Calcula el promedio de los insumos con historial.
- **Cálculo** (`internal/inventory/service/supply_cost.go`), solo insumos, con la fila de saldo bloqueada: reglas de la propuesta §3.
- **Compras:** costo sin IGV (con IGV en Nuevo RUS), sobre el importe de la línea y con el tipo de cambio a 4 decimales; es también el
  «último costo». Anular devuelve el promedio anterior si no hubo otra entrada costeada.
- **Documentos:** anular usa el costo del movimiento original. El conteo por Excel no cambia el promedio.
- **Traslados:** el costo se congela al despachar y se pondera al confirmar o cancelar.
- **Permisos:** `pkg/middleware/supply_cost.go` pone en 0 los costos de las filas de insumos en cualquier respuesta de `/api` para quien no
  tiene `products.view_cost` ni `rc.c`.
- **Tukichef:** «Promedio» y «Último» en la lista (hasta 4 decimales), «sin costo» en el kardex y aclaración en el formulario.

## 2. Diferencias con la propuesta
- Una columna más, `cost_applied`: sin ella no se puede saber si hubo una entrada costeada posterior al anular.
- Compras y documentos anulan sus líneas **de la última a la primera** (también las de productos normales; solo cambia el orden de los
  movimientos de anulación), para que un insumo repetido vuelva bien.
- El cálculo usa `math/big` y se redondea a 6 decimales al guardar.

## 3. Pruebas
- `internal/supplies/costs_test.go` (12 casos): compra con y sin IGV, Nuevo RUS, saco, USD, anular compras en varios órdenes, salidas y
  ajustes, saldo negativo, documentos, traslados, conteo Excel, productos normales sin cambios, tenant sin migrar y recálculo.
  Desactivando el cálculo fallan 9: detectan.
- Filtro de costos (`pkg/middleware/supply_cost_test.go`) y promedio en las rutas de insumos (`supply_routes_test.go`).
- Tukichef: `tsc` y 129 pruebas.
- SQLite: suite completa sin `pkg/saas`; 100 paquetes en verde y `internal/billing/handler` falló una vez en una prueba de eventos en
  vivo (`TestBillingEventsSSE_…`) que pasó tres veces seguidas al repetirla.
- MariaDB estricto: insumos, inventario, restaurante y migraciones en verde; en `sales/service` fallan las 2 pruebas de anulación que ya
  fallaban antes de insumos.

**No probado:** la pantalla de Tukichef en el navegador, MySQL 8, producción.

## 4. Límites conocidos
- El promedio no se reescribe al anular una entrada antigua si hubo otra costeada después.
- Quien tiene permiso de compras sin `products.view_cost` ve en 0 el costo unitario de las líneas de insumos, pero **el total de la compra
  sigue visible**.
- El costo de un ingreso manual de inventario se toma como neto (no hay indicador de IGV en ese documento).
- Un insumo con series o presentaciones no puede existir, así que no se calcula promedio por presentación.

## 5. Despliegue
Con el resto de insumos y recetas (decisión: todo junto). Backend con `migrate-fleet` (V193–V196).
