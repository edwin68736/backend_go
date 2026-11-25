# F3a — Compras, ingresos y carga inicial (backend): informe

Fecha: 25-nov-2026 · Estado: **implementado y probado en local; sin push, sin deploy, sin tocar producción.** F3b no se empezó.
Alcance: `F3_PROPUESTA.md`, parte F3a, con las recomendaciones 1 a 5.

## 1. Qué quedó
- Permiso `supplies.purchase` (`sp.p`), administrador y supervisor. `PermissionSchemaVersion` 4.
- **V197:** `tenant_purchases.idempotency_key`, única por usuario (aditiva).
- `PurchaseService.Create`: clave de reintento y rechazo de comprobante repetido, **solo cuando el llamador los pide**. Tukifac no los envía.
- Rutas `/api/restaurant` (solo insumos):
  - `GET/POST /suppliers`
  - `GET/POST /supply-purchases`, `GET /:id`, `POST /:id/void` (módulos `purchases` y `cashbank`)
  - `POST /supplies/:id/initial-stock` — solo sin movimientos previos en la sucursal
  - `POST /supply-entries` — `ingreso`, `merma`, `desperdicio`, con clave obligatoria
  - `GET /supplies/:id/purchases` — importes solo con permiso de costos

### Cuerpo de la compra
`supplier_id, doc_type, series, number, issue_date, due_date, payment_method ("" o "credit" = por pagar), price_includes_igv,
idempotency_key, lines[{supply_id, sale_unit_id, quantity, line_total}]`. Respuestas: 201 nueva, 200 reintento, 409 comprobante repetido.

## 2. Pruebas
`internal/restaurant/supply_purchases_test.go`, por las rutas reales: compra en saco con IGV (stock, promedio y egreso de caja),
reintento, comprobante repetido, línea que no es insumo, sin permiso, sin módulo, sin caja, sin clave, compra ajena invisible, historial,
anulación, carga inicial, ingreso, merma, y compras sin clave sin cambios (también sin V197).
En MariaDB estricto, 8 peticiones a la vez: misma clave, mismo comprobante, anulación, ingreso y carga inicial → un solo efecto. Cuatro
corridas seguidas en verde. SQLite: suite completa sin `pkg/saas`, 101 paquetes en verde.

**Defecto de mi implementación hallado por las pruebas y corregido:** si la sucursal no tenía serie de inventario, varios ingresos a la
vez creaban series repetidas y la sucursal quedaba sin poder registrar ingresos. Ahora se crea con la sucursal bloqueada.

**No probado:** MySQL 8, producción, pantallas (no existen aún), crédito con cuenta por pagar y pagos mixtos por la ruta nueva (los
resuelve el servicio de compras, que tiene sus propias pruebas).

## 3. Defecto previo encontrado (sin corregir)
**Anular una compra en Tukifac no está protegido contra dos pulsaciones a la vez.** Reproducido en MariaDB, 3 de 3: de 8 anulaciones
simultáneas, 2 tuvieron éxito; el stock se revirtió dos veces (quedó en 0 en vez de 50) y la caja recibió dos devoluciones.
Está en `PurchaseService.Void`: lee el estado fuera de la transacción. La ruta nueva de Tukichef sí está protegida. Se corrige igual que
los traslados (F2a); afecta a todos los tenants y no depende de insumos. Falta además revisar si ya ocurrió en producción.

## 4. Límites
- Solo soles. La carga inicial es insumo por insumo.
- El costo de un ingreso se toma como neto (sin indicador de IGV).
- La anulación de una compra ya consumida sigue fallando por falta de stock.

## 5. Despliegue
Con el resto de insumos y recetas. Backend con `migrate-fleet` (V193–V197).
