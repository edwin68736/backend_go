# F1 — Catálogo de insumos: diagnóstico y propuesta

Fecha: 25-nov-2026 · Estado: **PROPUESTA; no se modificó código.** Espera autorización expresa. Marco: `PROPUESTA_INSUMOS_RECETAS_TUKICHEF.md` v3.1.
Basada en un barrido de solo lectura (sin ejecutar nada). Rutas relativas a `backend_principal` salvo que se indique.

## 1. Qué verá el usuario al terminar F1
- En Tukichef, una pantalla **Insumos** (solo si se activa para el negocio): lista, alta y edición, unidad base (g, ml, und), unidades de compra (kg, L, saco, caja), stock por sucursal, stock mínimo y costo inicial.
- Los insumos **no aparecen** en el POS, mesas, carta, tienda virtual, cotizaciones ni combos, y **no se pueden vender** aunque alguien envíe su id.
- **No cuentan** en el cupo de productos del plan.
- Sí aparecen donde se compran y mueven: compras, órdenes de compra, ingresos, ajustes, traslados y kardex.

**No incluye:** recetas, precio sugerido, costo promedio (F2), compras desde Tukichef ni carga inicial (F3), descuento al cobrar (F5).

## 2. Hallazgos que cambian el diseño de la v3.1
1. **No basta "ocultar insumos".** Compras y documentos de inventario usan el mismo selector y el mismo `GET /api/products` que la venta. Se necesita un parámetro de tres estados: `supplies = exclude` (por defecto) · `only` · `include`.
2. **El cobro de mesa no pasa por `SaleService.Create`** (`restaurant_service.go` ~1318-1396): necesita su propio rechazo.
3. **El escáner (`lookup-by-code`) no usa la consulta común**: escanear un insumo lo agregaría al carrito.
4. **El kardex y los reportes de Tukichef filtran `is_restaurant = true`**: tal como están, nunca mostrarían insumos.
5. **El conteo del cupo ignora el error de la consulta** (`pkg/saas/quota.go:134`): si se filtra por `is_supply` en un tenant sin migrar, devolvería 0 y **desactivaría el cupo**. Se requiere sondear la columna (como ya hace `categorybranch`), también en los listados.
6. **Un alta con el código de un producto borrado lo reactiva**, y la importación Excel actualiza por código: un insumo podría convertirse en plato o al revés.

## 3. Cambios

### 3.1 Base de datos — migración V195 (aditiva)
| Columna | Detalle |
|---|---|
| `tenant_products.is_supply` | bool, por defecto `false`; se trata `NULL` como `false` |
| `tenant_products.supply_kind` | `'raw'` (reservado `'prepared'` para producción V2) |
| `tenant_restaurant_settings.recipes_enabled` | bool, por defecto `false` |

**Cambio respecto a la v3.1:** las tablas de recetas, líneas, consumos y reversas **no se crean en F1**, sino cuando se usen (F4 y F5). Evita esquema sin uso en cientos de tenants.

### 3.2 Reglas del insumo (en el servicio, no solo en la pantalla)
- `is_supply` se fija al crear y **no se puede cambiar**. Convertir permitiría saltarse el cupo y rompería anulaciones, comandas abiertas, cotizaciones, combos y pedidos web.
- Se fuerza: `type=product`, `is_restaurant=false`, no publicado, `manage_stock=true`, precio de venta 0, sin series, variantes, modificadores, combos ni área de preparación.
- Reactivar por código o importar Excel **falla** si cambiaría la clase (insumo ↔ producto).
- No exige ni consume cupo.

### 3.3 Dónde no deben verse
- Consulta común (`buildListQuery`): excluye por defecto. Cubre POS Tukifac y Tukichef, carta, selectores y tienda.
- Consultas aparte: `lookup-by-code`, tienda pública (rango de precios, categorías, galería, opciones), conteos por categoría/marca/área, candidatos a combo, contador del dashboard.
- El filtro **no depende** de que el módulo esté activado.

### 3.4 Rechazos de servidor (un solo helper y un mensaje)
`SaleService.Create` (cubre POS, conversión de NV y cotización, pedidos web, membresías, extras de combo) · `BillTable` · pedidos y comandas (`resolveRestaurantOrderItem`) · cotizaciones · pedido web público · componentes de combo · actualización masiva (publicar, precio de venta) · precios por sucursal o por unidad · compra con "actualizar precio de venta" · borrado masivo (no purgar un insumo con movimientos).
Compras, órdenes, documentos de inventario y traslados **sí** los aceptan.

### 3.5 Dónde sí deben verse
Enviar `supplies=include` en: selector de compra y orden de compra, documentos de inventario, selector de kardex y traslados, pantalla de inventario y ajuste (Tukifac). Kardex y reporte de stock de Tukichef: permitir insumos.

### 3.6 API y permisos para Tukichef
- Rutas nuevas bajo `/api/restaurant/supplies` (lista, alta, edición, existencias, movimientos, unidades), que reutilizan los servicios de productos e inventario. Así el personal con PIN puede usarlas (las rutas de productos de hoy exigen permisos de cuenta y devuelven 403 con PIN).
- Permisos nuevos de restaurante: `supplies.view`, `supplies.manage`, `recipes.view_cost`. Por defecto, administrador y supervisor.
- El costo del insumo solo se devuelve con `recipes.view_cost` (hoy el filtro de costos solo cubre `purchase_price`).
- Unidades de compra: permitir precio 0 en insumos (hoy exige precio > 0).
- Activación: `recipes_enabled` lo enciende superadmin; se publica en las capacidades como `restaurant_recipes` (el marcador ya existe y hoy es siempre `false`).

### 3.7 Frontend
- **Tukichef:** menú y ruta "Insumos" (condicionados a `restaurant_recipes` y al permiso), pantalla, editor de unidades (portado de Tukifac).
- **Tukifac:** solo enviar `supplies=include` en compras e inventario y mostrar la etiqueta "Insumo". La gestión de insumos vive en Tukichef.

## 4. Entrega en dos partes
| Parte | Contenido | Por qué separada |
|---|---|---|
| **F1a — Backend** | V195, reglas, filtros, rechazos, cupo, rutas, permisos y activación | Es lo que protege las ventas; se prueba solo |
| **F1b — Pantallas** | Tukichef y los ajustes en Tukifac | Depende de F1a |

## 5. Archivos principales
`pkg/database/migrations.go`, `tenantmigrations` (V195), `schema_features.go`; `internal/products` (service, handler, barcode, combo, importaciones, masivos); `pkg/saas/quota.go`; `internal/sales`, `internal/restaurant` (servicio, rutas, modificadores), `internal/quotations`, `internal/ecommerce`, `internal/purchases`, `internal/dashboard`, `internal/inventory`; `pkg/restaurantperm`. Frontends: `restaurantNav.ts`, `App.tsx`, `restaurantPermissions.ts`, `FeatureContext.tsx`, pantalla y servicio de insumos, selectores de compra e inventario de Tukifac.

## 6. Pruebas y criterios de aceptación
**Prueba integral:** crear un insumo y comprobar que
1. no aparece en `/api/products` (todas las variantes: búsqueda, categoría, sucursal, paginado y total), ni con el escáner, ni en la tienda pública;
2. los conteos y el cupo no cambian, y con el cupo lleno se puede crear un insumo pero no un producto;
3. venta, comanda, POS directo, cobro de mesa con comanda inyectada, cotización, pedido web, combo, importación y actualización masiva lo **rechazan** sin cambios parciales;
4. **sí** aparece en compra, ingreso, ajuste, traslado, kardex y pantalla de insumos;
5. el personal con PIN entra con permiso y recibe 403 sin él; sin `recipes.view_cost` no ve costos;
6. con un tenant **sin migrar**, listados y cupo funcionan como hoy;
7. un tenant que no usa insumos no nota ningún cambio (prueba dorada de listados y ventas).

En SQLite, MariaDB estricto y, cuando haya instancia, MySQL 8; suite completa en verde.

## 7. Riesgos
- **Fuga de un insumo a la venta** por un camino no cubierto: se mitiga con el rechazo en servidor además del filtro.
- **Tenants sin migrar:** sondeo de columna y `COALESCE` (hallazgo 5).
- **Visibilidad por sucursal:** hoy un producto con stock solo en la sucursal A no aparece en B; para insumos no se debe filtrar así (decisión 3).
- **Códigos repetidos** entre producto e insumo vuelven ambigua la importación: los códigos deben ser únicos entre ambos.
- **Costos expuestos** en kardex, compras y documentos (`unit_cost`): F1 protege las rutas nuevas; las existentes se protegen en F2/F3, antes de que los insumos tengan costos reales.

## 8. Dependencias y despliegue
- F1 depende de F0a (numeración V195 después de V193/V194 y creación segura de stock). **F0a está sin desplegar**, así que F1 se puede desarrollar en local, pero no desplegar antes que F0a.
- Despliegue con migración: backend primero y luego frontends; requiere la red entre VPS resuelta.

## 9. Decisiones que necesito (con mi recomendación)
1. **`is_supply` inmutable** tras crear. Recomendado: sí.
2. **Tablas de recetas y consumos fuera de F1.** Recomendado: sí.
3. **Insumos visibles en todas las sucursales** (con su stock, aunque sea 0), sin el filtro por sucursal de los productos. Recomendado: sí.
4. **Categorías:** en F1 los insumos sin categoría de carta, para no mostrar categorías vacías en el POS. Recomendado: sí.
5. **Stock bajo y reportes de inventario** incluyen insumos rotulados; el contador de productos del dashboard no. Recomendado: sí.
6. **Gestión solo en Tukichef**; Tukifac solo los ve en compras e inventario. Recomendado: sí.
7. **No exigir los módulos `products` ni `inventory`** para insumos (rutas bajo restaurante). Recomendado: sí.
8. **Entregar F1a y F1b por separado**, cada una con tu autorización. Recomendado: sí.

## 10. No verificado
No se ejecutó nada. Que algunos manejadores de páginas antiguas sean código muerto, y que las exportaciones de productos se generen en el navegador, son deducciones. La exposición de costos en endpoints existentes se detalló en `F0a_DIAGNOSTICO_Y_PROPUESTA.md` §3.3.
