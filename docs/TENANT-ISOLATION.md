# Aislamiento multi-tenant — Web + App móvil/desktop

## Flujos soportados

### Web

```
https://empresa1.tukifac.com → subdominio = tenant
```

- `X-Tenant-Slug` **ignorado** para resolver (solo validación si se envía).
- Header ≠ subdominio → **403** `TENANT_ISOLATION_VIOLATION`.

### Tukichef (Android / Tauri)

```
1. RUC → GET api.tukifac.com/api/public/tenant-by-ruc
2. Respuesta: { slug, api_url: "https://empresa1.tukifac.com", subdomain, tenant_version }
3. Guardar api_url en localStorage (tenantApiUrl)
4. Login y API → https://empresa1.tukifac.com/api/*
5. Header X-Tenant-Slug: empresa1 (redundancia; debe coincidir con host)
```

### Dev localhost

```
X-Tenant-Slug o cookie dev_tenant
```

## Validación backend (cadena)

| Capa | Qué valida |
|------|------------|
| TenantResolver | Host → slug; mismatch header en prod |
| TenantAuthAPI | JWT con tenant_id, tenant_slug, tenant_db, tenant_version ≥ 1 |
| ValidateTenantBinding | host slug = JWT slug = tenant DB = tenant_id |

## JWT

Tokens nuevos incluyen:

```json
{
  "tenant_id": 45,
  "tenant_slug": "empresa1",
  "tenant_db": "saas_tenant_empresa1",
  "tenant_version": 1
}
```

Tokens sin `tenant_id` o `tenant_version` (prod) → **401** `TOKEN_TENANT_INVALID`.

## Redis

Prefijo: `tukifac:tenant:{slug}:*`

Invalidación: `InvalidateTenantCache(slug)` tras cambios de plan/permisos/suspensión.

## Checklist post-deploy

1. **Forced relogin** — usuarios con tokens viejos deben volver a login.
2. **Purge Redis selectiva** — `SCAN tukifac:tenant:{slug}:*` si hubo incidente.
3. **Monitoreo** — alertas en logs `tenant_security_violation`.
4. **Postman** — JWT tenant A + Host empresa2 → 403.
5. **App** — verificar que peticiones van a `https://{slug}.tukifac.com`, no solo a `api.tukifac.com`.
6. **Nginx** — wildcard `*.tukifac.com` apunta al backend Go.

## Tests

```bash
go test ./pkg/middleware/... -count=1
# Linux CI:
go test -race ./pkg/middleware/...
```

## Riesgos residuales

- `/uploads/*` sin auth (archivos por URL).
- Login legacy en `api.tukifac.com` + header (deprecado; log `tenant_resolve_central_host_header`).
