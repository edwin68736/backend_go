# Panel central fiscal SaaS

Arquitectura **BFF** en `backend_go` + UI en `frontend_central`.  
**Source of truth:** `facturador_lycet` (`fiscal_documents`).  
**No hay mirror fiscal** en la BD central.

## Qué se reutilizó

| Componente | Reutilizado |
|------------|-------------|
| `FiscalController` (emit, list, detail, acciones unitarias, download) | ✅ Base existente |
| `FiscalDocumentRepository` | ✅ Extendido (filtros + cursor) |
| `FiscalDocumentDetailService` | ✅ Stats + timeline + detalle |
| `FiscalQueueService` | ✅ Colas emit/email/poll |
| Dashboard facturador (`fiscal_dashboard.html`) | ✅ Sin cambios (sigue operativo) |
| `fiscalclient.Emit` (ERP → facturador) | ✅ Sin cambios |
| Webhook `POST /api/internal/fiscal/status` | ✅ Sin cambios |
| Auth superadmin JWT (`/api/superadmin/*`) | ✅ |
| Auth tenant JWT + `ValidateTenantBinding` | ✅ |

## Endpoints nuevos

### facturador_lycet (`/api/v1/fiscal`)

| Método | Ruta | Descripción |
|--------|------|-------------|
| GET | `/documents?cursor=&limit=&filters...` | Listado con paginación cursor u offset |
| GET | `/stats?from=&to=&tenant_slug=&tenant_id=` | KPIs con rango de fechas |
| POST | `/documents/bulk/{send\|retry\|force\|email\|poll}` | Acciones masivas por UUIDs o filtros |

Filtros listado: `tenant_slug`, `tenant_id`, `company_ruc`, `document_type`, `status`, `provider`, `send_mode`, `series`, `number`, `customer_name`, `customer_email`, `from`, `to`, `errors_only`, `pending_only`, `retry_only`, `email_sent`, `email_pending`.  
Excluye tipos no fiscales `00`, `NV` por defecto (`electronic_only`).

### backend_go superadmin BFF (`/api/superadmin/fiscal`)

Proxy seguro al facturador (token server-side `FACTURADOR_TOKEN`):

- `GET /stats`
- `GET /documents`
- `GET /documents/:uuid`
- `GET /documents/:uuid/download/:type`
- `POST /documents/:uuid/{send|retry|force|email|poll}`
- `POST /documents/bulk/:action`

### backend_go tenant BFF (`/api/fiscal`)

Misma superficie, con **tenant_slug forzado desde JWT** y verificación de ownership en detalle/acciones/download/bulk.

## Paginación

- **Preferida:** cursor `(created_at DESC, id DESC)` → `next_cursor`, `has_more` (sin COUNT total).
- **Legacy:** `offset` + `limit` (max 200); `include_total=true` opcional (costoso en millones).
- UI central usa cursor 50/página con historial atrás/adelante.

## Performance (millones de registros)

- Índices migración `Version20260527000000`: `(created_at, id)`, `(tenant_id, status)`, `(tenant_slug, status, created_at)`.
- Listado **sin** timeline/snapshot completo (solo summary ligero).
- Detalle/timeline solo bajo demanda (`GET /documents/:uuid`).
- COUNT total desactivado por defecto en modo cursor.
- Filtro RUC vía `LIKE` en `snapshot_json` — usable con tenant/fecha; considerar columna `company_ruc` denormalizada si crece mucho.

## Seguridad tenant

| Rol | Alcance |
|-----|---------|
| Superadmin | Todos los tenants vía BFF |
| Tenant JWT | `tenant_slug` inyectado; query `tenant_slug`/`tenant_id` ignorados |
| Detalle/acción tenant | Verifica `document.tenant_slug === JWT` |
| Bulk tenant | `tenant_slug` obligatorio en body; facturador rechaza UUIDs de otro tenant |

## Bulk actions

- Por selección (`document_uuids[]`, max 500) o por `filters` + `max`.
- Acciones: `send`, `retry`, `force`, `email`, `poll` → encolan Redis (misma lógica que dashboard facturador).
- UI: bulk sobre selección o sobre filtros activos si no hay selección.

## Tenant panel vs central

| | Central (`frontend_central`) | Tenant (ERP API `/api/fiscal/*`) |
|--|------------------------------|----------------------------------|
| Tenants visibles | Todos | Solo JWT tenant |
| UI | `/fiscal` en panel admin | Consumir misma API desde frontend tenant (pendiente UI dedicada) |
| BD fiscal | Ninguna | Ninguna |

## Frontend central

- Ruta: `/fiscal`
- KPIs globales + filtros persistentes (`localStorage`)
- Tabla paginada, selección múltiple, modal detalle (timeline, snapshot, attempts, descargas)
- Servicio: `frontend_central/src/services/fiscal.service.ts`

## Configuración

```env
# backend_go
FISCAL_DECOUPLED=true
FACTURADOR_BASE_URL=https://facturador.example.com
FACTURADOR_TOKEN=<CLIENT_TOKEN facturador>
```

Migración facturador:

```bash
php bin/console doctrine:migrations:migrate
```

## Riesgos residuales

1. **Filtro RUC/cliente en JSON** — lento sin índice dedicado.
2. **Stats globales** — `countByStatus` agrega por status; con millones conviene cache Redis TTL corto (no implementado aún).
3. **Bulk >500** — requiere jobs batch dedicados (actualmente max 500/request).
4. **UI tenant ERP** — API lista; falta pantalla en frontend tenant si se desea paridad visual.
5. **Prev cursor pagination** — historial en cliente; no bi-direccional nativo en API.

## Monitoreo producción

- Latencia p95 `GET /api/v1/fiscal/documents` y `/stats`
- Profundidad colas Redis: `fiscal:emit`, `fiscal:email`, `fiscal:status_poll`
- Ratio `accepted` vs `rejected`/`error` por tenant (alertas)
- Errores BFF `502` hacia facturador
- Intentos bulk con `errors[]` > 0
- Migración índices aplicada en prod
- Logs tenant bleed: HTTP 403 `documento fuera del tenant`
