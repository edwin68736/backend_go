# Validación de V179 en un clon aislado (esquema real de producción, datos sintéticos)

Fecha: 10-nov-2026. **No se tocó producción** (solo se ejecutó una lectura de DDL) y **no se ejecutó `migrate-fleet`**.

## 1. Cómo se armó el clon y qué precauciones se tomaron
- No hay staging. Evalué tres opciones:
  1. Copiar datos de un tenant real a la máquina local: **descartada** (sacaría datos personales y fiscales de producción).
  2. Crear una base clon en el servidor de producción: **descartada** (el acceso es de solo lectura por regla de auditoría).
  3. **Elegida:** `mysqldump --no-data` (solo `CREATE TABLE`, 92 tablas, **0 filas**) del esquema real de `saas_tenant_doriconta`, cargado en 6 bases locales descartables, con datos **100 % sintéticos** (nombres inventados, correos `@example.invalid`) que reproducen la *forma* de los casos reales: mismos conteos de ventas por sucursal, usuarios por sucursal, cajas abiertas, estructura de principales y huérfanos.
- El esquema trae los índices reales de producción (p. ej. `uk_tenant_sales_series_correlative`, `ux_branch_daily_comanda_counter`). Las 6 bases se eliminaron al terminar.
- La migración se ejecutó con el *scope* de sucursal instalado, como en `migrate-fleet`, y con el `sql_mode` estricto de producción (`STRICT_TRANS_TABLES, ONLY_FULL_GROUP_BY, NO_ZERO_DATE…`).

## 2. Diferencias con producción (limitaciones)
| Producción | Clon |
|---|---|
| MySQL **8.0.46** | **MariaDB 10.4.32** (la única base disponible localmente) |
| Datos reales (miles de filas por tenant) | Datos sintéticos (≤ 1 900 ventas) |
| índice único funcional `ux_open_session_per_table` | omitido (sintaxis solo de MySQL 8; no afecta a V179: está en `tenant_table_sessions(table_id,status)`) |

`UPDATE IGNORE` y `information_schema` se comportan igual en ambos para lo que usa V179, pero **la prueba definitiva requiere MySQL 8** (no puedo instalarlo sin descargar un binario; si lo autorizas lo hago con el instalador oficial en un directorio aislado).

## 3. Resultados por escenario
| Escenario (basado en) | Sucursales antes → después | Principales que se conservan | Huérfanos antes → después | Filas que ya tenían sucursal modificadas | 2.ª ejecución |
|---|---|---|---|---|---|
| **doriconta** (2 principales, 1 huérfano) | Principal✔ · RESTAURANTE · sucursal 01✔ → Principal✔ · RESTAURANTE · sucursal 01 (**desmarcada**) | Principal (menor id activo) | 1 → 0 (retención → Principal; ventas 1841→1842) | **0** de 1 911 | idempotente |
| **industrialrafaz** (3 huérfanos) | sin cambios | FABRICA | 3 → 0 (a FABRICA; 500→503) | **0** de 555 | idempotente |
| **col** (1 huérfano) | sin cambios | Principal | 1 → 0 (70→71) | **0** de 73 | idempotente |
| **bernapom** (sin principal) | SANTA ANITA pasa a principal | SANTA ANITA (primera activa) | 0 → 0 | **0** de 31 | idempotente |
| **naturales** (sin principal) | San Borja pasa a principal | San Borja (primera activa) | 0 → 0 | **0** de 8 | idempotente |
| **stress** (caso extremo no visto en producción: principal inactiva + otra activa + conflicto único) | Cerrada(inactiva)✔ + Abierta✔ → solo **Abierta** | Abierta (la activa, aunque tenga mayor id) | ventas 2→0; contadores de comandas 2→**1** | **0** de 10 | idempotente |

Totales de filas por tabla: **sin cambios** en los 6 escenarios (V179 no inserta ni borra). Duración por tenant: 66–110 ms. Errores: ninguno.

## 4. Conflictos y advertencias
- Único conflicto reproducido (escenario *stress*): un contador de comandas huérfano `(branch_id=0, business_date=20261110)` chocaba con el ya existente `(principal, 20261110)`. V179 **omitió esa fila sin fallar**, vinculó la otra (`20261111`) y dejó el aviso `WARN V179: registros sin sucursal que no se pudieron vincular (índice único); revisar a mano table=tenant_branch_daily_comanda_counters rows=1`. En producción **no existe** este caso (0 huérfanos en esa tabla).
- Ajuste surgido de la prueba: el conteo posterior generaba mensajes de auditoría del scope; ahora la migración declara su acceso como `System` (una línea informativa en lugar de 16).
- Los índices únicos de `tenant_sales` no incluyen `branch_id`, así que vincular sucursal no puede chocar.

## 5. Qué falta antes de `migrate-fleet`
1. Repetir esta prueba en **MySQL 8.0.46** (autorizar la descarga del instalador oficial o proporcionar una instancia/staging).
2. Confirmar con los clientes la principal de `doriconta` y `naturales` (ver `REPORTE_CONFIRMACION_PRINCIPALES.md`).
3. Respaldo previo (backup/snapshot) y ejecutar V179 primero en 1–2 tenants pequeños.
