# Despliegue en producción — Tukifac SaaS

Esta es la arquitectura de URLs para la que está pensado el backend central:

| Rol | URL | Descripción |
|-----|-----|-------------|
| **Backend (API)** | `https://api.tukifac.cloud` | Servidor donde corre esta API (Go/Fiber). |
| **Panel central (SaaS)** | `https://app.tukifac.cloud` | Frontend Super Admin (empresas, planes, suscripciones). |
| **Tenants** | `https://tenant1.app.tukifac.cloud`, `https://tenant2.app.tukifac.cloud`, … | Mismo frontend tenant; el subdominio identifica la empresa. |

El backend **solo** se despliega en `api.tukifac.cloud`. Los frontends (panel central y panel tenant) se sirven desde `app.tukifac.cloud` y sus subdominios; todos llaman a `https://api.tukifac.cloud`.

---

## 1. Backend (API)

### Variables de entorno

En el servidor donde corre la API (`api.tukifac.cloud`):

```env
APP_ENV=production
# Dominio del FRONTEND (donde cargan app y tenants). No uses el dominio raíz (tukifac.cloud):
# correcto: app.tukifac.cloud → CORS y que api.tukifac.cloud no se interprete como tenant
# incorrecto: tukifac.cloud → el Host api.tukifac.cloud se interpretaría como tenant "api"
APP_DOMAIN=app.tukifac.cloud
PORT=3000

# CORS permitirá automáticamente:
# - https://app.tukifac.cloud (panel central)
# - https://*.app.tukifac.cloud (cualquier tenant: tenant1.app.tukifac.cloud, etc.)

FRONTEND_URL=https://app.tukifac.cloud
CENTRAL_FRONTEND_URL=https://app.tukifac.cloud

# Base de datos, JWT, etc. (valores seguros en producción)
DB_HOST=...
JWT_SECRET=...
SA_JWT_SECRET=...
```

### Resolución del tenant

- Las peticiones llegan al backend con `Host: api.tukifac.cloud` (no con el subdominio del tenant).
- El tenant se identifica **solo por el header `X-Tenant-Slug`** que envía el frontend.
- El frontend en `tenant1.app.tukifac.cloud` obtiene el slug del hostname (`tenant1`) y lo envía en cada request.

No hace falta configurar subdominios en el backend; solo CORS (ya resuelto con `APP_DOMAIN`) y que los frontends envíen `X-Tenant-Slug`.

### Proxy inverso (Nginx) delante de la API

Si `api.tukifac.cloud` pasa por Nginx (u otro proxy) antes de llegar al Go:

- **Tienes que reenviar el método OPTIONS** al backend. El navegador hace primero una petición OPTIONS (preflight) y si no recibe cabeceras CORS, bloquea el POST.
- **No elimines el header `Origin`**: el backend lo usa para decidir si pone `Access-Control-Allow-Origin`.

Ejemplo mínimo para `api.tukifac.cloud`:

```nginx
server {
    listen 443 ssl;
    server_name api.tukifac.cloud;
    # ... ssl_certificate, etc. ...

    location / {
        proxy_pass http://127.0.0.1:3000;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header Origin $http_origin;   # importante para CORS
    }
}
```

Si no reenvías OPTIONS o quitas `Origin`, el backend no podrá devolver CORS y verás "No 'Access-Control-Allow-Origin' header".

---

## 2. Panel central (app.tukifac.cloud)

- **URL pública:** `https://app.tukifac.cloud` (sin subdominio).
- **Las peticiones del panel central deben ir al backend:** `https://api.tukifac.cloud/api/...`, **no** a `https://app.tukifac.cloud/api/...`.

### Build (importante)

Hay que construir el frontend **con** la URL absoluta del backend. Si no, `VITE_API_URL` queda por defecto como `'/api'` (ruta relativa) y el navegador enviará las peticiones al mismo dominio del frontend (`app.tukifac.cloud/api/...`), donde no está el backend.

```bash
# En la máquina donde haces el build del panel central:
cd central-frontend-react
echo "VITE_API_URL=https://api.tukifac.cloud/api" > .env.production
npm run build
```

O en tu CI/CD / servidor, asegúrate de que la variable esté definida **antes** de `npm run build`:

```env
VITE_API_URL=https://api.tukifac.cloud/api
```

- **Despliegue:** sube la carpeta `dist/` a `https://app.tukifac.cloud` (mismo dominio que `APP_DOMAIN`).

---

## 3. Panel tenant (+ restaurante)

- **URL pública:** misma app desplegada en el mismo dominio, con **subdominios dinámicos**:
  - `https://tenant1.app.tukifac.cloud`
  - `https://demo.app.tukifac.cloud`
- **Build:** la misma app debe apuntar a la API. Si no, en producción las peticiones irán a `http://localhost:3000` y verás `ERR_CONNECTION_REFUSED` en el login.

  Crea `tenant-frontend-react/.env.production` (o define la variable antes del build):

```env
VITE_API_URL=https://api.tukifac.cloud
```

  Luego construye y despliega la misma carpeta `dist/` en el servidor que atiende `*.app.tukifac.cloud`:

```bash
cd tenant-frontend-react
# Asegúrate de tener .env.production con VITE_API_URL=https://api.tukifac.cloud
npm run build
# Despliega dist/ en el servidor de *.app.tukifac.cloud
```

- **Detección del tenant:** en el código ya está:
  - `window.location.hostname` → `demo.app.tukifac.cloud` → slug `demo`.
  - Se envía en el header `X-Tenant-Slug` en cada request a la API.

### DNS / proxy

- Wildcard para el frontend de tenants: `*.app.tukifac.cloud` → mismo servidor (o CDN) que sirve la SPA.
- Ejemplo Nginx:

```nginx
server_name app.tukifac.cloud *.app.tukifac.cloud;
root /var/www/tenant-frontend;
try_files $uri $uri/ /index.html;
```

---

## 4. Resumen de flujo

| Origen (navegador)              | Request a API           | Header X-Tenant-Slug |
|---------------------------------|-------------------------|----------------------|
| https://app.tukifac.cloud        | https://api.tukifac.cloud/... | (vacío; contexto central/superadmin) |
| https://tenant1.app.tukifac.cloud| https://api.tukifac.cloud/... | tenant1              |

El backend usa `X-Tenant-Slug` para elegir la base de datos del tenant. En producción no usa el Host para el slug porque el Host es siempre `api.tukifac.cloud`.

---

## 5. Checklist producción

- [ ] Migraciones SaaS: [MIGRATIONS-SaaS.md](./MIGRATIONS-SaaS.md) — `migrate-init-versions`, cron `migrate-fleet`, panel Fleet Migrations.
- [ ] Backend: `APP_ENV=production`, `APP_DOMAIN=app.tukifac.cloud`, CORS sin localhost en producción (ya manejado por `allowedOrigin`).
- [ ] Panel central: build con `VITE_API_URL=https://api.tukifac.cloud/api`, desplegado en `https://app.tukifac.cloud`.
- [ ] Panel tenant: build con `VITE_API_URL=https://api.tukifac.cloud` (archivo `tenant-frontend-react/.env.production`), desplegado en el mismo servidor que `app.tukifac.cloud` con wildcard `*.app.tukifac.cloud`. **Si no se define la variable, el login intentará usar localhost y fallará en producción.**
- [ ] HTTPS en api.tukifac.cloud y app.tukifac.cloud.
- [ ] JWT secrets y BD con valores seguros.
