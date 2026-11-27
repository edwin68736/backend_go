# F1b — Pantallas de insumos: diagnóstico y propuesta

Fecha: 25-nov-2026 · Estado: **PROPUESTA; no se modificó código.** Espera autorización expresa. Depende de F1a (`dccc1c0`, local).

## 1. Qué verá el usuario
- **Tukichef:** menú **Insumos** (solo si el negocio lo tiene activado y el usuario tiene permiso): lista con stock y aviso de stock bajo,
  alta y edición, unidades de compra (saco, caja, kg), kardex del insumo y, con permiso, su costo.
- **Tukifac:** los insumos aparecen, rotulados «Insumo», en compras, órdenes de compra, ingresos y salidas, traslados e inventario.
- **Panel central:** un interruptor «Insumos y recetas» por empresa.

**No incluye:** carga inicial ni ajustes de stock desde Tukichef, ni compras desde Tukichef (F3); costo promedio (F2); recetas (F4).
El stock de un insumo entra por Tukifac (compra o ingreso) hasta F3.

## 2. Diagnóstico
1. Tukichef ya declara `restaurant_recipes` en `FeatureContext.tsx`, pero nadie lo usa. El menú se filtra **solo por permiso**
   (`canAccess`) en `RestaurantSidebar`, `RestaurantHeader`, `MobileBottomNav` e `InicioPage`: la condición del módulo debe sumarse en un
   único punto (`AuthContext.canAccess`) para no repetirla.
2. `ProductosPage.tsx` tiene 1854 líneas y es de platos (carta, categorías, combos). Insumos va en **pantalla aparte**.
3. Tukifac usa un solo selector (`ProductPickerModal`) para venta y compra; su `variant="purchase"` cubre compra, orden de compra y
   documentos de inventario. Traslados e inventario piden la lista por su cuenta. El kardex filtra por texto en el servidor y **ya incluye
   insumos**: no cambia.
4. La compra de Tukifac **ya admite unidades de compra** (`PurchaseSaleUnitSelector`, sin precio). Órdenes de compra, documentos de
   inventario y traslados trabajan en unidad base.
5. La compra ofrece «Actualizar precio de venta»; con un insumo el servidor la rechaza, así que la casilla debe ocultarse.
6. **Falta en el backend (hueco de F1a):** el superadmin puede activar el módulo, pero no hay cómo **leer** si está activado. Sin eso el
   interruptor del panel no puede mostrar su estado.
7. El editor de unidades de Tukifac (`SaleUnitsModal`, 398 líneas) gira en torno a precios. Para insumos conviene uno propio y corto.

## 3. Cambios

### 3.1 Backend (mínimo)
`GET /api/superadmin/tenants/:id/restaurant-recipes` → `{ enabled, available }` (`available=false` si la empresa no tiene V195).
Permiso `empresas.view`. Sin migración.

### 3.2 Tukichef
| Archivo | Cambio |
|---|---|
| `config/restaurantNav.ts`, `utils/restaurantPermissions.ts`, `contexts/AuthContext.tsx`, `App.tsx` | función `insumos`, permiso `sp.v`/`sp.m` y módulo activado; ruta protegida |
| `services/supplies.service.ts` (nuevo) | llamadas a `/api/restaurant/supplies` |
| `pages/InsumosPage.tsx` (nuevo) | búsqueda, paginado, activos/inactivos, stock, stock bajo |
| `components/supplies/` (nuevo) | formulario, unidades de compra, kardex |
| `reports/definitions/stockReportDefinition.tsx`, `services/products.service.ts` | el reporte de stock pide `supplies=include` y rotula |

Reglas de pantalla: sin `sp.m` es solo lectura; sin `rc.c` no se muestra ni se envía el costo; el costo se rotula **por unidad base**;
la unidad base se bloquea si hay movimientos; las ventanas deben desplazarse en pantallas bajas (celular y tablet).

### 3.3 Tukifac
| Archivo | Cambio |
|---|---|
| `services/products.service.ts` | opción `supplies=include`; campo `is_supply` |
| `components/sales/ProductPickerModal.tsx` | solo con `variant="purchase"`: incluir y rotular |
| `pages/purchases/PurchaseRegisterPage.tsx` | ocultar «Actualizar precio de venta» en insumos |
| `pages/inventory/InventoryTransfersPage.tsx`, `InventoryPage.tsx` | incluir y rotular; sin acciones de venta en la fila |

Ventas, POS, guías, notas, productos y reportes de ventas **no** se tocan.

### 3.4 Panel central
`services/tenants.service.ts` y la ventana de módulos de `TenantsPage.tsx`: interruptor aparte «Insumos y recetas», deshabilitado con
explicación si la empresa no está migrada.

## 4. Entrega
| Parte | Contenido | Commit |
|---|---|---|
| F1b-1 | Backend (lectura del estado), panel central y Tukichef | uno por repositorio |
| F1b-2 | Tukifac | uno |

Con F1b-1 ya se puede activar una empresa y dar de alta insumos; F1b-2 permite meterles stock por compra. Van seguidas salvo que quieras
revisar entre ambas.

## 5. Pruebas
- `tsc` y build de los tres frontends; pruebas unitarias del acceso al menú (sin módulo, sin permiso, solo lectura, sin costo).
- Recorrido en el navegador contra backend local: activar en central, crear insumo y unidad, comprarlo en Tukifac, ver stock y kardex en
  Tukichef, y comprobar que **no** aparece en POS, mesas ni venta.
- Pantallas bajas y ancho de celular.
- Con el módulo apagado, Tukichef y Tukifac se ven como hoy.

Para el recorrido hay que aplicar V195 a la BD **local**.

## 6. Riesgos
- Un frontend nuevo contra un backend sin F1a: el módulo no se anuncia, así que no se muestra nada; el parámetro `supplies` se ignora.
- Apps Tukichef ya instaladas: no ven la pantalla hasta compilar una versión nueva.
- Quien active el módulo debe recargar la app (las capacidades se leen al iniciar sesión).
- Un negocio sin acceso a compras o inventario de Tukifac no podrá cargar stock hasta F3.
- El costo que escribas en Tukichef **lo reemplaza** la siguiente compra (comportamiento actual de las compras).

## 7. Despliegue (no autorizado)
Requiere desplegado antes: F0a y F1a (backend con `migrate-fleet`). Orden: backend, central, Tukifac, Tukichef web y apps.

## 8. Decisiones que necesito
1. **Incluir el interruptor en el panel central** y el endpoint de lectura. Recomendado: sí.
2. **Sin carga ni ajuste de stock desde Tukichef** en F1b. Recomendado: sí (es F3, con costos).
3. **Insumos fuera del selector de guías de remisión.** Recomendado: sí, hasta que alguien necesite trasladarlos con guía.
4. **Versión de Tukichef 1.6.0**, puesta al final, antes de que compiles. Recomendado: sí.

## 9. No verificado
Solo se leyó código; no se ejecutó nada. Falta comprobar en pantalla las acciones por fila de `InventoryPage` y que la ventana de módulos
del panel admita un interruptor que no es un módulo del plan.
