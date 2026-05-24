# Backend principal — Tukifac API (Go / Fiber)

API REST multi-tenant del ERP SaaS. Módulo Go: **`tukifac`**.

## Responsabilidades

- Autenticación JWT (super admin y tenant)
- Resolución de tenant y pool de conexiones MySQL por empresa
- Módulos de negocio: ventas, inventario, facturación, restaurante, membresías, caja, etc.
- Orquestación de facturación electrónica (facturador Lycet, Tukifac legacy, PSE)
- Panel central: CRUD tenants, planes, suscripciones, sync SUNAT/PSE
- Proxy opcional a módulos externos (`/api/modules/:key/forward/*`)

## Arquitectura de capas

```
routes/routes.go
    → middleware (TenantResolver, TenantAuthAPI, RequireModule, RequirePermission)
    → internal/<módulo>/routes.go
        → handler/     # Fiber, JSON, lectura de Locals
        → service/     # Lógica de negocio + GORM directo
```

**No existe capa `repository/`**. Los modelos GORM están centralizados en `pkg/database/migrations.go`.

## Estructura

```
backend_principal/
├── main.go
├── config/config.go          # Carga .env
├── routes/routes.go          # Router global, CORS, grupos público/protegido
├── internal/<módulo>/
│   ├── handler/
│   ├── service/
│   └── routes.go
├── pkg/
│   ├── database/             # CentralDB, GetTenantDB, migraciones, seeds
│   ├── middleware/           # tenant, auth, permissions
│   ├── facturador/           # Cliente HTTP → api_facturador
│   ├── tax/                  # IGV y afectación SUNAT
│   ├── tenantstorage/        # Uploads por RUC
│   ├── cron/                 # Vencimiento suscripciones (24h)
│   └── response/
├── .env.example
└── dockerfile
```

## Multi-tenancy

| BD | Nombre | Contenido |
|----|--------|-----------|
| Central | `tukifac_saas` | `Tenant`, planes, `TenantModule`, super admins |
| Tenant | `saas_tenant_{slug}` | Prefijo `Tenant*` en modelos |

`GetTenantDB` usa `TenantDBManager` (pool con singleflight + eviction). **No** ejecuta migraciones en cada request.

Migraciones SaaS (V30 baseline, fleet incremental): ver **[docs/MIGRATIONS-SaaS.md](docs/MIGRATIONS-SaaS.md)**.

### Resolver tenant

1. Header `X-Tenant-Slug`
2. Subdominio (`utils.ExtractSubdomain` + `APP_DOMAIN`)
3. Cookie `dev_tenant` (solo `APP_ENV != production`)

## Autenticación

### Super Admin

- `POST /api/superadmin/login` — público
- Resto bajo `/api/superadmin/*` con `SuperAdminAuthAPI` (Bearer, `SA_JWT_SECRET`, `type: superadmin`)

### Tenant

- `POST /api/login` — requiere `TenantResolver` previo (slug en header/subdominio/cookie)
- Grupo `/api/*` con `TenantAuthAPI` (Bearer o cookie `token`, `type: tenant`, `status: active` en JWT)

JWT incluye: `modules`, `permissions`, `restaurant_role`, `tenant_slug`, `tenant_db`.

**Importante:** `TenantAuthAPI` no valida que `claims.TenantSlug` coincida con el tenant resuelto por `TenantResolver`. Evitar mezclar token de un tenant con slug de otro.

### Middleware adicionales

- `RequireModule("billing")` — módulos en JWT
- `RequirePermission("sales.view")` — permisos en JWT
- `RequireRestaurantRole` / `RequireRestaurantAdminOrTenantAdmin`

## Endpoints principales (tenant autenticado)

Prefijo: `/api` + Bearer + contexto tenant.

| Área | Rutas ejemplo |
|------|----------------|
| Empresa | `GET/PUT /company/config`, `/company/sunat`, series, sucursales |
| Ventas | `GET/POST /sales`, `POST /sales/:id/issue-electronic` |
| Facturación | `POST /billing/send/:saleId`, resúmenes, voided, guías, retención |
| Restaurante | `/restaurant/*` (mesas, comandas, cocina, mozos) |
| Inventario | stock, movimientos, transferencias, ajustes |
| Caja | sesiones, movimientos, arqueo, métodos de pago |

Listado completo Super Admin: `internal/superadmin/routes.go`.

## Integración facturador

Variables: `FACTURADOR_BASE_URL`, `FACTURADOR_TOKEN` (mismo valor que `CLIENT_TOKEN` en PHP).

Cliente: `pkg/facturador/client.go` — timeout 45s, token en query `?token=`.

Endpoints usados: `/empresas`, `/invoice/send`, `/note/*`, `/voided/*`, `/summary/*`, `/despatch/*`, etc.

Modos en `TenantCompanyConfig.InvoicingMode`:

- `legacy_backend` — facturador HTTP o Tukifac
- `pse` — UBL + adaptador PSE (ValidaPSE u otro)

Almacenamiento local comprobantes: `INVOICE_STORAGE_PATH/tenants/{RUC}/...`

## Variables de entorno

Copiar `.env.example`. Mínimo:

```env
DB_HOST=127.0.0.1
DB_PORT=3306
DB_USER=root
DB_PASSWORD=
CENTRAL_DB_NAME=tukifac_saas
JWT_SECRET=...
SA_JWT_SECRET=...
PORT=3000
APP_ENV=development
APP_DOMAIN=localhost
FACTURADOR_BASE_URL=http://localhost:8000
FACTURADOR_TOKEN=...
FRONTEND_URL=http://localhost:5173
CENTRAL_FRONTEND_URL=http://localhost:5174
```

## Ejecución

```bash
go mod download
go run .
# http://localhost:3000
# Health: GET /health
# Fleet health: GET /api/internal/fleet-health (opcional X-Internal-Key)
```

### CLI migraciones (resumen)

```bash
./tukifac-api migrate-central
./tukifac-api migrate-init-versions
./tukifac-api migrate-fleet --workers=4 --limit=100
```

Ver [docs/MIGRATIONS-SaaS.md](docs/MIGRATIONS-SaaS.md).

## Convenciones

- Handlers: helper `db(c)` → `c.Locals("tenantDB")`
- Respuestas listas: `make([]T, 0)` para serializar `[]` no `null`
- Rutas públicas nuevas: lista en `pkg/middleware/auth.go` → `publicPaths`
- Nuevo modelo tenant: struct en `migrations.go` + slice en `MigrateTenant()`

## Docker

```bash
docker build -f dockerfile -t tukifac-api .
# EXPOSE 3000
```

## Deuda técnica conocida (código)

- `migrations.go` monolítico (~1300+ líneas)
- `BillingService` muy grande; múltiples adaptadores de facturación
- Código SSR legacy (`TenantAuthWeb`, handlers con `c.Render`) sin rutas activas
- Fleet de miles de tenants requiere cron `migrate-fleet` (no migrate-all en deploy)
- `go.mod` con dependencias marcadas `// indirect` sin tidying

## Extender un módulo

1. Crear `internal/mimodulo/{handler,service,routes.go}`
2. Registrar en `routes/routes.go` dentro del grupo `tenantAPI`
3. Si requiere plan: `middleware.RequireModule("clave")` en rutas
4. Modelos en `pkg/database/migrations.go` si persisten en tenant DB

esto es otro prueba


go run . migrate-fleet --workers=4 --limit=100

