# Guía de Implementación — Módulo Restaurante Tukifac

> Documento para implementar un cliente del módulo restaurante en cualquier tecnología (Kotlin, Rust, .NET, etc.) para Windows, móvil o escritorio.

---

## Índice

1. [Resumen](#1-resumen)
2. [Autenticación y Tenant](#2-autenticación-y-tenant)
3. [Configuración del Cliente HTTP](#3-configuración-del-cliente-http)
4. [Modelos de Datos](#4-modelos-de-datos)
5. [Endpoints por Módulo](#5-endpoints-por-módulo)
6. [Flujos de Negocio](#6-flujos-de-negocio)
7. [Roles y Permisos](#7-roles-y-permisos)
8. [Guía de Implementación por Tecnología](#8-guía-de-implementación-por-tecnología)
9. [Manejo de Errores](#9-manejo-de-errores)

---

## 1. Resumen

El módulo restaurante de Tukifac es un ERP multi-tenant. Cada cliente (frontend) debe:

1. **Identificar el tenant** por RUC (antes del login)
2. **Autenticarse** con email y contraseña
3. **Enviar en cada request** el token JWT y el slug del tenant
4. **Implementar las vistas** según los roles del usuario

### Arquitectura

```
┌─────────────────┐     HTTPS      ┌──────────────────────┐
│  Cliente        │ ◄────────────► │  API Tukifac          │
│  (Kotlin/Rust/  │   JSON REST    │  Base URL + /api/...  │
│   Windows)      │                │  Multi-tenant         │
└─────────────────┘                └──────────────────────┘
```

### Referencia de API Detallada

Para la documentación completa de cada endpoint (request/response, reglas de negocio), consultar: **[api-restaurant.md](./api-restaurant.md)**

---

## 2. Autenticación y Tenant

### 2.1 Flujo Inicial (Sin Autenticación)

#### Paso 1: Obtener tenant por RUC

**Endpoint:** `GET /api/public/tenant-by-ruc?ruc={ruc}`

- **Sin token** ni headers especiales
- **ruc:** Solo dígitos, 8–11 caracteres

**Request:**
```http
GET /api/public/tenant-by-ruc?ruc=20123456789 HTTP/1.1
Host: api.tukifac.app
Content-Type: application/json
```

**Response 200:**
```json
{
  "slug": "mi-empresa",
  "name": "Mi Empresa SAC",
  "token_consulta_datos": "opcional-token-sunat"
}
```

**Almacenar localmente:** `slug`, `name`, `token_consulta_datos` (para uso futuro).

**Error 404/400:** `{ "error": "Empresa no encontrada con ese RUC" }`

---

#### Paso 2: Login

**Endpoint:** `POST /api/login`

**Headers obligatorios:**
- `Content-Type: application/json`
- `X-Tenant-Slug: {slug}` ← del paso 1

**Body:**
```json
{
  "email": "usuario@empresa.com",
  "password": "contraseña",
  "slug": "mi-empresa"
}
```

**Response 200:**
```json
{
  "token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
  "user": {
    "id": 1,
    "name": "Juan Pérez",
    "email": "usuario@empresa.com",
    "role": "admin",
    "restaurant_role": "admin"
  },
  "modules": ["restaurant"],
  "permissions": []
}
```

**Almacenar:** `token`, `user` (incluyendo `restaurant_role`).

**Error 401:** `{ "error": "credenciales inválidas" }`

---

### 2.2 Uso del Token

En **todos** los requests posteriores (excepto `/api/public/*`):

```http
Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...
X-Tenant-Slug: mi-empresa
Content-Type: application/json
```

- Si el token expira (401), redirigir al login.
- El `X-Tenant-Slug` es **obligatorio** en desarrollo y cuando no se usa subdominio.

---

### 2.3 Base URL

| Entorno   | Base URL                    |
|-----------|-----------------------------|
| Producción| `https://api.tukifac.app`    |
| Local     | `http://localhost:3000`     |

En producción con subdominio: `https://{slug}.tukifac.app` (el tenant puede inferirse del host).

---

## 3. Configuración del Cliente HTTP

### Headers por Defecto

```
Content-Type: application/json
Authorization: Bearer {token}        # después del login
X-Tenant-Slug: {slug}                # siempre, salvo /api/public/*
```

### Códigos de Respuesta

| Código | Significado                          |
|--------|--------------------------------------|
| 200    | OK                                   |
| 201    | Creado                               |
| 400    | Error de validación o negocio        |
| 401    | No autenticado / token inválido      |
| 404    | Recurso no encontrado                |
| 500    | Error interno del servidor           |

---

## 4. Modelos de Datos

### Tipos Comunes

```typescript
// Pseudocódigo — adaptar a tu lenguaje

interface Floor {
  id: number
  name: string
  sort_order: number
  active: boolean
}

interface RestaurantTable {
  id: number
  floor_id: number
  floor_name?: string
  name: string
  capacity: number
  status: "libre" | "ocupada" | "en_consumo"
  active: boolean
  session_id?: number | null
  total_amount?: number
  waiter_name?: string
}

interface Waiter {
  id: number
  name: string
  code: string
  active: boolean
}

interface Product {
  id: number
  code: string
  name: string
  image_url?: string | null
  sale_price: number
  unit: string
  category_id?: number | null
  preparation_area?: string | null
  has_modifiers?: boolean
  is_restaurant: boolean
  active: boolean
}

interface Comanda {
  id: number
  order_id: number
  session_id: number
  product_name: string
  quantity: number
  unit_price: number
  notes?: string
  status: "pendiente" | "preparacion" | "lista" | "entregada"
  printed?: boolean
  created_at?: string
}

interface SessionDetail {
  id: number
  table_id: number | null
  table_name?: string
  floor_name?: string
  waiter_name?: string
  guests: number
  opened_at: string
  status: string
  total_amount: number
  orders: Array<{
    id: number
    order_number: number
    notes: string
    comandas: Comanda[]
  }>
}

interface Contact {
  id: number
  business_name: string
  doc_type: string
  doc_number: string
  type: "customer" | "supplier" | "both"
}

interface SeriesRow {
  id: number
  doc_type: string
  series: string
  sunat_code: string
  category: string
}
```

### URL de Imágenes de Producto

Si `image_url` es relativo (ej: `/uploads/products/123.jpg`), concatenar con la base URL:

```
{baseUrl}{image_url}
```

Si ya empieza con `http://` o `https://`, usarlo tal cual.

---

## 5. Endpoints por Módulo

### 5.1 Público (sin token)

| Método | Ruta | Descripción |
|--------|------|-------------|
| GET | `/api/public/tenant-by-ruc?ruc=` | Obtener tenant por RUC |

### 5.2 Autenticación

| Método | Ruta | Descripción |
|--------|------|-------------|
| POST | `/api/login` | Login (requiere `X-Tenant-Slug`) |

### 5.3 Restaurante — Pisos

| Método | Ruta | Descripción |
|--------|------|-------------|
| GET | `/api/restaurant/floors` | Listar pisos |
| POST | `/api/restaurant/floors` | Crear piso |
| PUT | `/api/restaurant/floors/:id` | Actualizar piso |
| DELETE | `/api/restaurant/floors/:id` | Eliminar piso |

### 5.4 Restaurante — Mesas

| Método | Ruta | Descripción |
|--------|------|-------------|
| GET | `/api/restaurant/tables?floor_id=` | Listar mesas |
| POST | `/api/restaurant/tables` | Crear mesa |
| PUT | `/api/restaurant/tables/:id` | Actualizar mesa |
| DELETE | `/api/restaurant/tables/:id` | Eliminar mesa |
| GET | `/api/restaurant/tables/:id/session` | Sesión activa de la mesa |

### 5.5 Restaurante — Mozos

| Método | Ruta | Descripción |
|--------|------|-------------|
| GET | `/api/restaurant/waiters` | Listar mozos |
| POST | `/api/restaurant/waiters` | Crear mozo |
| PUT | `/api/restaurant/waiters/:id` | Actualizar mozo |
| DELETE | `/api/restaurant/waiters/:id` | Eliminar mozo |

### 5.6 Restaurante — Sesiones y Pedidos

| Método | Ruta | Descripción |
|--------|------|-------------|
| POST | `/api/restaurant/sessions` | Abrir mesa o pedido rápido |
| GET | `/api/restaurant/sessions/:id` | Detalle de sesión |
| POST | `/api/restaurant/sessions/:id/cancel` | Cancelar sesión |
| POST | `/api/restaurant/sessions/:id/orders` | Agregar pedido |
| POST | `/api/restaurant/sessions/:id/bill` | Cobrar y generar venta |
| POST | `/api/restaurant/sessions/:id/close` | Cerrar mesa sin cobrar |

### 5.7 Restaurante — Comandas

| Método | Ruta | Descripción |
|--------|------|-------------|
| PUT | `/api/restaurant/comandas/:id/status` | Cambiar estado |
| POST | `/api/restaurant/comandas/:id/print` | Marcar como impresa |
| DELETE | `/api/restaurant/comandas/:id` | Anular (requiere PIN) |
| GET | `/api/restaurant/kitchen` | Vista cocina (comandas activas) |

### 5.8 Restaurante — Configuración

| Método | Ruta | Descripción |
|--------|------|-------------|
| GET | `/api/restaurant/settings` | ¿Hay PIN configurado? |

### 5.9 Productos

| Método | Ruta | Descripción |
|--------|------|-------------|
| GET | `/api/products?restaurant_only=true&active_only=true&page=1&per_page=0` | Todos los productos restaurante |
| GET | `/api/products/:id` | Detalle de producto |

### 5.10 Categorías

| Método | Ruta | Descripción |
|--------|------|-------------|
| GET | `/api/categories` | Listar categorías |
| POST | `/api/categories` | Crear categoría |

### 5.11 Grupos de Modificadores

| Método | Ruta | Descripción |
|--------|------|-------------|
| GET | `/api/modifier-groups` | Listar grupos |
| POST | `/api/modifier-groups` | Crear grupo (name, required, options[]) |

### 5.12 Empresa — Series

| Método | Ruta | Descripción |
|--------|------|-------------|
| GET | `/api/company/series?branch_id=1&category=venta` | Series para comprobantes |

### 5.13 Contactos (Clientes)

| Método | Ruta | Descripción |
|--------|------|-------------|
| GET | `/api/contacts?type=customer` | Listar clientes |

---

## 6. Flujos de Negocio

### 6.1 Flujo: Salas / Mesas (Abrir Mesa)

```
1. GET /api/restaurant/floors
   → Listar pisos

2. GET /api/restaurant/tables?floor_id={id}
   → Listar mesas (floor_id opcional para filtrar)

3. Si mesa.status == "libre":
   POST /api/restaurant/sessions
   Body: { "table_id": mesa.id, "waiter_id": opcional, "guests": 2, "notes": "" }
   → Retorna { "data": { "id": sessionId } }
   → Navegar a vista de mesa con sessionId

4. Si mesa.status == "ocupada":
   → Navegar a vista de mesa con mesa.session_id
```

### 6.2 Flujo: Mesa (Tomar Pedido)

```
1. GET /api/restaurant/sessions/{sessionId}
   → Obtener sesión, pedidos, comandas, total_amount

2. GET /api/products?restaurant_only=true&active_only=true&page=1&per_page=0
   → Listar productos para el carrito

3. Usuario agrega productos al carrito (local)

4. POST /api/restaurant/sessions/{sessionId}/orders
   Body: {
     "items": [
       { "product_id": 1, "product_code": "LOMO", "product_name": "Lomo Saltado", "quantity": 2, "unit_price": 35, "notes": "Sin ají" }
     ]
   }
   → Envía comanda a cocina

5. Repetir 1–4 para más rondas

6. Cobrar:
   GET /api/company/series?branch_id=1&category=venta
   GET /api/contacts?type=customer
   POST /api/restaurant/sessions/{sessionId}/bill
   Body: {
     "series_id": 3,
     "doc_type": "boleta",
     "currency": "PEN",
     "contact_id": opcional,
     "close_session": true,
     "payments": [
       { "method": "efectivo", "amount": 50 },
       { "method": "yape", "amount": 35 }
     ]
   }
   → Genera venta, libera mesa
```

### 6.3 Flujo: POS (Venta Rápida)

```
1. GET /api/products?restaurant_only=true&active_only=true&page=1&per_page=0
   → Productos para seleccionar

2. Usuario arma carrito (local)

3. POST /api/restaurant/sessions
   Body: { "table_id": null, "notes": "POS rápido" }
   → { "data": { "id": sessionId } }

4. POST /api/restaurant/sessions/{sessionId}/orders
   Body: { "items": [ ... ] }
   → Agregar ítems

5. POST /api/restaurant/sessions/{sessionId}/bill
   Body: { "series_id", "doc_type", "contact_id?", "close_session": true, "payments": [...] }
   → Cobrar y cerrar en un solo flujo
```

### 6.4 Flujo: Comandas (Cocina)

```
1. GET /api/restaurant/kitchen
   → Comandas pendientes y en preparación

2. PUT /api/restaurant/comandas/{id}/status
   Body: { "status": "preparacion" }
   → pendiente → preparacion

3. PUT /api/restaurant/comandas/{id}/status
   Body: { "status": "lista" }
   → preparacion → lista

4. (Mozo) PUT /api/restaurant/comandas/{id}/status
   Body: { "status": "entregada" }
   → lista → entregada
```

### 6.5 Anulación de Comanda

```
DELETE /api/restaurant/comandas/{id}
Body: { "reason": "Error en el pedido", "pin": "1234" }

Requiere: PIN configurado en Ajustes del Restaurante (panel tenant).
GET /api/restaurant/settings → { "has_deletion_pin": true }
```

---

## 7. Roles y Permisos

El campo `user.restaurant_role` determina el acceso:

| Rol       | Acceso                                                                 |
|-----------|------------------------------------------------------------------------|
| admin     | Todo (productos, modificadores, mesas, POS, salas, mesa, comandas, cerrar_mesa) |
| vendedor  | POS, salas, mesa, comandas, cerrar_mesa                                |
| mozo       | salas, mesa                                                           |
| cocinero   | comandas                                                              |

**Implementación:** Filtrar menú y rutas según `restaurant_role`. Si el usuario no tiene rol restaurante, mostrar mensaje de “Sin acceso al módulo restaurante”.

---

## 8. Guía de Implementación por Tecnología

### 8.1 Kotlin (Android / Desktop / Ktor Client)

```kotlin
// Ejemplo de cliente HTTP con Ktor
val client = HttpClient(CIO) {
    install(ContentNegotiation) { json(Json { ignoreUnknownKeys = true }) }
    defaultRequest {
        header("Content-Type", "application/json")
        header("X-Tenant-Slug", tenantSlug)
        bearerAuth(token)
    }
}

// Login
suspend fun login(email: String, password: String, slug: String): LoginResponse {
    return client.post("$baseUrl/api/login") {
        setBody(LoginRequest(email, password, slug))
    }.body()
}

// Listar mesas
suspend fun listTables(floorId: Int?): List<RestaurantTable> {
    val params = floorId?.let { "?floor_id=$it" } ?: ""
    val response = client.get<ApiResponse<List<RestaurantTable>>>("$baseUrl/api/restaurant/tables$params")
    return response.data ?: emptyList()
}
```

**Almacenamiento:** Usar `DataStore` (Android) o archivo encriptado (Desktop) para token y slug.

---

### 8.2 Rust (reqwest + serde)

```rust
// Ejemplo con reqwest
let client = reqwest::Client::new();

let res = client
    .post(format!("{}/api/login", base_url))
    .header("Content-Type", "application/json")
    .header("X-Tenant-Slug", &tenant_slug)
    .json(&LoginRequest {
        email: email.to_string(),
        password: password.to_string(),
        slug: Some(tenant_slug.to_string()),
    })
    .send()
    .await?
    .json::<LoginResponse>()
    .await?;

// Requests autenticados
let tables = client
    .get(format!("{}/api/restaurant/tables", base_url))
    .header("Authorization", format!("Bearer {}", token))
    .header("X-Tenant-Slug", &tenant_slug)
    .send()
    .await?
    .json::<ApiResponse<Vec<RestaurantTable>>>()
    .await?;
```

---

### 8.3 Consideraciones para Windows Desktop

1. **Almacenamiento seguro:** No guardar token en texto plano. Usar DPAPI (Windows) o equivalente.
2. **HTTPS:** Validar certificados en producción.
3. **Reintentos:** Implementar retry con backoff en errores 5xx.
4. **Offline:** Cachear productos y categorías para uso sin conexión (opcional).
5. **Imágenes:** Cachear URLs de productos; descargar en segundo plano.

---

## 9. Manejo de Errores

### Errores Comunes

| Código | Mensaje típico | Acción sugerida |
|--------|----------------|-----------------|
| 401 | Token inválido o expirado | Ir a login, limpiar token |
| 400 | la mesa 'X' ya está ocupada | Refrescar lista de mesas |
| 400 | monto pagado es menor al total | Validar suma de pagos antes de enviar |
| 400 | PIN incorrecto | Pedir PIN correcto para anulación |
| 400 | no se puede anular una comanda ya entregada | No permitir anular en UI |
| 404 | no hay sesión activa en esta mesa | Volver a salas |

### Estructura de Error

```json
{
  "error": "mensaje legible"
}
```

Siempre leer `error` del body en respuestas 4xx/5xx.

---

## 10. Resumen de Dependencias entre Endpoints

```
Login
  └─► Token + X-Tenant-Slug en todos los requests

Salas
  ├─► GET floors
  ├─► GET tables
  └─► POST sessions (abrir mesa)

Mesa
  ├─► GET sessions/:id
  ├─► GET products
  ├─► POST sessions/:id/orders
  └─► POST sessions/:id/bill
        ├─► GET company/series
        └─► GET contacts (opcional)

POS
  ├─► GET products
  ├─► POST sessions (table_id: null)
  ├─► POST sessions/:id/orders
  └─► POST sessions/:id/bill

Comandas
  ├─► GET kitchen
  └─► PUT comandas/:id/status
```

---

## Referencias

- **[api-restaurant.md](./api-restaurant.md)** — Documentación detallada de cada endpoint
- **Base URL:** Configurable por entorno (producción vs local)
- **Formato:** JSON en request y response
- **Encoding:** UTF-8
