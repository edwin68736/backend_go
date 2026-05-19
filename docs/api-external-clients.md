# API para clientes externos (Kotlin, Flutter, etc.)

Esta guía describe cómo integrar un frontend externo (app móvil en Kotlin/Android, Flutter, React Native, etc.) con la API de Tukifac. El flujo es: **resolver la empresa por RUC en la base central** → obtener el **slug** → enviar **todas las peticiones al mismo dominio** añadiendo el header `X-Tenant-Slug`.

---

## 1. URL base de la API

Todas las peticiones van al **mismo dominio**:

| Entorno   | URL base              |
|----------|------------------------|
| Producción | `https://api.tukifac.cloud` |

No hay un dominio distinto por tenant. El tenant se identifica con el header `X-Tenant-Slug` en cada request.

---

## 2. Flujo resumido

```
┌─────────────────────────────────────────────────────────────────────────┐
│ 1. Usuario ingresa RUC de su empresa                                     │
│    → GET /api/public/tenant-by-ruc?ruc=20123456789                       │
│    ← { "slug": "miempresa", "name": "Mi Empresa S.A.C.", ... }           │
└─────────────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────────────┐
│ 2. Guardar slug en la app (memoria o preferencias)                       │
└─────────────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────────────┐
│ 3. Todas las peticiones siguientes incluyen:                            │
│    - Header: X-Tenant-Slug: miempresa                                    │
│    - Mismo dominio: https://api.tukifac.cloud                             │
└─────────────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────────────┐
│ 4. Login (sin token aún):                                                │
│    POST /api/login                                                       │
│    Headers: X-Tenant-Slug: miempresa, Content-Type: application/json    │
│    Body: { "email": "usuario@empresa.com", "password": "***" }           │
│    ← { "token": "eyJ...", "user": {...}, "modules": [...], ... }         │
└─────────────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────────────┐
│ 5. Peticiones autenticadas:                                              │
│    Headers: X-Tenant-Slug: miempresa, Authorization: Bearer <token>      │
│    Ej: GET /api/dashboard/stats, GET /api/products, POST /api/sales, ... │
└─────────────────────────────────────────────────────────────────────────┘
```

---

## 3. Paso 1 — Obtener el slug por RUC (público)

Antes de login, la app debe saber a qué empresa pertenece el usuario. Se consulta la **base central** con el RUC (sin autenticación).

**Request**

```http
GET https://api.tukifac.cloud/api/public/tenant-by-ruc?ruc=20123456789
```

**Respuesta 200**

```json
{
  "slug": "miempresa",
  "name": "Mi Empresa S.A.C.",
  "token_consulta_datos": "opcional-token-para-otras-consultas"
}
```

**Errores**

- `400`: falta el parámetro `ruc`.
- `404`: no existe una empresa activa con ese RUC.

La app debe **guardar `slug`** y usarlo en el header `X-Tenant-Slug` en todas las peticiones siguientes (login y resto de la API).

---

## 4. Paso 2 — Login del tenant

**Request**

```http
POST https://api.tukifac.cloud/api/login
Content-Type: application/json
X-Tenant-Slug: miempresa

{
  "email": "usuario@empresa.com",
  "password": "tu_password"
}
```

**Respuesta 200**

```json
{
  "token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
  "user": {
    "id": 1,
    "name": "Juan Pérez",
    "email": "usuario@empresa.com",
    "role": "Administrador"
  },
  "modules": ["sales", "products", "restaurant", ...],
  "permissions": ["sales.create", "products.edit", ...],
  "subscription": {
    "plan_name": "pro",
    "status": "active",
    "start_date": "2025-01-01",
    "end_date": "2025-12-31"
  }
}
```

**Errores**

- `401`: credenciales incorrectas.
- `403`: empresa suspendida o inactiva (mensaje en el body).
- `404`: empresa no encontrada (slug incorrecto o no existe).

La app debe **guardar `token`** y enviarlo en `Authorization: Bearer <token>` en todas las peticiones que requieran autenticación.

---

## 5. Paso 3 — Peticiones a la API del tenant

Todas las rutas bajo `/api/...` (excepto las públicas) requieren:

1. **Header `X-Tenant-Slug`**: el slug obtenido en el paso 1.
2. **Header `Authorization`**: `Bearer <token>` obtenido en el login (salvo en endpoints públicos).

**Ejemplos**

```http
GET https://api.tukifac.cloud/api/dashboard/stats
X-Tenant-Slug: miempresa
Authorization: Bearer eyJhbGciOiJIUzI1NiIs...

GET https://api.tukifac.cloud/api/products?page=1&per_page=20
X-Tenant-Slug: miempresa
Authorization: Bearer eyJhbGciOiJIUzI1NiIs...

POST https://api.tukifac.cloud/api/sales
X-Tenant-Slug: miempresa
Authorization: Bearer eyJhbGciOiJIUzI1NiIs...
Content-Type: application/json

{ ... cuerpo de la venta ... }
```

Si falta `X-Tenant-Slug` o el slug no existe, la API responde `404` con mensaje tipo "Empresa no encontrada". Si el token es inválido o expirado, responde `401` y la app debe redirigir a login.

---

## 6. Resumen para implementar (ej. Kotlin)

1. **Configuración**
   - Base URL: `https://api.tukifac.cloud` (producción) o `http://localhost:3000` (desarrollo).
   - Cabeceras comunes: `Content-Type: application/json`, y cuando corresponda `X-Tenant-Slug` y `Authorization`.

2. **Al iniciar o al pedir “empresa por RUC”**
   - Llamar `GET /api/public/tenant-by-ruc?ruc=<RUC>`.
   - Guardar `slug` (y opcionalmente `name`, `token_consulta_datos`) en preferencias o memoria.

3. **Login**
   - `POST /api/login` con body `{ "email", "password" }` y header `X-Tenant-Slug: <slug>`.
   - Guardar `token` y datos de `user` / `subscription` que necesites.

4. **Resto de la API**
   - Misma base URL.
   - En cada request: `X-Tenant-Slug: <slug>` y `Authorization: Bearer <token>`.

5. **Manejo de errores**
   - `401`: token inválido o expirado → cerrar sesión y volver a login.
   - `404` con mensaje "Empresa no encontrada": slug inválido o empresa dada de baja → pedir de nuevo el RUC o mostrar mensaje al usuario.

---

## 7. CORS y desarrollo

En producción, CORS está configurado para orígenes del panel (p. ej. `https://app.tukifac.cloud` y subdominios). Las **apps nativas (Kotlin, Flutter, etc.)** no envían cabecera `Origin` de navegador, por lo que no dependen de CORS; solo deben usar la URL base correcta y los headers indicados. Para pruebas con un cliente web en otro dominio, puede ser necesario añadir ese origen en la configuración CORS del backend.
