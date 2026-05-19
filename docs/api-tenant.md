# Tukifac ERP — API Panel Tenant

> **Base URL:** `https://{slug}.tu-dominio.com` (o `http://localhost:3000` en desarrollo con header `X-Tenant-Slug: {slug}`)
> **Autenticación:** `Authorization: Bearer <token>` (excepto login)
> **Content-Type:** `application/json`

---

## Identificación del Tenant

El sistema identifica al tenant mediante el subdominio (`slug.dominio.com`).

**En desarrollo local**, como no hay subdominios, envía el header:
```
X-Tenant-Slug: mi-empresa
```

---

## 1. Autenticación

### POST `/api/login`

Autentica un usuario del tenant y devuelve un Bearer Token JWT.

**Request body:**
```json
{
  "email": "admin@empresa.com",
  "password": "clave123"
}
```

**Response 200:**
```json
{
  "token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
  "expires_in": 28800,
  "user": {
    "id": 1,
    "name": "Administrador",
    "email": "admin@empresa.com",
    "role": "Administrador"
  }
}
```

**Response 401:**
```json
{ "error": "Credenciales inválidas" }
```

> Token válido por **8 horas**. Incluye en todas las peticiones: `Authorization: Bearer <token>`

---

## 2. Dashboard

### GET `/api/dashboard/stats`

Retorna estadísticas generales del negocio para el dashboard.

**Response 200:**
```json
{
  "totals": {
    "contacts": 142,
    "products": 87,
    "sales": 530,
    "purchases": 45
  },
  "current_month": {
    "sales_count": 38,
    "sales_total": 15420.50,
    "purchases_total": 4200.00,
    "month": 3,
    "year": 2026
  },
  "monthly_sales": [
    { "month": 1, "year": 2026, "amount": 12000.00 },
    { "month": 2, "year": 2026, "amount": 14500.00 },
    { "month": 3, "year": 2026, "amount": 15420.50 }
  ],
  "low_stock_products": [
    { "product_id": 12, "product_name": "Arroz 1kg", "quantity": 2, "min_stock": 10 }
  ],
  "open_cash_sessions": 1,
  "pending_billing": 5
}
```

---

## 3. Empresa y Configuración

### GET `/api/company/config`

Retorna la configuración general de la empresa.

**Response 200:**
```json
{
  "id": 1,
  "business_name": "Mi Empresa SAC",
  "trade_name": "Mi Empresa",
  "ruc": "20123456789",
  "address": "Av. Principal 123, Lima",
  "phone": "01-2345678",
  "email": "contacto@empresa.com",
  "currency": "PEN",
  "tax_rate": 18,
  "color_theme": "blue",
  "logo_url": "https://..."
}
```

### PUT `/api/company/config`

Actualiza la configuración general.

**Request body:**
```json
{
  "business_name": "Mi Empresa SAC",
  "trade_name": "Mi Empresa",
  "ruc": "20123456789",
  "address": "Av. Principal 123, Lima",
  "phone": "01-2345678",
  "email": "contacto@empresa.com",
  "currency": "PEN",
  "color_theme": "blue"
}
```

### GET `/api/company/sunat`

Retorna la configuración SUNAT (sin mostrar tokens/passwords completos).

**Response 200:**
```json
{
  "sunat_enabled": true,
  "sunat_env_mode": "demo",
  "sunat_sol_user": "20123456789MODDATOS",
  "tax_rate": 18.0,
  "igv_regime": "standard",
  "tax_benefit_zone": false,
  "tukifac_token_set": true
}
```

**`igv_regime`:** `standard` | `reduced` | `exempt`

### PUT `/api/company/sunat`

Actualiza la configuración SUNAT.

**Request body:**
```json
{
  "sunat_enabled": true,
  "sunat_sol_user": "20123456789MODDATOS",
  "sunat_sol_pass": "contraseña",
  "sunat_env_mode": "production",
  "tukifac_token": "tk_live_...",
  "tax_rate": 18.0,
  "igv_regime": "standard",
  "tax_benefit_zone": false
}
```

### GET `/api/company/branches`

Lista las sucursales de la empresa.

**Response 200:**
```json
{
  "data": [
    { "id": 1, "name": "Sede Principal", "address": "Av. Lima 100", "phone": "", "is_main": true, "active": true }
  ]
}
```

### POST `/api/company/branches`

Crea una sucursal.

```json
{ "name": "Sede Norte", "address": "Av. Norte 45", "phone": "999111222", "is_main": false }
```

### PUT `/api/company/branches/:id`

Actualiza una sucursal. Mismo body que POST.

### DELETE `/api/company/branches/:id`

Elimina una sucursal (no eliminar si tiene datos asociados).

### GET `/api/company/series?branch_id=1&category=venta`

Lista las series de documentos. Filtro opcional por `branch_id` y `category`.

**Categorías:** `venta` | `nota_credito` | `nota_debito` | `retencion` | `percepcion` | `guia_remision` | `almacen`

**Response 200:**
```json
{
  "data": [
    {
      "id": 1,
      "branch_id": 1,
      "doc_type": "Factura Electrónica",
      "sunat_code": "01",
      "category": "venta",
      "series": "F001",
      "correlative": 125,
      "active": true
    }
  ]
}
```

### POST `/api/company/series`

Crea una serie.

```json
{
  "branch_id": 1,
  "doc_type": "Factura Electrónica",
  "sunat_code": "01",
  "category": "venta",
  "series": "F002"
}
```

### PUT `/api/company/series/:id`

Actualiza una serie (solo serie y estado activo).

```json
{ "series": "F002", "active": true }
```

---

## 4. Usuarios y Roles

### GET `/api/users?q=&role_id=`

Lista usuarios del tenant.

**Response 200:**
```json
{
  "data": [
    { "id": 1, "name": "Juan Pérez", "email": "juan@empresa.com", "role_id": 2, "branch_id": 1, "active": true }
  ]
}
```

### GET `/api/users/:id`

Detalle de un usuario.

### POST `/api/users`

Crea un usuario.

```json
{
  "name": "María López",
  "email": "maria@empresa.com",
  "password": "Pass1234!",
  "role_id": 2,
  "branch_id": 1
}
```

### PUT `/api/users/:id`

Actualiza un usuario.

```json
{ "name": "María López", "role_id": 3, "branch_id": 1 }
```

### DELETE `/api/users/:id`

Elimina un usuario.

### PATCH `/api/users/:id/toggle`

Activa o desactiva un usuario.

### GET `/api/roles`

Lista todos los roles.

**Response 200:**
```json
{
  "data": [
    { "id": 1, "name": "Administrador", "description": "Acceso total" },
    { "id": 2, "name": "Vendedor", "description": "Solo ventas y caja" }
  ]
}
```

### GET `/api/roles/:id`

Detalle del rol con sus permisos asignados.

**Response 200:**
```json
{
  "data": { "id": 2, "name": "Vendedor", "description": "Solo ventas y caja" },
  "permission_ids": [1, 5, 8, 12]
}
```

### POST `/api/roles`

Crea un rol con permisos.

```json
{
  "name": "Cajero",
  "description": "Acceso a caja y ventas",
  "permission_ids": [1, 5, 8]
}
```

### PUT `/api/roles/:id`

Actualiza rol y permisos. Mismo body que POST.

### DELETE `/api/roles/:id`

Elimina un rol (no si tiene usuarios asignados).

### GET `/api/permissions`

Lista todos los permisos disponibles.

**Response 200:**
```json
{
  "data": [
    { "id": 1, "key": "sales.create", "name": "Crear ventas", "module": "sales" },
    { "id": 2, "key": "sales.cancel", "name": "Anular ventas", "module": "sales" }
  ]
}
```

---

## 5. Contactos (Clientes y Proveedores)

### GET `/api/contacts?q=&type=customer`

Busca contactos. `type`: `customer` | `supplier` | `both`

**Response 200:**
```json
{
  "data": [
    {
      "id": 1,
      "type": "customer",
      "doc_type": "RUC",
      "doc_number": "20123456789",
      "business_name": "Cliente ABC SAC",
      "trade_name": "Cliente ABC",
      "address": "Av. Lima 100",
      "phone": "999000111",
      "email": "abc@email.com",
      "active": true
    }
  ]
}
```

### GET `/api/contacts/:id`

Detalle de un contacto.

---

## 6. Productos

### GET `/api/products?q=&category_id=&restaurant_only=false`

Busca productos.

**Response 200:**
```json
{
  "data": [
    {
      "id": 1,
      "code": "PROD001",
      "name": "Arroz Extra 1kg",
      "unit": "KG",
      "sale_price": 4.50,
      "purchase_price": 3.20,
      "tax_rate": 18,
      "igv_affectation_type": "10",
      "price_includes_igv": true,
      "manage_stock": true,
      "is_restaurant": false,
      "active": true,
      "category_id": 3
    }
  ]
}
```

### POST `/api/categories`

Crea una categoría de producto.

```json
{ "name": "Bebidas", "description": "" }
```

**Response 201:**
```json
{ "success": true, "data": { "id": 5, "name": "Bebidas" } }
```

### GET `/api/modifier-groups`

Lista grupos de modificadores (variantes de producto).

### POST `/api/modifier-groups`

Crea un grupo de modificadores.

```json
{
  "name": "Tamaño",
  "options": ["Pequeño", "Mediano", "Grande"]
}
```

---

## 7. Inventario

### GET `/api/inventory/stock/:productId?branch_id=1`

Retorna el stock de un producto por sucursal.

**Response 200:**
```json
{
  "product_id": 5,
  "stocks": [
    { "branch_id": 1, "branch_name": "Sede Principal", "quantity": 45.0, "reserved": 0.0 }
  ]
}
```

### GET `/api/inventory/movements?product_id=5&branch_id=1&from=2026-01-01&to=2026-03-31`

Lista movimientos de inventario (Kardex).

**Response 200:**
```json
{
  "data": [
    {
      "id": 10,
      "product_id": 5,
      "branch_id": 1,
      "type": "in",
      "quantity": 50,
      "unit_cost": 3.20,
      "reference": "COMPRA-001",
      "created_at": "2026-02-01T10:00:00Z"
    }
  ]
}
```

---

## 8. Ventas

### POST `/api/sales`

Crea una venta nueva.

**Request body:**
```json
{
  "branch_id": 1,
  "contact_id": 5,
  "doc_type": "FACTURA",
  "series": "F001",
  "currency": "PEN",
  "payment_method": "cash",
  "cash_session_id": 3,
  "notes": "",
  "items": [
    {
      "product_id": 1,
      "code": "PROD001",
      "description": "Arroz Extra 1kg",
      "unit": "KG",
      "quantity": 5,
      "unit_price": 4.50,
      "igv_affectation_type": "10",
      "price_includes_igv": true
    }
  ]
}
```

**Response 201:**
```json
{
  "success": true,
  "data": {
    "id": 88,
    "doc_type": "FACTURA",
    "series": "F001",
    "number": "00000088",
    "issue_date": "2026-03-01",
    "subtotal": 19.07,
    "tax_amount": 3.43,
    "total": 22.50,
    "status": "active",
    "billing_status": "pending"
  }
}
```

### GET `/api/sales/:id`

Detalle completo de una venta, incluyendo ítems e información de facturación electrónica.

**Response 200:**
```json
{
  "sale": {
    "id": 88,
    "doc_type": "FACTURA",
    "series": "F001",
    "number": "00000088",
    "contact_id": 5,
    "subtotal": 19.07,
    "tax_amount": 3.43,
    "total": 22.50,
    "status": "active",
    "billing_status": "sent"
  },
  "items": [ ... ],
  "invoice": {
    "xml_url": "https://...",
    "pdf_url": "https://...",
    "cdr_url": "https://...",
    "sunat_response": "La empresa... ha aceptado...",
    "sunat_status": "accepted"
  }
}
```

### GET `/api/sales/:id/payments`

Lista los pagos registrados para una venta.

**Response 200:**
```json
{
  "data": [
    { "id": 1, "method": "cash", "amount": 20.00, "reference": "" },
    { "id": 2, "method": "card", "amount": 2.50, "reference": "TXN123" }
  ]
}
```

### POST `/api/sales/:id/payments`

Registra pagos múltiples (pago mixto) para una venta.

```json
{
  "payments": [
    { "method": "cash", "amount": 20.00 },
    { "method": "card", "amount": 2.50, "reference": "TXN123" }
  ]
}
```

**Métodos válidos:** `cash` | `card` | `transfer` | `yape` | `plin` | `check` | `credit`

**Regla:** La suma de `amount` debe ser igual al `total` de la venta.

---

## 9. Facturación Electrónica (SUNAT)

### POST `/api/billing/send/:saleId`

Envía una venta a SUNAT mediante la API de Tukifac.

**Response 200:**
```json
{
  "success": true,
  "status": "accepted",
  "message": "La empresa 20123456789 ha aceptado el comprobante F001-00000088",
  "xml_url": "https://...",
  "pdf_url": "https://...",
  "cdr_url": "https://..."
}
```

**Response 400:**
```json
{ "error": "La venta ya fue enviada a SUNAT" }
```

### GET `/api/billing/invoice/:saleId`

Obtiene el estado de facturación y los links de una venta.

**Response 200:**
```json
{
  "sale_id": 88,
  "billing_status": "accepted",
  "xml_url": "https://...",
  "pdf_url": "https://...",
  "cdr_url": "https://...",
  "sunat_response": "Aceptado",
  "sent_at": "2026-03-01T15:30:00Z"
}
```

---

## 10. Compras

### GET `/api/purchases?q=&contact_id=&from=2026-01-01&to=2026-03-31`

Lista compras con filtros opcionales.

**Response 200:**
```json
{
  "data": [
    {
      "id": 12,
      "doc_type": "FACTURA",
      "series": "F001",
      "number": "00000245",
      "issue_date": "2026-02-15",
      "supplier_name": "Proveedor ABC SAC",
      "currency": "PEN",
      "subtotal": 847.46,
      "tax_amount": 152.54,
      "total": 1000.00,
      "status": "active"
    }
  ],
  "total": 12
}
```

### GET `/api/purchases/:id`

Detalle de una compra con sus ítems.

### POST `/api/purchases`

Crea una compra nueva.

```json
{
  "branch_id": 1,
  "contact_id": 8,
  "doc_type": "FACTURA",
  "series": "F001",
  "number": "00000245",
  "issue_date": "2026-02-15",
  "currency": "PEN",
  "notes": "Compra de insumos",
  "items": [
    {
      "product_id": 3,
      "code": "INS001",
      "description": "Harina de trigo 25kg",
      "unit": "SAC",
      "quantity": 10,
      "unit_cost": 85.00,
      "igv_affectation_type": "10",
      "price_includes_igv": false
    }
  ]
}
```

**Response 201:**
```json
{
  "success": true,
  "data": {
    "id": 13,
    "subtotal": 847.46,
    "tax_amount": 152.54,
    "total": 1000.00,
    "status": "active"
  }
}
```

---

## 11. Caja

### GET `/api/cashbank/sessions?branch_id=1`

Lista sesiones de caja (máx. 50 últimas).

**Response 200:**
```json
{
  "data": [
    {
      "id": 5,
      "branch_id": 1,
      "opened_by": 1,
      "opening_balance": 500.00,
      "closing_balance": null,
      "expected_balance": null,
      "difference": null,
      "status": "open",
      "opened_at": "2026-03-01T08:00:00Z",
      "closed_at": null
    }
  ]
}
```

### GET `/api/cashbank/sessions/open?branch_id=1`

Retorna la sesión abierta de la sucursal, o `null` si no hay.

**Response 200 (abierta):**
```json
{ "data": { "id": 5, "status": "open", ... }, "open": true }
```

**Response 200 (cerrada):**
```json
{ "data": null, "open": false }
```

### POST `/api/cashbank/sessions`

Abre una nueva sesión de caja.

```json
{
  "branch_id": 1,
  "opening_balance": 500.00,
  "notes": "Turno mañana"
}
```

**Regla:** Solo puede haber una sesión abierta por sucursal.

### POST `/api/cashbank/sessions/:id/close`

Cierra la sesión de caja con arqueo.

```json
{
  "closing_balance": 1250.00,
  "notes": "Cierre turno tarde"
}
```

**Response 200:**
```json
{ "success": true }
```

### GET `/api/cashbank/sessions/:id/movements`

Lista los movimientos de una sesión de caja.

**Response 200:**
```json
{
  "data": [
    {
      "id": 3,
      "cash_session_id": 5,
      "type": "income",
      "amount": 200.00,
      "category": "Venta efectivo",
      "reference": "VENTA-088",
      "notes": "",
      "created_at": "2026-03-01T10:15:00Z"
    }
  ]
}
```

### POST `/api/cashbank/sessions/:id/movements`

Registra un movimiento manual en caja.

```json
{
  "type": "expense",
  "category": "Gastos operativos",
  "reference": "GASTO-001",
  "amount": 45.00,
  "notes": "Compra de útiles"
}
```

**`type`:** `income` | `expense`

---

## 12. Bancos

### GET `/api/cashbank/bank-accounts`

Lista las cuentas bancarias activas.

**Response 200:**
```json
{
  "data": [
    {
      "id": 1,
      "name": "Cuenta Corriente BCP",
      "bank_name": "BCP",
      "account_number": "194-123456-0-70",
      "currency": "PEN",
      "balance": 15420.50,
      "active": true
    }
  ]
}
```

### POST `/api/cashbank/bank-accounts`

Crea una cuenta bancaria.

```json
{
  "name": "Cuenta Corriente BCP",
  "bank_name": "BCP",
  "account_number": "194-123456-0-70",
  "currency": "PEN",
  "initial_balance": 5000.00
}
```

### GET `/api/cashbank/bank-accounts/:id/movements`

Lista movimientos de una cuenta bancaria.

### POST `/api/cashbank/bank-accounts/:id/movements`

Registra un movimiento bancario.

```json
{
  "type": "credit",
  "description": "Depósito cliente",
  "reference": "OPER-2026030001",
  "amount": 1500.00,
  "date": "2026-03-01"
}
```

**`type`:** `credit` (abono/ingreso) | `debit` (cargo/egreso)

---

## 13. Módulo Restaurante

> Ver documentación completa en [`api-restaurant.md`](./api-restaurant.md)

### Resumen de endpoints:

| Método | Endpoint | Descripción |
|--------|----------|-------------|
| GET | `/api/restaurant/floors` | Listar pisos/salas |
| POST | `/api/restaurant/floors` | Crear piso |
| PUT | `/api/restaurant/floors/:id` | Actualizar piso |
| DELETE | `/api/restaurant/floors/:id` | Eliminar piso |
| GET | `/api/restaurant/tables` | Listar mesas con estado |
| POST | `/api/restaurant/tables` | Crear mesa |
| PUT | `/api/restaurant/tables/:id` | Actualizar mesa |
| DELETE | `/api/restaurant/tables/:id` | Eliminar mesa |
| GET | `/api/restaurant/waiters` | Listar mozos |
| POST | `/api/restaurant/waiters` | Crear mozo |
| PUT | `/api/restaurant/waiters/:id` | Actualizar mozo |
| DELETE | `/api/restaurant/waiters/:id` | Eliminar mozo |
| POST | `/api/restaurant/sessions` | Abrir mesa / pedido rápido |
| GET | `/api/restaurant/sessions/:id` | Detalle de sesión |
| POST | `/api/restaurant/sessions/:id/orders` | Agregar orden |
| POST | `/api/restaurant/sessions/:id/bill` | Cobrar y cerrar mesa |
| POST | `/api/restaurant/sessions/:id/cancel` | Cancelar sesión |
| GET | `/api/restaurant/tables/:id/session` | Sesión activa de una mesa |
| PUT | `/api/restaurant/comandas/:id/status` | Cambiar estado de comanda |
| POST | `/api/restaurant/comandas/:id/print` | Marcar comanda como impresa |
| DELETE | `/api/restaurant/comandas/:id` | Cancelar comanda (admin) |
| GET | `/api/restaurant/kitchen` | Vista cocina |

---

## Tipos de Afectación IGV (SUNAT Catálogo N°07)

| Código | Descripción |
|--------|-------------|
| `10` | Gravado — Operación Onerosa |
| `20` | Exonerado — Operación Onerosa |
| `30` | Inafecto — Operación Onerosa |
| `40` | Exportación |
| `11` | Gravado — Retiro por premio |
| `12` | Gravado — Retiro por donación |
| `21` | Exonerado — Transferencia gratuita |
| `31` | Inafecto — Retiro |

---

## Reglas de Negocio Globales

1. **IGV dinámico:** La tasa IGV nunca es fija. Se obtiene de `GET /api/company/sunat` (`tax_rate`). Úsala en tus cálculos frontend.
2. **Series por tipo:** Al mostrar el selector de serie en ventas, filtra con `GET /api/company/series?category=venta`.
3. **Multi-sucursal:** Todas las entidades que manejan stock, caja o ventas requieren `branch_id`.
4. **Pago mixto:** Una venta puede tener múltiples pagos con distintos métodos. La suma debe igualar el total.
5. **Stock bloqueado en transferencias:** Al realizar una transferencia de productos con número de serie, el stock queda bloqueado hasta que se confirme la recepción.
6. **Facturación electrónica:** Solo se envían a SUNAT documentos tipo `FACTURA` o `BOLETA`. La propiedad `billing_status` puede ser: `pending` | `sent` | `accepted` | `rejected`.

---

## Códigos de Error

| HTTP | Descripción |
|------|-------------|
| 400 | Datos inválidos / regla de negocio no cumplida |
| 401 | Sin autenticación o token expirado |
| 403 | Rol sin permiso para esta operación |
| 404 | Recurso no encontrado |
| 409 | Conflicto (ej: sesión de caja ya abierta) |
| 500 | Error interno del servidor |

```json
{ "error": "descripción del error" }
```

---

## Flujo Completo de Venta (POS)

```
1. GET  /api/company/sunat                  → obtener tax_rate
2. GET  /api/company/series?category=venta  → listar series disponibles
3. GET  /api/cashbank/sessions/open?branch_id=1  → verificar caja abierta
4. GET  /api/products?q=                    → buscar productos
5. GET  /api/contacts?q=&type=customer      → buscar cliente (opcional)
6. POST /api/sales                          → crear venta
7. POST /api/billing/send/:saleId           → enviar a SUNAT (si aplica)
```

## Flujo Completo de Restaurante

```
1. GET  /api/restaurant/floors              → listar salas
2. GET  /api/restaurant/tables              → ver mesas con estado
3. POST /api/restaurant/sessions            → abrir mesa { table_id, guests }
4. POST /api/restaurant/sessions/:id/orders → agregar productos (genera comandas)
5. GET  /api/restaurant/kitchen             → vista cocina (comandas pendientes)
6. PUT  /api/restaurant/comandas/:id/status → cambiar estado { status: "lista" }
7. POST /api/restaurant/sessions/:id/bill   → cobrar, registrar pagos, cerrar mesa
```
