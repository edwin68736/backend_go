# Insumos y recetas para Tukichef — propuesta v3.1 (gestión profesional de restaurante)

Fecha: 24-nov-2026 (v3) · 25-nov-2026 (v3.1: decisiones confirmadas y precisiones A–E, ver §20) · Estado: **PROPUESTA aprobada en arquitectura y alcance; nada implementado** (ni código, ni migraciones, ni BD, ni producción) · Reemplaza a la v2 y es autocontenida.

> **Regla de trabajo vigente (dueño, 25-nov-2026):** una fase a la vez; por cada fase: diagnóstico → archivos y componentes → riesgos y dependencias → propuesta concreta → **esperar autorización expresa** → implementación limitada → pruebas (unitarias, integración, regresión; MySQL estricto y MariaDB) → auditoría de efectos secundarios → documentación y commit independiente. Sin avance automático, sin despliegues ni cambios en producción sin autorización expresa. F0a y F0b se implementan **por separado**, cada una con observación independiente.
Fuentes: sistema antiguo (`C:\xampp\htdocs\facturador-tukifac`) y cuatro auditorías de solo lectura de nuestro código (modelo/inventario, flujos de venta y reversa, costos/unidades/activación, reutilización de componentes). `[V]` = verificado leyendo código; `[I]` = inferido.

**Idea rectora.** No es "un mecanismo para descontar ingredientes": es un módulo de gestión de insumos que se apoya en el inventario, las compras y el catálogo que ya existen. **No se crea un segundo sistema de inventario ni se duplica ningún módulo.**

---

## 1. Alcance: V1 y V2

### 1.1 V1 (incluye lo que exige la operación profesional)

| Área | Funcionalidad V1 |
|---|---|
| Catálogo de insumos | Pantallas, filtros, existencias por sucursal, costos, movimientos, unidades de compra; independiente del catálogo de productos |
| Proveedores | Alta y listado de proveedores (contactos tipo `supplier`) dentro de Tukichef |
| Compras de insumos | Compra con proveedor y comprobante, unidades de compra (kg, saco, caja, L, und) con conversión a base, actualización de stock y costo promedio, historial |
| Ingreso manual | Ingreso sin compra formal (documento de inventario de ingreso) con costo |
| Carga inicial | Carga de insumos con existencias y costo, idempotente |
| Recetas | Receta de un nivel con merma por línea y rendimiento; pestaña **Receta** en el plato |
| Costeo | Costo por ingrediente, total de la receta, por porción, merma, food cost, margen bruto; recalculado con los costos vigentes sin tocar ventas históricas |
| **Precio sugerido** | Por food cost objetivo y por margen deseado, comparación con el precio actual, redondeo comercial, nunca automático (§6) |
| Disponibilidad | "Platos posibles" por receta y sucursal, con el ingrediente limitante (§7) |
| Consumo al cobrar | Descuento de insumos en POS rápido y cobro de mesa, con reversa en anulaciones y NC, exactamente una vez (§9) |
| Reportes | Existencias y valorización, historial de compras, consumo de ingredientes, costo por plato, food cost, margen, productos de mayor costo, insumos de mayor consumo, disponibilidad (§11) |
| Seguridad | Activación gradual por tenant, apagado sin pérdida de datos, permisos con puente de restaurante |

### 1.2 V2 (el modelo ya queda preparado en V1)

| Área | Funcionalidad V2 |
|---|---|
| Preparaciones intermedias | Registro de producción de salsas/bases con costo de producción, stock de preparados y uso como ingrediente (§8) |
| Subrecetas | Receta de más de un nivel (insumo "preparado") |
| Recetas por presentación | Receta distinta por tamaño/presentación comercial (§10.3) |
| Modificadores y extras | Agregar/quitar/sustituir ingredientes por opción |
| Mermas | Reporte de mermas de preparación y almacenamiento; conteos de inventario |
| Consumo teórico vs real | Comparación con conteos físicos |
| Costos adicionales | Empaque, gas, mano de obra por porción |
| Rendimiento de compra | Pérdida por limpieza/pelado aplicada al costo |
| Importación | Excel de insumos y recetas |

---

## 2. Decisiones definitivas

| # | Decisión |
|---|---|
| D1 | Insumo = `tenant_products` con `is_supply=true`, pero **catálogo funcionalmente independiente** (§3) |
| D2 | Consumo **al cobrar**, dentro de la transacción de la venta |
| D3 | Política de stock insuficiente por tenant: `allow` (por defecto, con alerta) o `block` |
| D4 | Los insumos no cuentan en el cupo de productos |
| D5 | Plato con receta activa no descuenta su stock directo (ni el de la presentación) |
| D6 | V1: receta de un nivel; modelo preparado para niveles, presentaciones y modificadores |
| D7 | Activación independiente de `inventory` (flags en la BD del tenant) |
| D8 | Unidad base g/ml (y unidad para conteos); kg/L/sacos/cajas por conversión |
| D9 | **Ledger propio de consumos** con unicidad en base de datos y snapshot de la receta |
| D10 | Los movimientos de consumo usan la referencia `RECETA/<nro>` (no `VENTA/`), para no cruzarse con las reversas genéricas |
| D11 | Costo promedio ponderado persistido **solo para insumos** |
| D12 | Costo de compra de insumos **sin IGV** |
| **D13** | **Food cost y margen se calculan sobre el precio neto (sin IGV)**; el precio publicado se obtiene sumando IGV según la afectación del producto |
| **D14** | **El precio de venta jamás se modifica solo**: el sistema sugiere y el usuario confirma; cada cambio queda auditado |
| **D15** | Las compras de insumos reutilizan `PurchaseService` (proveedores, moneda, IGV, cuentas por pagar); el ingreso manual reutiliza documentos de inventario. Ambos con **puente de restaurante** nuevo |
| **D16** (revisada tras auditoría) | El costo histórico de la receta se guarda en un **campo independiente** (`tenant_sale_items.recipe_cost`, nullable), **no** en `purchase_price`, para no alterar el significado actual de ese campo (ver §20.1). `ProfitDetail` lo leerá con `COALESCE(recipe_cost, purchase_price, p.purchase_price, 0)` en la fase de consumo |
| **D13/D15/D-redondeo/D-permisos** | **Confirmadas por el dueño el 25-nov-2026**: food cost y margen sobre precio neto (la interfaz muestra el neto y el publicado con IGV); reutilizar `PurchaseService`, proveedores y documentos de inventario sin segundo sistema de compras (documentando limitaciones de módulo y caja abierta); redondeo comercial hacia arriba a S/ 0,10 por defecto, configurable por tenant; permiso independiente `recipes.view_cost` |
| **D17** | Las preparaciones (V2) se construyen sobre el diseño ya documentado de Producción (`docs/produccion`: `StockGateway`, un solo kardex, snapshot de receta), no como un módulo paralelo |

---

## 3. Separación de productos e insumos

Comparten tabla pero se comportan como catálogos distintos.

| | Productos (vendibles) | Insumos |
|---|---|---|
| Qué son | Platos, bebidas, combos, artículos | Ingredientes y materiales de receta |
| Marca | `is_supply=false` | `is_supply=true`, `is_restaurant=false`, `manage_stock=true`, sin presentaciones |
| Pantallas | Productos, Carta, POS | **Insumos** (propia): lista, alta/edición, existencias, costos, movimientos, unidades de compra |
| Precio de venta | Sí | No |
| Cupo del plan | Cuenta | **No cuenta** |
| Aparece en POS, carta digital, e-commerce, buscadores de venta, cotizaciones | Sí | **Nunca** |

**Garantías (servidor, no solo interfaz):**
1. `buildListQuery` (base común de `/api/products`) excluye insumos por defecto; el parámetro `SuppliesOnly` se usa solo en pantallas de insumos y de compras `[V]`.
2. Rechazo explícito de `is_supply` en `SaleService.Create`, `resolveRestaurantOrderItem`, `quotation_calc` y alta de comandas (hoy ninguno valida el producto `[V]`).
3. Las consultas directas que no pasan por la base común se revisan una por una: e-commerce (`PriceBounds`, `PublicCategories`, `convert.go`, `print_data.go`), candidatos de combos, `StockSummary`, kardex, dashboards, inventario y operaciones masivas.
4. Un insumo no puede ser componente de un combo ni plato de carta (`is_restaurant` bloqueado).
5. Prueba automática: un insumo creado por la API no aparece en ningún listado de venta y cualquier intento de venderlo devuelve error.

---

## 4. Compras e ingresos de insumos

### 4.1 Compra formal (reutiliza `PurchaseService`) `[V]`
- **Proveedor:** es `TenantContact` tipo `supplier` o `both` (permisos separados `suppliers.*`). Tukichef solo crea clientes hoy; se agrega `contactType` al modal (patrón ya presente en Tukifac) y una pantalla de proveedores copiada de `SuppliersPage`. Backend: nada nuevo.
- **Registro:** `POST /purchases` ya resuelve proveedor, comprobante del proveedor (serie/número obligatorios), moneda y tipo de cambio, IGV por línea, contado/crédito (cuenta por pagar) y pagos mixtos.
- **Unidades de compra:** cada línea trae `SaleUnitID` (kg, saco 50 kg, caja 12 und…); `saleunit.ResolveForLine` convierte cantidad y costo a la unidad base y el kardex guarda el snapshot (unidad, factor) `[V]`.
- **Stock y costo:** entra el stock a la sucursal; se actualiza `PurchasePrice` (último costo) y, para insumos, el **costo promedio ponderado** (§5) con costo sin IGV.
- **Historial:** cada línea queda en `TenantPurchaseItem` y el movimiento referencia `purchase_item_id`; el reporte de historial por insumo es nuevo (consulta sobre esas tablas).
- **Anulación:** `Void` hace una salida al costo original; falla si el insumo ya se consumió (se documenta y se ofrece ajuste).

### 4.2 Ingreso manual (sin compra formal)
Documento de inventario `OTHER_IN` / `INVENTORY_ADJUSTMENT_IN` (borrador → confirmar → anular, con serie de inventario por sucursal) con **costo por unidad base**; no exige proveedor, comprobante ni caja `[V]`. Salidas manuales de merma ya existen: `SHRINKAGE`, `WASTE`, `DESTRUCTION`, `WITHDRAWAL` `[V]`.

### 4.3 Lo que falta hoy y se resuelve con puentes `[V]`
- Compras y documentos de inventario **no tienen puente de restaurante**: un staff por PIN no puede usarlos. Se crean `RequireSuppliesPurchaseAccess` y `RequireSuppliesStockAccess` (patrón de `sales_access.go`/`inventory_access.go`, con `HasRestaurantPerm`), no `RequirePermission` a secas.
- Compras exigen los módulos `purchases`, `products`, `cashbank` y **caja abierta incluso a crédito** `[V]`; Tukichef ya requiere `cashbank`. Se mantiene (no se duplica la regla). Si un tenant no tiene el módulo `purchases`, solo queda el ingreso manual.
- Documentos de inventario exigen el módulo `inventory` `[V]`; por D7 los endpoints de insumos van bajo el grupo `restaurant` y llaman a `InventoryService`, de modo que no se exige `inventory`.
- Permisos nuevos de restaurante: `supplies.view`, `supplies.manage`, `supplies.purchase`, `recipes.manage`, `recipes.view_cost` (el cocinero puede ver recetas sin ver costos; los costos son información sensible).

### 4.4 Qué se reutiliza y qué vive en Tukichef

| Componente | Origen | Acción |
|---|---|---|
| Servicio de compras, cuentas por pagar, proveedores (backend) | `internal/purchases`, `payables`, `contacts` | **Reutilizar tal cual**; agregar solo puente de permisos |
| Documento de inventario (ingreso/egreso) | `inventory_document_service.go` | Reutilizar; ensanchar `unit_cost` a (15,6) |
| Pantalla de compras | `PurchaseRegisterPage.tsx` (1173 líneas, monolito) | **No copiar entera**: formulario reducido de "Compra de insumos" en Tukichef reutilizando `PurchaseSaleUnitSelector`/`UnitQuantityInput` copiados |
| Editor de líneas de ingreso | `InventoryDocumentHeader/Lines/Toolbar`, `ProductServerSelect` | Copiar el patrón |
| `SaleUnitsEditor` | Tukifac | **Portar a Tukichef** (hoy no tiene soporte de unidades de venta/compra) |
| `QuickContactCreateModal` / `SuppliersPage` | Tukifac | Copiar con `contactType` |
| `ComboGroupsEditor` | Tukichef | Modelo del editor de líneas de receta |
| Framework de reportes (`TableReportDefinition`, `useTableReportRunner`, `exportExcel`) | Tukichef | **Reutilizar** para todos los reportes nuevos; clonar `stockReportDefinition`/`kardexReportDefinition` |
| `ProductImportModal`, `ImportPlanLimitNotice`, `StockAdjustmentModal` | Tukichef | Reutilizar/adaptar (importación en V2) |
| `FeatureContext.restaurant_recipes` / `schema_features.go` | ambos | Marcador **ya existe** (siempre `false`, sin consumidores): se conecta |
| Navegación | `restaurantNav.ts` | Ampliar la unión `NavFeature`, `NAV_GROUPS` y el tipo de `RequireFeature` en `App.tsx` (dos copias del tipo) |

No existe paquete compartido entre frontends (todo es copia divergente `[V]`); no se crea ahora, se acuerda aparte.

---

## 5. Costos e inventario

1. **Costo promedio ponderado persistido** en `tenant_product_stocks.avg_unit_cost`, solo insumos, actualizado dentro del bloqueo de fila de `RecordMovementTx`: entrada con costo > 0 → `avg = (avg·qty + costo·q) / (qty + q)`; salida → no cambia y se registra con ese costo. Backfill único con `WeightedAverageUnitCosts`.
2. **Sin IGV**: para insumos, el costo de kardex es neto (si la línea es gravada y el precio incluye IGV, se divide por 1+IGV). No se modifica el costo de productos normales (hoy incluye IGV según el checkbox `[V]`).
3. **Entradas con costo 0** (traslado, devolución, stock inicial sin costo, ajuste) no alteran el promedio; si aún no hay promedio quedan "sin costo" y se avisa.
4. **Histórico inmutable**: cada consumo y reversa guarda su `unit_cost`; cambiar el costo de un insumo **no reescribe** costos de ventas pasadas (D16).
5. **Conversiones**: kg↔g, L↔ml solo por `TenantProductSaleUnit` (factor 1000 o 0,001). `price1>0` es obligatorio hoy en una `SaleUnit` y se relaja para insumos.
6. **Precisión**: cantidades `decimal(15,3)`, costos `decimal(15,6)`, receta `decimal(15,6)`. `unit_cost` de documentos de inventario es (15,2) y truncaría S/0,0042 por g → migración a (15,6). Cálculo con aritmética decimal (no `float64`); redondeo a 3 decimales solo al registrar.
7. **Mermas y rendimiento**: ver §10.
8. **Consistencia stock–kardex–ledger**: hoy no existe ningún chequeo `[V]`; se agrega un reconciliador de solo lectura (por `id` del movimiento, no por fecha) usado como oráculo de pruebas y job nocturno.

---

## 6. Recetas, costeo y precio de venta sugerido

### 6.1 Pestaña Receta (V1)
Al crear/editar un plato aparece la pestaña **Receta**: agregar ingredientes (insumos), cantidad neta (en g/ml/und, mostrable en kg/L), merma %, y el rendimiento de la receta (porciones). Por cada ingrediente el sistema muestra automáticamente:

| Dato | Fórmula |
|---|---|
| Cantidad bruta a consumir | `qty_neta / (1 − merma%)` |
| Costo unitario del insumo | `avg_unit_cost` vigente (opción `recipe_cost_basis`: promedio o último costo) |
| Costo de la línea | `cantidad_bruta × costo_unitario` |
| Costo de merma (informativo) | `(cantidad_bruta − qty_neta) × costo_unitario` |
| **Costo total de la receta** (= costo total de preparación) | `Σ costo de líneas` |
| **Costo por porción** | `costo_total / rendimiento` |
| **Food cost actual** | `costo_porción / precio_neto_actual` |
| **Margen bruto (S/)** | `precio_neto_actual − costo_porción` |
| **Margen bruto (%) sobre ventas** | `margen / precio_neto_actual` |

El costo se recalcula **en lectura** con los costos vigentes; las ventas pasadas conservan su snapshot (D16).

### 6.2 Precio de venta sugerido (V1, explícito)
**Margen sobre ventas ≠ recargo sobre costo**, y se muestran por separado para evitar errores:

| Concepto | Fórmula | Equivalencia (ejemplo FC 30 %) |
|---|---|---|
| Food cost % | `C / P` | 30 % |
| Margen sobre ventas | `(P − C) / P = 1 − FC` | 70 % |
| Recargo sobre costo (markup) | `(P − C) / C` | 233,3 % |

- **Precio sugerido por food cost objetivo:** `P_neto = C / FC_objetivo`.
- **Precio sugerido por margen deseado (sobre ventas):** `P_neto = C / (1 − margen)`.
- **Precio publicado:** si el producto es gravado y `PriceIncludesIgv`, `P_pub = P_neto × (1 + IGV)`; si es exonerado/inafecto no se suma. Luego se aplica el **redondeo comercial** configurado.
- El recargo sobre costo se muestra solo como dato informativo derivado; **no es una entrada de cálculo** (la entrada es FC o margen sobre ventas).

**Ejemplo del requerimiento.** Costo de receta `C = S/ 4,15`, FC objetivo 30 %:
- `P_neto = 4,15 / 0,30 = S/ 13,83` (este es el precio **sin IGV**, el que coincide con tu ejemplo).
- Si el plato se publica con IGV incluido: `13,83 × 1,18 = S/ 16,32`; con redondeo comercial al alza a 0,10 → **S/ 16,40** (FC real = 4,15 / (16,40/1,18) = **29,86 %**, margen 70,14 %). Con redondeo a `x,90` → S/ 16,90; a 0,50 → S/ 16,50; a 1,00 → S/ 17,00. La pantalla muestra siempre el FC y margen **reales** resultantes del precio redondeado.
- Decisión D13: la base es el neto. Si prefieres que el 13,83 sea el precio publicado (FC sobre precio con IGV), se cambia el parámetro `fc_basis` a `gross`; recomiendo `net` porque el IGV no es ingreso del negocio.

**Comparación y configuración**
- Pantalla comparativa: precio actual vs sugerido (neto y publicado), FC y margen actuales vs objetivo, diferencia en S/ y %, con alerta si el FC actual supera el objetivo.
- Configuración (en `tenant_restaurant_settings`, con anulación opcional por receta): `target_food_cost_pct` (30 % por defecto), `target_margin_pct`, `price_rounding` (`none` | `0.10` | `0.50` | `1.00` | `x.90`), `rounding_direction` (`up` por defecto | `nearest`), `fc_basis` (`net`).
- Precios por sucursal: el cálculo usa el precio vigente de la sucursal activa (`TenantProductBranchPrice` si existe).
- **Nunca se actualiza automáticamente** (D14): el botón "Aplicar precio sugerido" pide confirmación explícita, muestra precio anterior y nuevo, y escribe en el precio publicado mediante el servicio existente de productos (`bulk_update_prices` / `PUT /products/:id`, que hoy exige `products.edit` y no pasa por el puente de PIN `[V]`). Se agrega un puente de restaurante para administradores y una **tabla de auditoría de cambios de precio** (usuario, fecha, precio anterior/nuevo, motivo "sugerido por receta"), porque hoy no existe historial de precios `[V]`.
- Si cambian los costos de insumos, la pantalla marca "costo cambió; el precio sugerido ahora es X" sin tocar nada.

---

## 7. Disponibilidad de platos según existencias (V1)

Cuántas porciones de un plato se pueden preparar con el stock **de la sucursal**:

`disponibles = floor( min sobre ingredientes ( stock_insumo / cantidad_bruta_por_porción ) )`

Ejemplo: hamburguesa con 150 g de carne y 2 kg (2000 g) → `floor(2000 / 150) = 13` por disponibilidad de carne. Si otro ingrediente alcanza para menos (pan: 8), el plato muestra **8** y señala el **ingrediente limitante**.

Reglas: considera **todos** los ingredientes; stock negativo o cero cuenta como 0; combos = mínimo de sus componentes; se calcula en lectura (sin tabla caché mezclada con reservas, error del sistema antiguo); la disponibilidad de cada plato es **independiente** (dos platos que comparten carne no suman); usa la cantidad bruta (con merma). Se expone en la lista de Productos, en un reporte y como indicador de solo lectura en el POS; **no bloquea la venta** en V1 (la política de stock decide). `checkOrderItemStock` hoy solo valida productos/presentaciones y no ve insumos `[V]`: en V1 se agrega una advertencia en cocina/POS.

---

## 8. Preparaciones intermedias y producción (V2, modelo preparado desde V1)

Ejemplo: comprar tomate, cebolla y otros; receta de **salsa**; producir 10 L; descontar ingredientes; registrar costo de producción; subir el stock de "salsa elaborada"; usarla como ingrediente de otras recetas.

**Cómo se diferencia del descuento directo por venta**

| | Consumo directo por venta (V1) | Producción de preparación (V2) |
|---|---|---|
| Cuándo | Al cobrar un plato | Al registrar una producción |
| Qué descuenta | Insumos crudos según receta del plato | Insumos crudos según receta de la preparación |
| Qué aumenta | Nada | Stock de la preparación (producto `supply_kind='prepared'`) |
| Costo | Costo promedio del insumo al vender | Costo de producción ÷ cantidad producida → costo promedio de la preparación |
| Documento | Línea de venta | Registro de producción |

**Modelo (alineado con la ADR de Producción ya escrita, no aprobada)**
- Preparación = producto `is_supply=true`, `supply_kind='prepared'` con **receta propia** (`yield_qty`/unidad de salida: 10 L).
- `tenant_production_runs` (preparación, sucursal, receta y versión, cantidad planificada, **cantidad producida**, estado, costo total, costo unitario, usuario) y `tenant_production_run_inputs` (insumo, cantidad, costo, movimiento). Movimientos por `RecordMovementTx`: salidas de insumos (`PRODUCTION_OUT`, ya existe) y entrada de la preparación (`PRODUCTION_IN`, nuevo), todo en una sola transacción y con `source_type/source_id` genérico.
- Costo de producción = Σ insumos reales ÷ unidades buenas; la diferencia entre rendimiento esperado y real es la **merma de preparación**.
- Una receta puede usar una preparación como ingrediente (`component_product_id`): al vender se descuenta **stock de la preparación** (modo `stocked`) o, para preparaciones que no se producen aparte, se **explota su receta** al vender (modo `explode`). Validación de ciclos y profundidad máxima.
- Se reutilizan las decisiones de la ADR: un solo kardex, tablas propias de producción, snapshot de receta, costo de gestión; sin variantes en V1 de producción. Se respeta que el módulo `manufacturing` está definido pero no implementado: las preparaciones serán su primera rebanada, **sin crear un módulo paralelo** (D17).

**Estructuras que se preparan en V1:** `supply_kind`, `component_product_id` en las líneas, receta indexada por producto (un insumo puede tener receta), `yield_qty`, tipos de operación `PRODUCTION_IN/OUT`, `source_kind` en el ledger. V1 solo admite `supply_kind='raw'`.

---

## 9. Consumo de insumos al cobrar: reglas, flujos e "exactamente una vez"

### 9.1 Reglas de negocio
1. Consumo por línea: `consumido = round3(cantidad_cobrada_base × qty_neta / rendimiento / (1 − merma%))`, calculado sobre la **línea completa** (acota el error a 0,0005 g por línea e insumo); el mismo valor se guarda en movimiento y ledger.
2. La receta se lee **siempre en el servidor**; ningún dato del cliente decide el consumo.
3. Plato con receta activa y consumo encendido: no descuenta stock directo (D5, incluye stock de presentación). Sin receta o con consumo apagado: comportamiento actual.
4. Combos: se consume la receta de cada componente (× cantidad del combo), colgada del `sale_item` del combo (`source_kind='combo_component'`).
5. NV → comprobante: el consumo es de la nota de venta; el comprobante hijo (`SkipInventory`) no consume.
6. Reversa: siempre por el ledger y a costo original, aunque el consumo esté apagado. NC que no devuelve mercadería (motivos 04/05/09) no repone.
7. Insumo agotado: política D3 (`allow` con alerta, o `block` y el cobro falla antes de persistir).
8. Un error inesperado de la función de consumo **aborta la venta completa**; la salida de emergencia es apagar el consumo.

### 9.2 Estructuras de datos
- `tenant_recipe_consumptions` (ledger): `sale_id`, `sale_item_id`, `branch_id`, `supply_product_id`, `source_kind` (`recipe`|`combo_component`|`modifier`), `source_ref_id`, `recipe_id`, `recipe_version`, `sold_qty`, `qty_per_unit_base`, `waste_pct`, `yield_qty` (snapshot), `consumed_qty`, `reversed_qty`, `unit_cost`, `movement_id`. **UNIQUE `(sale_item_id, supply_product_id, source_kind, source_ref_id)`**.
- `tenant_recipe_consumption_reversals`: `consumption_id`, `reversal_ref` (`ANULACION/<nro>` o `NC/<nro>`), `qty`, `unit_cost`, `movement_id`. **UNIQUE `(consumption_id, reversal_ref)`**; cada reversa con `UPDATE ... WHERE reversed_qty + x <= consumed_qty` verificando filas afectadas.
- Movimientos con referencia `RECETA/<nro>`, `OperationCode` `RECIPE_CONSUMPTION` / `RECIPE_REVERSAL`.

### 9.3 Invariantes
- **I1** La venta nace una sola vez (idempotencia + reclamo de sesión + `UPDATE billed_at ... WHERE billed_at IS NULL` verificando filas).
- **I2** Un consumo por línea e insumo, insertado en la **misma transacción** que la venta (UNIQUE).
- **I3** Cada fila del ledger apunta a su movimiento y coincide en cantidad.
- **I4** Reversas acotadas (`reversed ≤ consumed`) e idempotentes (UNIQUE).
- **I5** Conservación: `stock = inicial + entradas − consumos + reversas ± ajustes`, comprobable con el reconciliador.
- **I6** Sin recetas activas, el flujo de venta genera los mismos movimientos que hoy.

### 9.4 Flujo único de consumo
`recipes.ConsumeForSaleTx(tx, sale, saleItems, branchID)` se invoca desde **`SaleService.Create` y desde `BillTable`** (hoy son dos implementaciones del descuento que ya divergen `[V]`; no se duplica la lógica de receta):
1. Si `recipes_enabled` y `recipes_consume` no están encendidos → retorna (una lectura barata).
2. Carga en lote las recetas de los productos (y de los componentes de combos).
3. Calcula consumos por línea; ordena por `supply_product_id` ascendente (orden de bloqueo estable); aplica `FOR UPDATE` y la política D3.
4. Inserta ledger y movimiento `out` con costo promedio; **guarda en `tenant_sale_items.recipe_cost` el costo unitario de la receta de ese momento (por unidad de la línea)** (D16 revisada; `purchase_price` no se toca).
5. Cualquier fallo revierte venta, movimientos y ledger.

### 9.5 Auditoría de cada flujo

| Flujo | Enganche | Reversa | Garantía de una vez | Hueco actual → corrección |
|---|---|---|---|---|
| Venta rápida POS/ERP (`Create`, POS directo) | Tras el bucle de líneas, con `saleItems[i].ID` | Ledger | Clave de idempotencia + UNIQUE ledger + misma transacción | Sin clave (vacía o >64) un doble clic crea dos ventas, cada una consistente → exigir clave en Tukichef |
| Cobro de mesa total (`BillTable`) | Bucle de líneas | Ledger | Idem + reclamo `open→billing` + `FOR UPDATE` de sesión | Doble cobro de la misma comanda con **claves distintas** `[V]` → releer comandas **dentro** de la transacción y `UPDATE ... WHERE billed_at IS NULL AND cancelled_at IS NULL` con `RowsAffected` exacto (**F0b**) |
| Cobro parcial por cantidad | La `sale_item` refleja lo cobrado | Ledger | Consumo sobre `sale_item.quantity` = lo cobrado | Cubierto por la corrección anterior |
| Cobro dividido | Una llamada por venta | Ledger por venta | Suma de ventas = lo cobrado (I1) | Verificar que `comandaSaleLineKey` no mezcle líneas con recetas distintas |
| Combos y componentes | Dentro de la función, por componente | Ledger | UNIQUE con `source_ref_id` | Hoy sus movimientos no llevan `SaleItemID` `[V]`; el ledger no depende de eso |
| Anulación de venta/NV | Misma transacción de `Cancel`/`CancelNotaVenta` | `ReverseForSaleTx` a costo original | UNIQUE de reversas | La anulación actual usa solo el `status` leído antes de la transacción `[V]` → `UPDATE ... WHERE status<>'cancelled'` con `RowsAffected==1` (**F0b**) |
| NC total | `Cancel` ya la invoca | Ledger | Idem | NC total tras parciales descuenta lo ya revertido |
| NC parcial | En `applyPartialCreditNoteSideEffects` | Por línea, con **remanente exacto** | UNIQUE `(consumption_id, reversal_ref)` | El efecto actual no es idempotente para productos normales `[V]` (ticket aparte); para insumos queda resuelto por el ledger |
| NV convertida a comprobante | Consumo en la NV | Anular el hijo cascada a la NV | Sin doble descuento `[V]` | NC parcial sobre el hijo no repone stock hoy `[V]` → mapear línea del hijo a la de la NV; hasta entonces, aviso |
| Cancelar comanda antes del cobro | Ninguno | n/a | No hay consumo (el stock sale solo al cobrar) `[V]` | La validación de pedido no ve insumos → advertencia en V1 |
| Reintentos/duplicados | — | — | Idempotencia + UNIQUE ledger + UNIQUE reversas + rollback atómico | Con clave regenerada vale la corrección de `billed_at` |
| Edición de ventas | No existe `[V]` | — | — | — |

**Demostración.** El consumo ocurre una vez por cantidad cobrada porque (a) la venta nace una sola vez (I1) y (b) el consumo se inserta en la misma transacción atado por UNIQUE a la línea (I2); un reintento choca contra los índices y se revierte entero; las reversas están acotadas e idempotentes (I4). **La garantía depende de cerrar los huecos de F0b y de las pruebas de concurrencia en MySQL; mientras tanto no se afirma.**

---

## 10. Unidades, presentaciones, mermas y rendimiento

### 10.1 Unidades
| Concepto | Modelo |
|---|---|
| Unidad base del insumo | `GRM` (peso), `MLT` (volumen) o `NIU` (conteo: huevos, panes). Catálogo SUNAT ya incluye `GRM`, `MLT`, `KGM`, `LTR` `[V]` |
| kg, L | `SaleUnit` con factor 1000 (`allow_fraction=true`) |
| Paquete, saco, caja | `SaleUnit` con su factor (saco 50 kg = 50 000 g; caja 12 und = 12) |
| Compra | Se elige la unidad de compra; el servidor convierte cantidad y costo a base `[V]` |
| Receta | Se escribe en base (g/ml/und); la pantalla puede mostrar kg/L |
| Conteo físico | En la unidad que el usuario elija, convertida a base |

Cada insumo lleva sus unidades; al marcar `is_supply` se ofrece una **plantilla** (por ejemplo peso: g/kg; líquido: ml/L). Tukichef hoy no tiene soporte de unidades de venta: se porta `SaleUnitsEditor`. Se relaja la regla `price1>0` para insumos.

### 10.2 Mermas y rendimiento (sin doble conteo)
- **Regla:** el stock se guarda **como se compra (bruto)**; la receta expresa lo **servido (neto)**; la merma de preparación se declara por línea.
- Merma de preparación → `waste_pct` por línea. Merma de almacenamiento (vencimiento, rotura) → ajuste con motivo `WASTE`/`SHRINKAGE`/`WASTE_STORAGE`. Rendimiento de preparación → `yield_qty` de la receta. Rendimiento de compra → `yield_pct` del insumo (V2, reservado).
- Cada pérdida se aplica **una sola vez**.

### 10.3 Receta por presentación (V2, estructura en V1)
- `tenant_recipes.presentation_key` (0 = receta del producto, otro = presentación comercial); UNIQUE `(product_id, presentation_key)`.
- Resolución: receta de la presentación si existe; si no, la del producto.
- Hoy una presentación con `ManageStock` descuenta su stock propio y se propaga en comanda y venta `[V]`; con receta activa para esa presentación, el stock directo se ignora (extensión de D5).
- Receta de presentaciones no incluida en V1: el consumo V1 cubre el producto sin presentaciones; los platos con presentaciones usan la receta del producto hasta V2.

---

## 11. Reportes

Todos con el framework de reportes de Tukichef (`TableReportDefinition`, filtros, Excel/PDF) y datos por sucursal.

| Reporte | Fuente | Fase |
|---|---|---|
| **Existencias y valorización de insumos** | `tenant_product_stocks` × `avg_unit_cost` | V1 (no existe hoy `[V]`) |
| **Historial de compras por insumo** | `TenantPurchaseItem` + kardex `purchase_item_id` | V1 |
| **Consumo de ingredientes** | Ledger | V1 (desde que el consumo está encendido) |
| **Costo de preparación por plato** | Receta + costo vigente | V1 |
| **Food cost por producto** | Costo por porción ÷ precio neto (vigente) e histórico (ledger/snapshot) | V1 |
| **Margen de rentabilidad** | Utilidad por plato con `tenant_sale_items.recipe_cost` (D16 revisada), vía `ProfitDetail` con `COALESCE(recipe_cost, purchase_price, p.purchase_price, 0)` | V1 |
| **Productos con mayor costo** | Costo por porción | V1 |
| **Insumos con mayor consumo** | Ledger | V1 |
| **Disponibilidad de platos** | §7 | V1 |
| **Mermas** | Kardex por `WASTE/SHRINKAGE/WASTE_STORAGE` + merma de preparación del ledger | V1 (movimientos) / V2 (mermas de preparación y almacenamiento completas) |
| **Consumo teórico vs real** | Ledger vs conteos físicos (apertura + compras − cierre) | V2 (requiere conteos de inventario) |

---

## 12. Cambios que afectan al inventario y al motor de ventas

**Inventario (afecta a todos los tenants salvo donde se indique)**
1. `RecordMovementTx`: `FOR UPDATE` ordenado, reintento por deadlock, UNIQUE `(product, branch)` (previo: detección de duplicados en solo lectura), opción de saldo negativo (apagada por defecto) → **F0a, impacta a todos**.
2. Costo promedio persistido y `unit_cost` en salidas: **solo insumos**.
3. `unit_cost` de documentos de inventario a (15,6).
4. Tipos de operación nuevos (`RECIPE_CONSUMPTION`, `RECIPE_REVERSAL`, `WASTE_STORAGE`, `PRODUCTION_IN`) con migración que ejecuta el seed.
5. Reconciliador stock–kardex–ledger (solo lectura).

**Motor de ventas**
1. `SaleService.Create` y `BillTable`: llamada a `ConsumeForSaleTx` (solo con flags encendidos).
2. `BillTable`: corrección de doble cobro (releer en transacción, `UPDATE` condicional) → **F0b, impacta a todos**.
3. `Cancel`/`CancelNotaVenta`: `UPDATE ... WHERE status<>'cancelled'` → **F0b, impacta a todos**; `ReverseForSaleTx` en la misma transacción.
4. `applyPartialCreditNoteSideEffects`: reversa de insumos por ledger.
5. Campo nuevo `tenant_sale_items.recipe_cost` (nullable) con el costo histórico de receta (D16 revisada); `ProfitDetail` lo lee primero con `COALESCE`. `purchase_price` conserva su significado actual.
6. Rechazo de insumos en venta, cotización y comanda; `buildListQuery` y consultas directas excluyen insumos.
7. Cupo: excluir insumos en `CountProductSeats` y puntos de importación.
8. **No cambia ningún payload ni XML fiscal**; aun así se prueba por el flujo real.

---

## 13. Activación gradual y seguridad

- **Flags en la BD del tenant** (`tenant_restaurant_settings`): `recipes_enabled` (catálogo, compras de insumos, recetas, costeo, precio sugerido, pantallas; **sin tocar ventas**; lo enciende superadmin), `recipes_consume` (descuento al cobrar; se enciende después de la carga inicial y validación), `recipes_stock_policy`. Se publican en `/api/session/capabilities` como `restaurant_recipes` y efecto inmediato **sin re-login** (no viaja en el JWT). Middleware `RequireRecipesEnabled` en el backend.
- Sin dependencia del módulo `inventory` (endpoints bajo `restaurant`, D7). Las compras reutilizan `purchases` y los permisos nuevos con puente.
- **Interruptor global de emergencia** por variable de entorno.
- **Desactivar:** `recipes_consume=false` detiene consumos nuevos; las **reversas continúan** (dependen del ledger); no se borra ni altera ningún movimiento, consumo ni receta; las ventas hechas con el consumo apagado quedan sin consumo (se documenta la "ventana sin consumo"). `recipes_enabled=false` oculta pantallas; los datos permanecen.
- **Tenants que no lo usan:** migraciones aditivas con valores por defecto; ninguna consulta nueva en venta salvo la lectura de flag; prueba dorada de movimientos idénticos. F0 sí cambia el inventario de todos (por eso va primero y aislada).
- **Carga inicial:** pantalla/Excel con código, nombre, unidad base, unidades de compra, stock por sucursal en base y **costo por unidad base sin IGV**; lote con hash del archivo y llave `(hash, sucursal)` en `tenant_supply_load_batches`, llave por fila `(lote, producto, sucursal)` (reintentos no duplican); movimiento `STOCK_INICIAL` con costo; avg inicial = costo; trazabilidad de lote/usuario/fecha. Se hace con conteo físico previo y **antes** de encender `recipes_consume`.
- **Piloto:** un tenant con reconciliación diaria durante el período de observación antes de abrirlo.

---

## 14. Estructuras de datos a preparar desde el inicio

| Estructura | Se usa en V1 | Reservada para V2 |
|---|---|---|
| `tenant_products.is_supply`, `supply_kind` (`raw`/`prepared`), `yield_pct` | `is_supply`, `raw` | `prepared`, `yield_pct` |
| `tenant_recipes` (`product_id`, `presentation_key`, `yield_qty`, `version`, `active`, `extra_cost`, objetivos opcionales de FC) | producto, rendimiento, versión | presentación, costo adicional |
| `tenant_recipe_lines` (`component_product_id`, `qty_net`, `display_sale_unit_id`, `waste_pct`) | insumos crudos | preparaciones como componente |
| Ledger de consumos y reversas (`source_kind`, `source_ref_id`) | `recipe`, `combo_component` | `modifier` |
| `tenant_product_stocks.avg_unit_cost` | insumos | preparaciones |
| Configuración de precio (`target_food_cost_pct`, `target_margin_pct`, `price_rounding`, `rounding_direction`, `fc_basis`, `recipe_cost_basis`) | sí | — |
| Tabla de auditoría de cambios de precio | sí | — |
| `tenant_supply_load_batches` | sí | — |
| `tenant_production_runs` / `_inputs` | no (se crean en V2) | sí |
| Tipos de operación (`RECIPE_*`, `WASTE_STORAGE`, `PRODUCTION_IN`) | sí | — |
| Flags de configuración y capabilities | sí | `recipes_production` |

Las tablas de catálogo (recetas) no llevan `branch_id` (catálogo compartido, visibilidad por `tenant_product_branches`); stock, consumo y compras sí son por sucursal. Toda tabla nueva entra al baseline de tenants nuevos y a `schema_features` si lleva probe.

---

## 15. Fases, dependencias y criterios de aceptación

Método de siempre por fase: diagnóstico → autorización expresa → implementación limitada → pruebas (SQLite **y** MySQL/MariaDB estricto) → revisión de regresión → documentación → commit independiente → despliegue solo con autorización → observación. El **motor de ventas se toca al final**, cuando el usuario ya se beneficia del costeo y el precio sugerido.

| Fase | Contenido | Depende de | Criterios de aceptación |
|---|---|---|---|
| **F0a** Endurecer inventario | Lock ordenado, UNIQUE `(product, branch)`, reintento por deadlock, saldo negativo opcional (apagado), `unit_cost` (15,6) en documentos, reconciliador | — | Pruebas de concurrencia MySQL (N hilos, saldo exacto); sin duplicados que impidan el UNIQUE; reconciliador sin falsos positivos sobre datos reales; ventas sin recetas idénticas; latencia y pool medidos |
| **F0b** Exactitud de cobro y anulación | `BillTable` releer en transacción + `UPDATE` condicional; anulación condicional | **F0a cerrada y observada** (se implementa por separado, §20.5) | Misma comanda con claves distintas → 1 venta; anulación concurrente → 1 reversa; sin cambios fiscales; regresión de ventas/caja |
| **F1** Modelo y catálogo | Migración, `is_supply`, tablas de receta/ledger/auditoría de precios (vacías), flags y capabilities, filtros y rechazos de servidor, cupo, `SaleUnit` relajada, pantallas Insumos y unidades | F0a | Migración idempotente en MySQL estricto; insumo ausente de todo listado de venta y no vendible; cupo intacto; tenants sin flags sin cambios |
| **F2** Costos | `avg_unit_cost`, costo sin IGV, costo en salidas/traslados de insumos, backfill | F0a, F1 | Promedio correcto en compras/ajustes/traslados; IGV excluido; S/0,0042/g exacto; productos normales sin cambios |
| **F3** Compras, ingresos y carga inicial | Puentes de permisos, proveedores, compra de insumos (reutiliza `PurchaseService`), ingreso manual, carga inicial idempotente, historial | F1, F2 | Compra en saco/kg convierte y costea; permisos por PIN funcionan; reintento de carga no duplica; anulación coherente |
| **F4** Recetas, costeo, precio sugerido y disponibilidad | Pestaña Receta, costos y márgenes, precio sugerido con redondeo y confirmación auditada, disponibilidad (solo lectura) | F1, F2 (F3 para tener costos reales) | Fórmulas verificadas (ejemplo 4,15 → 13,83 neto; con IGV y redondeo); el precio jamás cambia sin confirmar; auditoría de cambios; disponibilidad = ejemplo de 13 hamburguesas; **sin tocar ventas** |
| **F5** Consumo y reversa | `ConsumeForSaleTx`, `ReverseForSaleTx`, políticas, combos, NV→comprobante, snapshot en `tenant_sale_items.recipe_cost` (campo independiente) | F0a, F0b, F1, F2, F4; carga inicial de F3 antes de encender | Invariantes I1–I6 con la matriz de pruebas; reconciliador vacío tras pruebas aleatorias; prueba dorada; apagar consumo conserva reversas |
| **F6** Reportes | Existencias/valorización, historial, consumo, costo y food cost, margen, mayores costos, mayor consumo, mermas, disponibilidad | F3, F4, F5 | Cifras cuadran con ledger y kardex; consultas livianas |
| **V2-a** Producción de preparaciones | Registro de producción, costo, stock de preparados, uso como ingrediente | F5 | Se define aparte (alineado con ADR de Producción) |
| **V2-b** Presentaciones, modificadores, mermas completas, teórico vs real, importación | — | F5, F6 | Se define aparte |

---

## 16. Estrategia de pruebas

- **Por el flujo real**: POS, `BillTable` total/parcial/dividido, combos, anulación, NC, NV→comprobante; nunca armando movimientos a mano.
- **Matriz** de flujos × {consumo, anulación, NC parcial, NC total, NC total tras parciales, reintento}.
- **Concurrencia en MySQL 8 estricto** (SQLite no simula `FOR UPDATE`; tenant anonimizado, decisión D10 de Audit V2): cobros simultáneos del mismo insumo (saldo exacto); misma comanda con claves distintas; anulación concurrente; efecto de NC ejecutado dos veces.
- **Prueba dorada**: venta sin recetas = mismos movimientos antes/después; también con `recipes_enabled=true` y consumo apagado.
- **Propiedad**: tras secuencias aleatorias de ventas/anulaciones/NC, `Σ consumos − Σ reversas = variación de stock` y reconciliador vacío.
- **Inyección de fallos**: error tras el movimiento → rollback total; política `block` → nada persistido.
- **Costeo y precio**: casos con merma, rendimiento, kg↔g, L↔ml, costos con y sin IGV, S/0,0042/g, redondeos (`0.10`, `0.50`, `1.00`, `x.90`), FC y margen reales tras redondear, precio por sucursal, producto exonerado; el precio publicado no cambia sin confirmación y cada cambio queda auditado.
- **Disponibilidad**: ejemplo de 150 g y 2 kg = 13; ingrediente limitante; stock cero/negativo; combos.
- **Costo histórico**: cambiar el costo de un insumo no altera ventas pasadas ni la utilidad histórica.
- **Permisos**: puente de restaurante (PIN) en compras/ingresos/insumos; cocinero sin `recipes.view_cost` no ve costos.
- **Filtros y cupo**: insumo ausente de POS, carta, e-commerce, buscadores; rechazo en venta/cotización/comanda; importación respeta cupo.
- **Migraciones**: idempotentes, en MySQL con `sql_mode` estricto, verificando `decimal`/`varchar`.
- En cada entrega se declara qué se probó (local MariaDB 10.4 vs producción MySQL 8.0.46) y qué no.

---

## 17. Riesgos

1. **Doble cobro de la misma comanda** con claves distintas `[V]`: sin F0b no se puede afirmar "una vez".
2. **F0a cambia el inventario de todos los tenants** (bloqueos): medir latencia y pool (6 conexiones por tenant); orden de bloqueo estable.
3. **Reversas genéricas existentes**: de usar `VENTA/` se revertiría dos veces → D10.
4. **NC parcial sobre comprobante convertido no repone stock hoy** `[V]`; se resuelve para insumos con el mapeo (F5).
5. **Efecto de NC parcial no idempotente para productos normales** `[V]`: fuera de alcance, ticket aparte.
6. **Costo de compra con IGV** en productos normales `[V]`: no se toca; solo insumos van sin IGV.
7. **Confusión margen vs recargo**: la pantalla muestra ambos rotulados y la entrada es FC o margen sobre ventas.
8. **Base del food cost** (neto vs con IGV): decidido neto (D13); configurable.
9. **Precio sugerido mal aplicado**: confirmación explícita y auditoría; no hay actualización masiva automática.
10. **Costos visibles al personal**: permiso `recipes.view_cost`.
11. **Ventana sin consumo** al apagar/encender: se documenta y no se recupera automáticamente.
12. **Transacción más larga** con N insumos por plato y pool pequeño: cálculo en lote y medición antes de abrir a más tenants.
13. **Plato con receta y stock directo / presentación con stock propio**: se ignora el directo (D5); aviso en el formulario.
14. **Disponibilidad independiente por plato**: platos que comparten insumo no suman; se aclara en pantalla.
15. **Validación de pedido no ve insumos**: con política `block` un plato puede pedirse y fallar al cobrar; advertencia en V1.
16. **Cantidades < 0,001 g** se pierden por precisión de stock; condimentos en unidad mayor o por lote.
17. **Compras exigen caja abierta incluso a crédito y el módulo `purchases`** `[V]`: tenants sin ese módulo solo tendrán ingreso manual.
18. **Despliegue**: hay migraciones (backend primero; `migrate-central` y `migrate-fleet` antes del reinicio; no usar `skip_migrate`); la BD local no es estricta.
19. **Frontends sin código compartido**: los componentes se copian y pueden divergir; se acuerda aparte si se extrae un paquete.

---

## 18. Decisiones de esta v3 (confirmadas por el dueño el 25-nov-2026; ver §2 y §20)

- **D13** Food cost y margen sobre precio **neto** (sin IGV); ejemplo: 4,15 con FC 30 % → 13,83 neto → 16,32 con IGV → 16,40 redondeado. ¿Confirmas, o prefieres que el 13,83 sea el precio publicado?
- **D15** Compras de insumos reutilizando `PurchaseService` (con módulo `purchases`, caja abierta y puente de restaurante) más ingreso manual para quien no tenga compras.
- ~~D16~~ **Resuelta**: campo independiente `recipe_cost`, ver §20.1.
- Redondeo comercial por defecto: `up` a 0,10 (alternativas 0,50, 1,00 o terminado en 0,90).
- Permisos: `recipes.view_cost` separado para ocultar costos al personal.
- Alcance V1 propuesto en §1.1 (costeo, precio sugerido y disponibilidad se entregan **antes** del consumo, F4 antes de F5).

---

## 19. Qué NO se ha verificado

- No se ejecutó nada ni se tocó BD, migraciones ni producción; todo es lectura de código.
- Las carreras (doble cobro, doble reversa) son deducciones por lectura (`[I]`); se confirmarán con las pruebas de concurrencia de F0.
- El puente de permisos de contactos para staff por PIN no se comprobó en el código del middleware.
- No se auditó el frontend por pantalla (estructura y consumidores de `/api/products`), ni promociones y caja de restaurante del sistema antiguo.
- No se verificó si `go.mod` ya incluye una librería de decimales; la propuesta exige aritmética decimal para recetas y promedio.
- Los redondeos del ejemplo se calcularon a mano y se verificarán con pruebas.

---

## 20. Precisiones confirmadas el 25-nov-2026 (v3.1)

### 20.1 Auditoría de `tenant_sale_items.purchase_price` (D16)

**Qué significa hoy `[V]`:** snapshot del costo del producto vendido **por unidad de la línea** (el costo del catálogo se multiplica por el factor de la unidad de venta; migración V145, `resolveSaleItemPurchasePrice` en `sale_service_calc.go`). Es `NULL` en líneas manuales y en ventas anteriores a V145. Se serializa como `purchase_price` en el JSON de la línea.

**Quién la escribe:**
- `resolveSaleItemPurchasePrice` (`sale_service_calc.go:67` y `:199`): `SaleService.Create` y la conversión NV → comprobante (el hijo **recalcula** el snapshot con el costo vigente al convertir, no copia el de la NV: peculiaridad existente).
- `restaurantSaleItemPurchasePrice` (`restaurant_service.go:1391`): cobro de mesa (`BillTable`); **no** aplica el factor de la unidad de venta (esa ruta ignora `SaleUnit`).

**Quién la lee `[V]`:**
- `ProfitDetail` (`sale_service.go:2307`, `COALESCE(tenant_sale_items.purchase_price, p.purchase_price, 0)`), que alimenta `GET /api/sales/profit-detail`, el resumen de utilidad del dashboard (`dashboard/handler/profit_api.go:75`) y la pantalla `ProfitDetailReportPage` de Tukifac.
- Respuestas JSON de ventas: el middleware `StripProductCost` (`pkg/middleware/product_cost.go`) **pone en 0 toda clave `purchase_price`** para quien no tiene `products.view_cost`; el endpoint `/api/sales/profit-detail` está exento (lo gobierna su propio acceso de ventas).
- El e-commerce público la fuerza a 0 (`ecommerce_service.go:401`).
- No hay otros lectores en backend ni en los frontends (solo el reporte de utilidades y los tipos de `sales.service.ts`).

**Conclusión.** Escribir el costo de receta en `purchase_price` mantendría el significado numérico (costo por unidad de la línea), pero:
1. mezclaría en un mismo campo dos bases de costo distintas (último costo con IGV de productos normales vs promedio sin IGV de insumos);
2. quedaría gobernado por `products.view_cost` en vez de `recipes.view_cost` (el middleware filtra por nombre de clave), contradiciendo el permiso independiente aprobado;
3. la conversión NV → comprobante ya recalcula `purchase_price` del hijo al convertir, así que el costo de receta del hijo no podría heredarse sin una regla especial;
4. cualquier lector futuro asumiría "costo del catálogo".

**Decisión:** campo independiente `tenant_sale_items.recipe_cost decimal(15,6) NULL` (migración aditiva en la fase de consumo, F5), con clave JSON propia `recipe_cost`:
- `StripProductCost` se amplía para ocultar `recipe_cost` sin `recipes.view_cost` (la sola existencia de otra clave no la protege).
- `ProfitDetail` lee `COALESCE(recipe_cost, purchase_price, p.purchase_price, 0)`; para quien no tenga `recipes.view_cost`, el costo de las líneas con receta se oculta igual que hoy se oculta el costo del producto sin su permiso (se define exactamente en F5).
- En NV → comprobante, el hijo **copia** `recipe_cost` de la línea de la NV (el consumo es de la NV), sin recalcular.
- `purchase_price` no se toca: su significado y sus lectores no cambian.

### 20.2 A. Costo promedio ponderado con saldos negativos

Definiciones (solo insumos, por `(producto, sucursal)`, en `tenant_product_stocks.avg_unit_cost`): `Q` = saldo antes del movimiento (puede ser negativo con política `allow`), `A` = costo promedio vigente (`NULL` si nunca hubo costo), `q` = cantidad del movimiento, `c` = costo unitario de la entrada.

| Movimiento | Regla |
|---|---|
| **Entrada con `c > 0`** | `base = max(Q, 0)`; `A' = (base·A + q·c) / (base + q)`. Como `q > 0`, el denominador **nunca es cero**. Si `Q ≤ 0` (o `A` es `NULL`), `A' = c`: el faltante ya se consumió y se costeó con el promedio de su momento; **no se reescribe** el histórico ni se mezcla un saldo negativo en el promedio. Si la entrada no cubre todo el faltante, `A' = c` igualmente y el saldo sigue negativo |
| **Entrada con costo 0 o desconocido** (traslado sin costo, devolución, stock inicial sin costo) | No cambia `A`; solo mueve el saldo. Si `A` es `NULL`, queda "sin costo" y las salidas posteriores se marcan con costo desconocido |
| **Salida** (consumo, merma, anulación de compra) | No cambia `A`; se registra con `unit_cost = A`. Con saldo negativo se costea igual al promedio vigente. Si `A` es `NULL`, `unit_cost = 0` **y** se marca "costo desconocido" (un `0` no se interpreta como costo real) |
| **Ajuste absoluto** | Cambia el saldo; `A` se conserva (si el saldo queda en 0, `A` **no** se reinicia a 0: es el último costo conocido) |
| **Reversa de consumo** (NC, anulación) | Entra a **costo original** del consumo; se trata como entrada con `c = costo original` con la misma fórmula |
| **Anulación de compra** | Salida al costo original; `A` **no** se recalcula hacia atrás (aproximación documentada; el reconciliador señala si el saldo queda inconsistente) |

Invariantes: `A ≥ 0` siempre; `A` solo se calcula con costos `> 0`; sin divisiones por cero; redondeo a 6 decimales solo al guardar; los `unit_cost` de movimientos pasados **nunca** se modifican; el reconciliador verifica `A ≥ 0` y que `A` no sea `NULL` si hay saldo positivo con entradas costeadas. Pruebas obligatorias: saldo negativo seguido de entradas parciales y totales, entrada con costo 0, ajuste a cero, reversa, anulación de compra.

### 20.3 B. Disponibilidad: informativa, no es una reserva
- La disponibilidad por plato (`floor(min(stock_insumo / cantidad_bruta_por_porción))`) es **solo informativa**: **no reserva inventario**, no descuenta nada y no bloquea la venta en V1.
- Muestra siempre el **ingrediente limitante**.
- Varios platos que comparten insumos **no suman disponibilidad**: cada plato se calcula de forma independiente contra el mismo stock, y la suma de lo mostrado puede superar lo realmente preparable. La pantalla lo indica con un texto fijo ("Disponibilidad estimada por plato; no es una reserva ni es acumulable entre platos").
- El valor puede cambiar entre la consulta y el cobro (otros cobros, compras, ajustes).
- Se calcula en lectura, por sucursal, sin tabla caché.

### 20.4 C. Sucursales

| Dato | Alcance |
|---|---|
| Recetas, líneas de receta, configuración de precio y objetivos | **Compartidas** (catálogo; visibilidad del plato por `tenant_product_branches`); sin `branch_id` |
| Existencias, costo promedio (`avg_unit_cost`), movimientos, compras, ingresos, mermas | **Por sucursal** (`tenant_product_stocks` y los movimientos ya llevan `branch_id`) |
| Ledger de consumos y reversas | **Por sucursal** (`branch_id` obligatorio); un consumo usa solo el stock de la sucursal de la venta |
| Disponibilidad de platos | **Por sucursal** (stock de la sucursal activa) |
| Costo de receta y food cost | Se calculan con los costos promedio **de la sucursal consultada**; la pantalla indica de cuál sucursal son; dos sucursales pueden mostrar costos distintos para la misma receta |
| Precio de venta | Precio vigente de la sucursal (`TenantProductBranchPrice` si existe) |
| Traslados de insumos | Transportan el costo promedio de origen y se ponderan en el destino (misma fórmula de entrada) |
| Reportes | Respetan el aislamiento por sucursal; una vista "todas" es solo de consulta |
| Carga inicial | Por sucursal |

Toda tabla nueva con `branch_id` (ledger, reversas, lotes de carga) se registra en `BranchScopedModels`; las de catálogo (recetas) no.

### 20.5 D. F0a y F0b: independientes y con observación propia
- **F0a** (inventario: bloqueo de fila, UNIQUE, reintentos, saldo negativo opcional apagado, `unit_cost` de documentos, reconciliador de solo lectura) y **F0b** (cobro de mesa: doble cobro de comanda; anulación condicional) **se implementan, prueban, commitean, despliegan y observan por separado**.
- Ambas afectan a **todos** los tenants existentes: cada una exige su propia batería de regresión, verificación en MySQL estricto y MariaDB, y un período de observación independiente.
- **F0b no empieza hasta cerrar la observación de F0a**; ninguna fase posterior empieza hasta cerrar ambas.
- Cada una tiene su criterio de salida, su plan de reversión y su lista de efectos secundarios auditados.

### 20.6 E. Producción futura (V2)
Las preparaciones intermedias, subrecetas y la producción de elaborados (V2) **reutilizan la arquitectura de Producción ya documentada** (`docs/produccion`: adaptador `StockGateway`, un solo kardex, tablas propias de producción, snapshot de receta, costo de gestión) y el módulo `manufacturing` ya catalogado. **No se construye un sistema paralelo.** V1 deja preparados `supply_kind`, `component_product_id`, `yield_qty`, `source_kind` en el ledger y los tipos de operación `PRODUCTION_IN/OUT`.

### 20.7 Procedimiento por fase (obligatorio)
1. Diagnóstico del alcance exacto.
2. Archivos y componentes que se modificarán.
3. Riesgos y dependencias.
4. Propuesta concreta de implementación.
5. **Autorización expresa del dueño.**
6. Implementación limitada exclusivamente a esa fase.
7. Pruebas unitarias, de integración y de regresión.
8. Verificación en MySQL estricto y MariaDB cuando corresponda.
9. Auditoría de cambios y revisión de efectos secundarios.
10. Documentación y commit independiente.

Sin avance automático a la siguiente fase; sin despliegues ni cambios en producción sin autorización expresa.

### 20.8 Ajustes a la v3.1 derivados de la revisión técnica previa a F0a (25-nov-2026)

Detalle y pruebas en `F0a_DIAGNOSTICO_Y_PROPUESTA.md` §3. Lo que **cambia la especificación**:

**Costo promedio (F2, no F0a)**
- `RecordMovementTx` necesitará `CostMode` (`auto` | `fixed` | `none`) y devolver un `MovementResult {UnitCost, AvgAfter, Balance}`; `tenant_stock_movements` necesitará `cost_unknown` y `avg_before`; `avg_unit_cost` debe ser nullable (NULL = nunca hubo costo).
- Compras de insumos: costo **neto** calculado con `tax.CalcItem` (hoy entra el costo tal como se digitó, con o sin IGV).
- **Anulación de compra:** debe **seguir fallando** aunque exista la política `allow`; y restaura `A` desde `avg_before` si no hubo otra entrada costeada posterior (la regla "A no se recalcula hacia atrás" de §20.2 deja A mal para siempre en el caso común).
- Egreso/merma de insumos: se valoriza a `A` e **ignora** el costo digitado; anular un documento usa el `unit_cost` del movimiento original, no el del detalle.
- Ajuste masivo de insumos: `A` se lee dentro del lock; entradas con costo 0 y salidas a `A`.
- Devoluciones por anulación de venta y NC parcial: pasan `mv.UnitCost` del movimiento de salida original.
- Tipo de cambio redondeado a 4 decimales antes de usarlo; evaluar `decimal(18,8)` para `unit_cost`, `avg_unit_cost` y el log de traslados **a la vez** (la conversión por unidad de venta pierde cifras con costos por gramo).
- Traslados: `tenant_transfer_logs.unit_cost` (aditivo); costo **congelado al crear**, usado al confirmar y al cancelar; confirmar/cancelar con estado condicional dentro de la transacción.
- Validaciones de `is_supply` (F1): no admite `manage_series` ni `has_variants`; activar `is_supply` en un producto con historial exige backfill de `A`.
- Stock inicial con costo (F3): parámetro de costo en `RecordInitialStock`.

**Permisos de costo**
- La protección es un **middleware hermano `StripRecipeCost`** con su propio mapa de claves y su propio predicado (no ampliar `StripProductCost`, que quedaría gobernado por `products.view_cost`); defensa primaria en handler/servicio y strip como red de seguridad; claves únicas y no genéricas (`recipe_cost`, `recipe_cost_total`, `avg_unit_cost`, `food_cost_pct`, `margin_pct`).
- `unit_cost` de movimientos, documentos, compras, órdenes de compra y preview del ajuste masivo **hoy viaja sin filtro**; para insumos se filtra por endpoint cruzando con `is_supply`. El `purchase_price` de un insumo exige además `recipes.view_cost`.
- `/api/sales/profit-detail` no tiene permiso propio en backend (`reports.profit` solo existe en el router del frontend) y está exento del filtro: hueco **previo** a decidir en F5.
- `g.p` y `products.manage` **no** implican `recipes.view_cost`. Clave corta `r.vc` en `restaurantperm`, `PermissionSchemaVersion` incrementada, migración de permisos estilo V165 (solo Administrador al inicio); los permisos de tenant viajan en el JWT, por lo que se necesita un nuevo inicio de sesión. Sin sockets; los eventos futuros no llevarán costos.

**`recipe_cost` (F5)**
- Se agrega `tenant_sale_items.recipe_cost_total decimal(15,6) NULL` como fuente exacta; `recipe_cost` por unidad de línea se deriva (cantidad comercial).
- Se calcula en la misma transacción y bajo el bloqueo de los insumos.
- `NULL` = sin receta, consumo apagado o costo desconocido; `0` solo si el costo conocido es cero y nunca se genera automáticamente; `ProfitDetail` cuenta como "sin costo" las líneas con receta y costo `NULL`.
- NV → comprobante: el hijo copia por posición (campos `json:"-"` en `SaleItemInput`) y `GetItems` recibe `ORDER BY id`.
- Combos: hace falta vincular componente ↔ línea; **regla del combo mixto = decisión abierta** (todo-o-nada con `purchase_price` de los componentes sin receta, o `NULL`).
- `ProfitDetail` ignora hoy las NC (no resta devoluciones): hueco previo que sobrestima la utilidad, a documentar en F5. `comandaSaleLineKey` no incluye `PresentationID`: corregir antes de recetas por presentación (V2).

**F0a**
- F0a no toca costos. Su alcance exacto, riesgos, estrategia en tres etapas, pruebas MySQL/MariaDB y criterios están en `F0a_DIAGNOSTICO_Y_PROPUESTA.md`.
