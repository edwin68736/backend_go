# F3 — Compras, ingresos y carga inicial desde Tukichef: diagnóstico y propuesta

Fecha: 25-nov-2026 · Estado: **PROPUESTA; no se modificó código.** Espera autorización expresa.
Marco: `PROPUESTA_INSUMOS_RECETAS_TUKICHEF.md` §4 (D15: reutilizar compras e inventario, sin un segundo sistema).

## 1. Qué verá el usuario en Tukichef
- **Comprar insumos:** proveedor (buscar o crear), comprobante, forma de pago y líneas (insumo, unidad como «Saco 50 kg», cantidad e
  importe). Lista de compras con anulación.
- **Ingreso y merma:** meter stock sin comprobante (compra al paso en el mercado) o descontar por merma o desperdicio.
- **Carga inicial:** al empezar, decir cuánto hay de cada insumo y a qué costo.
- **Historial de compras** por insumo: cuándo, a quién y a cuánto.

No incluye: recetas (F4), descuento al cobrar (F5), órdenes de compra, compras en dólares ni carga por Excel.

## 2. Estado actual
1. `PurchaseService` ya resuelve proveedor, IGV, crédito y cuentas por pagar, pagos mixtos, unidades de compra y (desde F2b) el costo
   promedio.
2. **Compras no tiene acceso para personal con PIN:** exige `purchases.create`. Documentos de inventario exigen el módulo `inventory` y
   permisos de cuenta.
3. **Proveedores:** con PIN solo se accede a clientes; proveedores, únicamente el rol Administrador.
4. Toda compra, incluso a crédito, **exige caja abierta** del usuario.
5. **No hay protección contra compras repetidas:** ni clave de reintento ni control de proveedor + serie + número. Un doble toque en el
   celular, o un reintento por mala señal, crea dos compras y duplica el stock. Pasa hoy también en Tukifac. Deducido del código; no
   reproducido.
6. El ingreso manual y la merma ya existen como documentos de inventario, con costo por unidad base y serie propia por sucursal.

## 3. Cambios

### 3.1 Backend
- Permiso nuevo `supplies.purchase` (`sp.p`), por defecto administrador y supervisor.
- **Rutas propias bajo `/api/restaurant`, que solo aceptan insumos** y llaman a los servicios existentes:

| Ruta | Qué hace | Exige |
|---|---|---|
| `GET/POST /suppliers` | buscar y crear proveedores | `sp.p` |
| `GET/POST /supply-purchases`, `GET /:id`, `POST /:id/void` | compras de insumos | `sp.p`, módulos `purchases` y `cashbank` |
| `POST /supplies/:id/initial-stock` | carga inicial | `sp.m` |
| `POST /supply-entries` | ingreso o merma (documento confirmado) | `sp.p` |
| `GET /supplies/:id/purchases` | historial | `sp.v`; costos solo con `rc.c` |

  La v3.1 planteaba abrir las rutas actuales de compras al personal con PIN. Con rutas propias, ese personal no puede comprar ni ver
  otra cosa que insumos.
- **V197:** `tenant_purchases.idempotency_key` (anulable, única). Repetir la misma petición devuelve la compra ya creada.
- **Carga inicial:** solo se permite si el insumo no tiene ningún movimiento en esa sucursal. Así repetirla no duplica.

### 3.2 Tukichef
Menú «Compras de insumos» (con `sp.p`), pantallas de compra, lista y proveedor; y en cada insumo: «Carga inicial» (si no tiene
movimientos), «Ingreso / merma» e historial.

En la línea de compra se escribe el **importe total de la línea**, no el costo unitario: así no hay que calcular el precio del saco, que
hoy Tukifac sugiere mal (propone el costo del kilo).

## 4. Entrega
| Parte | Contenido |
|---|---|
| **F3a** | Backend: permiso, V197, rutas, protección contra repetidos y pruebas |
| **F3b** | Pantallas de Tukichef y recorrido en navegador |

## 5. Pruebas
- Compra en saco con y sin IGV: stock, promedio y último costo; anulación; contado desde caja y crédito en cuentas por pagar.
- La misma petición dos veces y a la vez (MariaDB): una sola compra.
- Rechazo de líneas que no son insumos; PIN con y sin permiso; sin caja abierta; sin módulo de compras; otra sucursal.
- Carga inicial repetida y simultánea: un solo movimiento.
- Compras de Tukifac sin cambios (prueba dorada). SQLite y MariaDB estricto.
- F3b: recorrido en el navegador, incluido ancho de celular.

## 6. Riesgos
- La compra mueve dinero (caja y cuentas por pagar): se reutiliza el servicio actual sin tocar su lógica de pagos.
- V197 agrega un índice único en `tenant_purchases` en todas las empresas; la columna nace vacía, así que no puede fallar por duplicados.
- Usuarios sin caja abierta no podrán registrar compras, aunque sean a crédito (regla actual, se mantiene).
- Un negocio sin el módulo `purchases` solo tendrá ingreso manual y carga inicial.

## 7. Decisiones que necesito
1. **Rutas propias limitadas a insumos**, en vez de abrir las de compras. Recomendado: sí.
2. **Evitar repetidos:** clave de reintento y, en Tukichef, rechazar el mismo proveedor + serie + número. Tukifac queda como está.
   Recomendado: sí. (Proteger también Tukifac sería un arreglo aparte.)
3. **Mantener la exigencia de caja abierta.** Recomendado: sí.
4. **Solo soles** en las compras desde Tukichef. Recomendado: sí.
5. **Carga inicial insumo por insumo**, sin Excel por ahora. Recomendado: sí.
6. **F3a y F3b juntas o por separado.** Recomendado: por separado.

## 8. No verificado
Solo se leyó código. La compra repetida no se reprodujo. No se revisó si todas las sucursales antiguas tienen creada la serie de
inventario; si falta, el ingreso manual debe crearla o avisar.
