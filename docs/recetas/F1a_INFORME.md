# F1a — Catálogo de insumos (backend): informe

Fecha: 25-nov-2026 · Estado: **implementado y probado en local; sin push, sin deploy, sin tocar producción.** F1b (pantallas) no se empezó.
Alcance: `F1_PROPUESTA.md` §3.1–3.6 con las 8 decisiones recomendadas (§9).

## 1. Qué quedó
- **V195** (aditiva): `tenant_products.is_supply` y `supply_kind`, `tenant_restaurant_settings.recipes_enabled`. Todo producto existente queda
  como no insumo y el módulo apagado. Los tres campos son de **solo lectura para GORM**: un tenant sin migrar sigue creando productos, y solo
  `supplies.Mark` y `database.SetRecipesEnabled` los escriben.
- **Reglas** (`pkg/supplies`, `internal/products/service/supply_rules.go`): clase inmutable; bien con stock, precio de venta 0, sin carta,
  categoría, sucursal propia, series, presentaciones, extras ni combo; códigos que no se cruzan con productos; reactivar o importar no cambia
  la clase; no ocupa cupo.
- **Ocultos por defecto** en `GET /api/products` (`supplies=exclude|only|include`), lector de códigos, tienda, conteos de categoría, marca y
  área, contador del dashboard y cupo.
- **Rechazados por el servidor**: venta (líneas y salidas de combos/extras), pedido y comanda, POS directo, cobro de mesa, cotización, pedido
  web, combos, publicar/llevar a carta/quitar stock en masivo, precios por sucursal, actualización e importación masivas, compra con
  «actualizar precio de venta», borrado masivo de un insumo con movimientos, asignación de sucursales.
- **Sí funcionan**: compra, ajuste, traslado, kardex (también el filtro de restaurante) y resumen de stock (con `IsSupply`).

## 2. API nueva
Bajo `/api/restaurant`, con módulo `restaurant`, perfil de restaurante y `recipes_enabled`:

| Ruta | Permiso |
|---|---|
| `GET /supplies`, `/supplies/:id`, `/:id/stock`, `/:id/movements`, `/:id/units`, `/supplies/unit-catalog` | ver o gestionar insumos |
| `POST /supplies`, `PUT /supplies/:id`, `POST/PUT/DELETE /:id/units[/:unitId]` | gestionar insumos |

- Permisos: `sp.v` (supplies.view), `sp.m` (supplies.manage), `rc.c` (recipes.view_cost). Por defecto administrador y supervisor.
  `PermissionSchemaVersion` pasó a 3 (renueva la caché).
- El costo viaja y se escribe solo con `rc.c`, como `unit_cost`. Sin el permiso, el que se envíe se ignora.
- La unidad base no se cambia si el insumo ya tiene movimientos.
- Activación: `PUT /api/superadmin/tenants/:id/restaurant-recipes {"enabled": true}` (`empresas.update`). Se publica como
  `restaurant_recipes` en las capacidades.
- `GET /api/products?supplies=…` solo se respeta con `products.view` o permiso de insumos; si no, se ignora.

## 3. Pruebas
| Prueba | Dónde |
|---|---|
| Forma del insumo, códigos, inmutabilidad, reactivación | `internal/supplies/acceptance_test.go` |
| Ausencia en listados, lector, tienda y cupo | idem |
| Rechazo en venta, mesa, cobro, cotización, web, combo, masivos, importación, compra | idem |
| Compra, ajuste, traslado, kardex, resumen, borrado, unidades de compra | idem |
| Tenant sin migrar y migración V195 repetida | idem |
| Rutas y permisos (admin, supervisor, mozo, sin permiso de costo, módulo apagado) | `internal/restaurant/supply_routes_test.go` |
| `supplies=` en `/api/products` | `internal/products/handler/supplies_list_test.go` |

- SQLite: suite completa sobre el código final, 101 paquetes en verde y **uno con fallos, `pkg/saas`, previos a F1a**: usa archivos SQLite de
  nombre fijo y falla con `database is locked`. Sobre `81fbe24` falla igual: en bloque, 46 pruebas; una por una, las mismas 13 de 107
  (flujos de pago). Las demás pasan, incluidas las que usan el conteo del cupo. No se corrigió.
- MariaDB 10.4 en modo estricto: aceptación de F1a, `restaurant/service`, `inventory/service` y `tenantmigrations` en verde.
- `sales/service` en MariaDB: fallan `TestCancel_dobleAnulacionInterna…` y `TestCancel_anulacionNormalSigueIgual` (pruebas de F0b). **Fallan
  igual sin F1a** (comprobado sobre `81fbe24`); en SQLite pasan. Causa no investigada.

## 4. No probado
- MySQL 8 (producción es 8.0.46) y datos reales de un tenant.
- Ningún flujo desde pantalla: no hay pantallas todavía.
- La ruta del superadmin solo se probó en autorización (401/403), no activando un tenant de punta a punta.
- No se emitió ningún comprobante contra SUNAT. F1a no cambia el contenido de los comprobantes; solo rechaza líneas que son insumos.

## 5. Límites conocidos y pendientes
- **Panel central sin interruptor:** el módulo se activa por API. Falta decidir si entra en F1b.
- **Tukifac** todavía no envía `supplies=include`: hasta F1b sus selectores de compra e inventario no muestran insumos.
- **Reporte de stock de Tukichef:** el backend lo permite con `supplies=include`; falta que la pantalla lo pida.
- El lector de códigos tampoco encuentra insumos en compras.
- Las alertas de stock bajo del dashboard incluyen insumos sin rotular.
- El costo de un insumo en endpoints antiguos (productos, kardex, documentos) sigue los permisos de siempre; se revisa en F2/F3.
- La unicidad de códigos es por consulta previa, sin índice único (igual que hoy entre productos).
- En un tenant sin migrar, cada venta consulta el esquema una vez (solo se cachea el resultado positivo). Dura hasta `migrate-fleet`.

## 6. Despliegue (no autorizado)
1. F0a antes o junto (V193 y V194 preceden a V195).
2. Backend con `migrate-fleet` (V195). No usar `skip_migrate`.
3. El módulo queda apagado en todos: nada cambia hasta activarlo por empresa.
4. No activar ninguna empresa antes de F1b.

Vuelta atrás: desplegar el binario anterior; las columnas nuevas pueden quedarse. Si ya hubiera insumos creados, el binario anterior los
trataría como productos con precio 0: desactivarlos antes.
