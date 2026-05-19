# API Módulo Restaurante — Tukifac ERP

> **Base URL:** `https://{tenant}.tukifac.app/api/restaurant`  
> **Autenticación:** `Authorization: Bearer <token>` requerido en todos los endpoints.  
> **Tenant:** El tenant se identifica automáticamente por subdominio o header `X-Tenant-Slug`.

---

## Índice

1. [Pisos / Salas](#1-pisos--salas)
2. [Mesas](#2-mesas)
3. [Mozos](#3-mozos)
4. [Sesiones de Mesa](#4-sesiones-de-mesa)
5. [Pedidos (Órdenes)](#5-pedidos-órdenes)
6. [Comandas](#6-comandas)
7. [Vista de Cocina](#7-vista-de-cocina)
8. [Cobro y Cierre de Mesa](#8-cobro-y-cierre-de-mesa)
9. [Pedido Rápido sin Mesa](#9-pedido-rápido-sin-mesa)
10. [Pagos Múltiples en Venta](#10-pagos-múltiples-en-venta)
11. [Caja del Restaurante](#11-caja-del-restaurante)
12. [Flujo Completo](#12-flujo-completo-mesacomandaventapagoscierre)

---

## 1. Pisos / Salas

### `GET /api/restaurant/floors`
Lista todos los pisos o salas activas del restaurante.

**Response 200:**
```json
{
  "data": [
    { "id": 1, "name": "Planta Baja", "sort_order": 1, "active": true }
  ]
}
```

---

### `POST /api/restaurant/floors`
Crea un nuevo piso o sala.

**Body:**
```json
{ "name": "Terraza", "sort_order": 2 }
```

**Response 201:**
```json
{ "success": true, "data": { "id": 3, "name": "Terraza", "sort_order": 2, "active": true } }
```

---

### `PUT /api/restaurant/floors/:id`
Actualiza un piso existente.

**Body:**
```json
{ "name": "Terraza Principal", "sort_order": 2, "active": true }
```

**Response 200:** `{ "success": true }`

---

### `DELETE /api/restaurant/floors/:id`
Elimina un piso. Falla si tiene mesas activas.

**Response 200:** `{ "success": true }`  
**Response 400:** `{ "error": "no se puede eliminar un piso con mesas activas" }`

---

## 2. Mesas

### `GET /api/restaurant/tables?floor_id=1`
Lista mesas con su estado actual, total acumulado y mozo asignado.

| Query param | Tipo | Descripción |
|---|---|---|
| `floor_id` | uint | Filtrar por piso (opcional) |

**Response 200:**
```json
{
  "data": [
    {
      "id": 1,
      "floor_id": 1,
      "name": "Mesa 01",
      "capacity": 4,
      "status": "ocupada",
      "active": true,
      "floor_name": "Planta Baja",
      "session_id": 5,
      "total_amount": 85.00,
      "waiter_name": "Carlos"
    },
    {
      "id": 2,
      "floor_id": 1,
      "name": "Mesa 02",
      "capacity": 2,
      "status": "libre",
      "active": true,
      "floor_name": "Planta Baja",
      "session_id": null,
      "total_amount": 0,
      "waiter_name": ""
    }
  ]
}
```

**Estados de mesa:**
| Estado | Descripción |
|---|---|
| `libre` | Mesa disponible |
| `ocupada` | Mesa con sesión activa |
| `en_consumo` | Reservado para uso futuro |

---

### `POST /api/restaurant/tables`
Crea una nueva mesa.

**Body:**
```json
{ "floor_id": 1, "name": "Mesa 05", "capacity": 6 }
```

**Response 201:**
```json
{ "success": true, "data": { "id": 5, "floor_id": 1, "name": "Mesa 05", "capacity": 6, "status": "libre" } }
```

---

### `PUT /api/restaurant/tables/:id`
Actualiza una mesa.

**Body:**
```json
{ "name": "Mesa VIP", "capacity": 8, "active": true }
```

**Response 200:** `{ "success": true }`

---

### `DELETE /api/restaurant/tables/:id`
Elimina una mesa. Falla si tiene sesión activa.

**Response 400:** `{ "error": "la mesa tiene una sesión activa, no se puede eliminar" }`

---

### `GET /api/restaurant/tables/:id/session`
Obtiene la sesión activa de una mesa (con pedidos y comandas).

**Response 200:** Ver [SessionDetail](#sessiondetail-object)  
**Response 404:** `{ "error": "no hay sesión activa en esta mesa" }`

---

## 3. Mozos

### `GET /api/restaurant/waiters`
Lista todos los mozos activos.

**Response 200:**
```json
{
  "data": [
    { "id": 1, "name": "Carlos Quispe", "code": "CQ01", "user_id": null, "active": true }
  ]
}
```

---

### `POST /api/restaurant/waiters`
Crea un mozo.

**Body:**
```json
{ "name": "Ana López", "code": "AL02", "user_id": null }
```

| Campo | Tipo | Requerido | Descripción |
|---|---|---|---|
| `name` | string | Sí | Nombre completo |
| `code` | string | No | Código corto de identificación |
| `user_id` | uint\|null | No | Vinculación a usuario del sistema |

**Response 201:** `{ "success": true, "data": { ... } }`

---

### `PUT /api/restaurant/waiters/:id`
Actualiza un mozo.

**Body:**
```json
{ "name": "Ana López Torres", "code": "AL02", "active": true }
```

---

### `DELETE /api/restaurant/waiters/:id`
Elimina (soft delete) un mozo.

---

## 4. Sesiones de Mesa

Una **sesión** representa el ciclo completo de una mesa: desde que se abre hasta que se cobra.

### `POST /api/restaurant/sessions`
Abre una mesa o inicia un pedido rápido.

**Body:**
```json
{
  "table_id": 1,
  "waiter_id": 2,
  "guests": 3,
  "notes": "Cliente VIP"
}
```

| Campo | Tipo | Requerido | Descripción |
|---|---|---|---|
| `table_id` | uint\|null | No | Si es `null`, es pedido rápido sin mesa |
| `waiter_id` | uint\|null | No | Mozo asignado |
| `guests` | int | No | Número de comensales (default: 1) |
| `notes` | string | No | Observaciones generales |

**Response 201:**
```json
{
  "success": true,
  "data": {
    "id": 7,
    "table_id": 1,
    "waiter_id": 2,
    "user_id": 3,
    "branch_id": 1,
    "guests": 3,
    "opened_at": "2026-03-01T12:00:00Z",
    "status": "open",
    "total_amount": 0
  }
}
```

**Error 400:** `{ "error": "la mesa 'Mesa 01' ya está ocupada" }`

---

### `GET /api/restaurant/sessions/:id`
Obtiene los detalles completos de una sesión (pedidos + comandas).

#### SessionDetail object:
```json
{
  "data": {
    "id": 7,
    "table_id": 1,
    "table_name": "Mesa 01",
    "floor_name": "Planta Baja",
    "waiter_name": "Carlos",
    "guests": 3,
    "opened_at": "2026-03-01T12:00:00Z",
    "status": "open",
    "total_amount": 85.00,
    "orders": [
      {
        "id": 10,
        "session_id": 7,
        "order_number": 1,
        "notes": "Primera ronda",
        "status": "active",
        "comandas": [
          {
            "id": 20,
            "order_id": 10,
            "product_name": "Lomo Saltado",
            "quantity": 2,
            "unit_price": 35.00,
            "notes": "Sin ají",
            "status": "preparacion",
            "printed": true
          }
        ]
      }
    ]
  }
}
```

---

### `POST /api/restaurant/sessions/:id/cancel`
Cancela una sesión sin cobrar y libera la mesa.

**Body:** `{ "reason": "Cliente se retiró" }`

**Regla:** Solo funciona si la sesión está en estado `open`.

---

## 5. Pedidos (Órdenes)

Un pedido (orden) es una ronda de ítems agregados a una sesión. Cada pedido genera comandas independientes.

### `POST /api/restaurant/sessions/:id/orders`
Agrega un pedido a una sesión abierta.

**Body:**
```json
{
  "waiter_id": 2,
  "notes": "Pedir bien caliente",
  "items": [
    {
      "product_id": 15,
      "product_code": "LOMO-01",
      "product_name": "Lomo Saltado",
      "quantity": 2,
      "unit_price": 35.00,
      "notes": "Sin ají"
    },
    {
      "product_id": 15,
      "product_code": "LOMO-01",
      "product_name": "Lomo Saltado",
      "quantity": 1,
      "unit_price": 35.00,
      "notes": "Con extra arroz"
    }
  ]
}
```

> **Regla importante:** Dos ítems del mismo producto generan **dos comandas independientes**. No se agrupan. Esto permite instrucciones distintas por plato.

**Response 201:**
```json
{
  "success": true,
  "data": {
    "id": 11,
    "session_id": 7,
    "order_number": 2,
    "status": "active",
    "comandas": [
      { "id": 25, "product_name": "Lomo Saltado", "quantity": 2, "status": "pendiente" },
      { "id": 26, "product_name": "Lomo Saltado", "quantity": 1, "notes": "Con extra arroz", "status": "pendiente" }
    ]
  }
}
```

---

## 6. Comandas

### `PUT /api/restaurant/comandas/:id/status`
Cambia el estado de una comanda.

**Body:** `{ "status": "preparacion" }`

| Estado | Descripción |
|---|---|
| `pendiente` | Recién generada, esperando cocina |
| `preparacion` | En preparación |
| `lista` | Lista para ser llevada |
| `entregada` | Entregada al cliente |

**Flujo recomendado:** `pendiente → preparacion → lista → entregada`

---

### `POST /api/restaurant/comandas/:id/print`
Marca una comanda como impresa.

**Response 200:** `{ "success": true }`

---

### `DELETE /api/restaurant/comandas/:id`
Anula una comanda. **Requiere verificación del PIN de seguridad** configurado en Ajustes del Restaurante (panel tenant).

**Body:**
```json
{
  "reason": "Error en el pedido",
  "pin": "1234"
}
```

| Campo | Tipo | Requerido | Descripción |
|---|---|---|---|
| `reason` | string | Sí | Motivo de anulación |
| `pin` | string | Sí | PIN de operación (el configurado en Ajustes del Restaurante) |

**Reglas:**
- Requiere motivo obligatorio.
- Requiere PIN correcto (el mismo que se configura en Mi empresa → Módulos → Restaurante → Ajustes).
- No se puede anular una comanda ya entregada.
- La anulación descuenta automáticamente del total de la sesión.

**Response 200:** `{ "success": true }`  
**Response 400:** `{ "error": "PIN incorrecto" }` o `{ "error": "no se puede anular una comanda ya entregada" }`

---

## 7. Vista de Cocina

### `GET /api/restaurant/kitchen`
Retorna todas las comandas pendientes o en preparación de la sucursal activa.

**Response 200:**
```json
{
  "data": [
    {
      "id": 20,
      "session_id": 7,
      "product_name": "Lomo Saltado",
      "quantity": 2,
      "notes": "Sin ají",
      "status": "pendiente",
      "printed": false,
      "created_at": "2026-03-01T12:05:00Z"
    }
  ]
}
```

> Ordenadas por fecha de creación (más antiguas primero). Usar `PUT /api/restaurant/comandas/:id/status` para avanzar el estado desde cocina.

---

## 8. Cobro y Cierre de Mesa

### `POST /api/restaurant/sessions/:id/bill`
Cierra la sesión, genera una venta formal y registra los pagos.

**Body:**
```json
{
  "series_id": 3,
  "doc_type": "boleta",
  "currency": "PEN",
  "cash_session_id": 1,
  "issue_date": "2026-03-01",
  "payments": [
    { "method": "efectivo", "amount": 50.00, "reference": "", "notes": "" },
    { "method": "yape",     "amount": 35.00, "reference": "OP-1234", "notes": "" }
  ]
}
```

| Campo | Tipo | Requerido | Descripción |
|---|---|---|---|
| `series_id` | uint | Sí | ID de la serie de documento |
| `doc_type` | string | Sí | `boleta` o `factura` |
| `currency` | string | No | `PEN` (default) |
| `cash_session_id` | uint\|null | No | Sesión de caja activa |
| `issue_date` | string | No | Fecha `YYYY-MM-DD` (default: hoy) |
| `payments` | array | Sí | Al menos un método de pago |

**Reglas:**
- La suma de `payments[].amount` debe ser ≥ total de la sesión.
- Si hay vuelto, el exceso no se registra como deuda.
- La mesa se libera automáticamente.
- Las comandas se marcan como `entregada`.

**Response 201:**
```json
{
  "success": true,
  "data": {
    "id": 55,
    "number": "B001-00000055",
    "doc_type": "boleta",
    "subtotal": 72.03,
    "tax_amount": 12.97,
    "total": 85.00,
    "status": "paid"
  }
}
```

**Métodos de pago soportados:**

| Valor | Descripción |
|---|---|
| `efectivo` | Pago en efectivo |
| `tarjeta` | Tarjeta de débito/crédito |
| `transferencia` | Transferencia bancaria |
| `yape` | Billetera digital Yape |
| `plin` | Billetera digital Plin |
| `credito` | Crédito al cliente |

---

## 9. Pedido Rápido sin Mesa

Para crear pedidos directos a cocina sin asignar mesa, usar el mismo endpoint de apertura de sesión omitiendo `table_id`:

### `POST /api/restaurant/sessions`
```json
{
  "table_id": null,
  "waiter_id": 1,
  "notes": "Para llevar"
}
```

Luego agregar productos con `POST /api/restaurant/sessions/:id/orders` y cobrar con `POST /api/restaurant/sessions/:id/bill`.

---

## 10. Pagos Múltiples en Venta

Para ventas generadas fuera del módulo de restaurante (POS, etc.).

### `POST /api/sales/:id/payments`
Registra uno o más pagos para una venta existente.

**Body:**
```json
{
  "payments": [
    { "method": "efectivo",      "amount": 100.00, "reference": "" },
    { "method": "transferencia", "amount": 50.00,  "reference": "TRF-9876" }
  ]
}
```

**Reglas:**
- La suma de todos los pagos (existentes + nuevos) debe cubrir el `total` de la venta.
- Si la venta ya tiene pagos registrados, se suman a los nuevos.

**Response 201:** `{ "success": true }`  
**Response 400:** `{ "error": "el total pagado (100.00) es menor al total de la venta (150.00)" }`

---

### `GET /api/sales/:id/payments`
Obtiene todos los pagos de una venta.

**Response 200:**
```json
{
  "data": [
    { "id": 1, "sale_id": 55, "method": "efectivo",      "amount": 50.00, "reference": "" },
    { "id": 2, "sale_id": 55, "method": "yape",          "amount": 35.00, "reference": "OP-1234" }
  ]
}
```

---

## 11. Caja del Restaurante

El módulo de restaurante reutiliza el sistema de caja general del ERP. No es necesario un módulo separado.

### Apertura de caja
```
POST /api/cashbank/cash/open
```
```json
{
  "branch_id": 1,
  "opening_balance": 200.00,
  "notes": "Turno mañana"
}
```

### Cierre de caja
```
POST /api/cashbank/cash/:id/close
```
```json
{
  "closing_balance": 1450.00,
  "notes": "Todo cuadrado"
}
```

### Estado de la caja
```
GET /api/cashbank/cash
```
Retorna la sesión activa con totales de ingresos/egresos del turno.

> Cada venta de restaurante puede asociarse a la sesión de caja activa mediante el campo `cash_session_id` en el cobro de mesa.

---

## 12. Flujo Completo: Mesa → Comanda → Venta → Pagos → Cierre

```
1. Abrir mesa
   POST /api/restaurant/sessions
   { "table_id": 3, "waiter_id": 1, "guests": 2 }
   → Retorna session_id: 7
   → Mesa cambia a estado "ocupada"

2. Primera ronda de pedidos
   POST /api/restaurant/sessions/7/orders
   { "items": [{ "product_name": "Ceviche", "quantity": 1, "unit_price": 45.00 }] }
   → Se generan comandas en estado "pendiente"

3. Cocina ve comandas
   GET /api/restaurant/kitchen
   → Lista todas las comandas pendientes

4. Cocina actualiza estado
   PUT /api/restaurant/comandas/25/status  { "status": "preparacion" }
   PUT /api/restaurant/comandas/25/status  { "status": "lista" }

5. Mozo entrega y actualiza
   PUT /api/restaurant/comandas/25/status  { "status": "entregada" }

6. Segunda ronda (opcional)
   POST /api/restaurant/sessions/7/orders
   { "items": [...] }

7. El cliente pide la cuenta
   GET /api/restaurant/sessions/7
   → Ver total acumulado en total_amount

8. Cobrar (pago mixto)
   POST /api/restaurant/sessions/7/bill
   {
     "series_id": 3,
     "doc_type": "boleta",
     "payments": [
       { "method": "efectivo", "amount": 50.00 },
       { "method": "tarjeta",  "amount": 25.00 }
     ]
   }
   → Se genera venta formal
   → Mesa vuelve a estado "libre"
   → Comandas marcadas como "entregada"

9. (Opcional) Ver pagos de la venta
   GET /api/sales/55/payments
```

---

## Errores comunes

| Código | Mensaje | Causa |
|---|---|---|
| 400 | `la mesa 'X' ya está ocupada` | Intentar abrir una mesa que ya tiene sesión activa |
| 400 | `la sesión ya está cerrada o facturada` | Intentar cobrar una sesión ya procesada |
| 400 | `monto pagado es menor al total` | Los pagos no cubren el total de la sesión |
| 400 | `se requiere motivo de anulación` | Anular comanda sin especificar razón |
| 400 | `se requiere motivo de anulación y PIN` | Falta reason o pin en el body |
| 400 | `PIN incorrecto` | El pin enviado no coincide con el configurado |
| 400 | `no se puede anular una comanda ya entregada` | Estado final, no reversible |
| 404 | `no hay sesión activa en esta mesa` | Mesa está libre |
| 401 | `Token inválido o expirado` | Sin Bearer token o token vencido |

---

## Seguridad

- Todos los endpoints requieren `Authorization: Bearer <token>` válido.
- El token se obtiene en `POST /login` con las credenciales del usuario tenant.
- El tenant se identifica por subdominio (ej: `empresa1.tukifac.app`) o header `X-Tenant-Slug: empresa1`.
- Los datos están completamente aislados por base de datos por tenant.
- **Anulación de comandas:** se debe enviar en el body de `DELETE /api/restaurant/comandas/:id` el campo `pin` con el PIN configurado en Ajustes del Restaurante (panel tenant). Si no hay PIN configurado, configurarlo en Mi empresa → Módulos → Restaurante → Ajustes.

### Ajustes del módulo (PIN)

- **GET /api/restaurant/settings** — Retorna `{ "has_deletion_pin": true }` o `false` (no expone el PIN).
- **PUT /api/restaurant/settings** — Guarda el PIN. Body: `{ "deletion_pin": "1234" }`. Se configura desde el panel tenant (Ajustes del Restaurante).
