# F4a — Recetas, costo, precio sugerido y disponibilidad (backend): informe

Fecha: 25-nov-2026 · Estado: **implementado y probado en local; sin push, sin deploy, sin tocar producción.** F4b (pantallas) no se empezó.

## 1. Qué quedó
Aún **no hay nada visible para el usuario**: son las rutas que usarán las pantallas de F4b.

- **V198** (aditiva, repetible): `tenant_recipes`, `tenant_recipe_lines`, `tenant_price_changes` y en la configuración de restaurante food
  cost objetivo (30 %), redondeo (0.10), dirección (al alza) y base de costo (promedio).
- Cálculo en `internal/recipes/service`: costo por porción con merma y rendimiento, food cost sobre precio neto, margen, precio sugerido
  con redondeo (`none`, `0.10`, `0.50`, `1.00`, `x.90`) y disponibilidad con el insumo limitante.
- Receta por presentación; la que no tiene una propia usa la común del plato.
- Combos: suma de los componentes por defecto.

| Ruta (`/api/restaurant`) | Permiso |
|---|---|
| `GET /recipes`, `GET /recipes/:productId` | `rc.v` o `rc.m` (costos solo con `rc.c`) |
| `PUT` / `DELETE /recipes/:productId` | `rc.m` |
| `GET /recipe-settings`, `GET /recipes/:productId/price-history` | `rc.c` |
| `PUT /recipe-settings` | `rc.m` + `rc.c` |
| `POST /recipes/:productId/apply-price` | `rc.m` + `rc.c` + gestionar productos |

Permisos nuevos: `rc.v` (administrador, supervisor, cocinero) y `rc.m` (administrador, supervisor). Versión de plantillas de permisos 4 → 5.

## 2. Precio de venta
Solo cambia con «aplicar precio». El cliente envía el precio que vio; si ya es otro, responde 409 y no cambia nada. Si la sucursal tiene
precio propio se cambia ese; si no, el general (`applied_branch_id = 0`). Cada cambio queda en `tenant_price_changes`.

## 3. Efectos fuera de las recetas
- Un insumo usado en una receta **no se puede eliminar** (individual ni masivo) ni cambiar de unidad base.
- Al eliminar físicamente un plato (borrado masivo) se borran sus recetas.
- Las ventas, el cobro, el stock y la facturación **no se modificaron**: nada se descuenta todavía (F5).

## 4. Reglas que conviene saber
- Con un ingrediente sin costo, el plato queda «incompleto»: sin food cost ni precio sugerido (la disponibilidad sí se muestra).
- En un combo, si a un componente le falta la receta, no hay costo ni disponibilidad. Los extras y modificadores no se costean.
- En un plato exonerado, en Nuevo RUS o con precio que no incluye IGV, el precio publicado se toma como neto y no se le suma IGV.
- Quien no ve costos puede (si tiene `rc.m`) editar la receta, pero no cambia el objetivo propio de food cost.

## 5. Pruebas
- Fórmulas (ejemplo aprobado: 4.15 → 16.40, food cost 29.86 %), redondeos y conversión g↔kg, ml↔L.
- Por las rutas reales, con stock y costo cargados con «carga inicial»: hamburguesa (alcanza para 13), merma y rendimiento, 0.0035 por
  gramo, ingrediente sin costo, exonerado, Nuevo RUS, presentaciones, combos, configuración, permisos (cocinero sin costos, mozo sin
  acceso), plato de otra sucursal, aplicar precio (general, de sucursal, de presentación, 409) y protección de insumos.
- SQLite y MariaDB 10.4 estricto; seis «aplicar precio» simultáneos en MariaDB: uno aplica y cinco reciben 409.
- Suite completa sin `pkg/saas` (fallos conocidos previos): sin errores.

## 6. No probado
- MySQL 8 y producción; la V198 con `migrate-fleet` ni sobre una base real.
- Nada desde pantalla (no existe todavía). Plato cuyo precio no incluye IGV solo se probó en el cálculo, no por las rutas.
- Rendimiento de la lista con cartas muy grandes.
- Cuántos clientes usan presentaciones o combos.

## 7. Observaciones
- «Aplicar precio» en el precio general afecta a todas las sucursales sin precio propio, aunque quien lo haga solo tenga una: F4b debe
  decirlo en la confirmación. Es el mismo alcance que ya tiene editar el producto.
- `tenant_price_changes` registra solo lo aplicado desde recetas, no las ediciones de precio desde Productos.
