# Migraciones SaaS — Migration System v2

Guía operativa para producción multi-tenant (database-per-tenant). Basada en el código actual de `pkg/database`, `pkg/database/engine` y comandos CLI.

## Resumen

| Concepto | Valor / comportamiento |
|----------|------------------------|
| Baseline congelado | **V30** — estado del sistema antes del registry incremental |
| Versión objetivo actual | **V33** — pedidos restaurante + timestamps repartidores (`V033DeliveryDriversTimestamps`) |
| Registry central | Tabla `tenant_schema_versions` en BD `tukifac_saas` |
| Historial por tenant | Tabla `tenant_migration_history` |
| Deploy HTTP | **No** migra tenants; solo pool de conexiones |
| Deploy CI/VPS | `migrate-central` antes del restart; fleet = cron aparte |

---

## Arquitectura

```
Deploy (CI o deploy.sh)
  └─ ./tukifac-api migrate-central   → BD central (sin migrar en entrypoint del contenedor)

Cron cada 5 min (run-migrate-fleet.sh)
  ├─ migrate-bump-target            → target_version = V32 en central
  ├─ migrate-fleet --workers=4      → incremental hasta V32 (sin AutoMigrate masivo)
  └─ migrate-backfill-fleet         → datos run-once (V032RestaurantOrdersBackfill, etc.)

Panel Super Admin → /fleet-migrations
  └─ Retry / Migrate / Pause / Resume por tenant
```

**Importante:** `migrate-fleet` ejecuta solo migraciones registradas en `pkg/database/tenantmigrations/` (hasta `V032RestaurantOrders`). **No** ejecuta `AutoMigrate` de todos los modelos en cada tenant.

Los **tenants nuevos** (alta desde panel) siguen usando `MigrateTenantSchema` (bootstrap con `AutoMigrate` + seeds) una sola vez.

---

## Comandos CLI

Ejecutar dentro del contenedor o con binario local (misma `.env`):

```bash
docker exec tukifac-backend-go ./tukifac-api <comando>
# o en deploy previo al restart:
docker compose -f docker-compose.production.yml run --rm --no-deps backend-go ./tukifac-api <comando>
```

| Comando | Uso en producción |
|---------|-------------------|
| `migrate-central` | **Deploy:** solo BD central (obligatorio antes del restart) |
| `migrate` | Alias de `migrate-central` + aviso fleet |
| `migrate-init-versions` | **Una vez** por entorno: registra todos los tenants en V30 |
| `migrate-bump-target` | Tras deploy con nueva versión de código: sube `target_version` |
| `migrate-fleet --workers=4 --limit=100` | Migración incremental de tenants pendientes |
| `migrate-backfill-fleet --workers=4 --limit=100` | Backfills de datos run-once |
| `migrate-tenant <slug>` | **Emergencia:** bootstrap AutoMigrate de un tenant |
| `migrate-tenants` | **Bloqueado en producción** (`APP_ENV=production`) |
| `migrate-backfill-branch` | Alias backfill V31 |

### Flags útiles

```bash
migrate-fleet --workers=4 --limit=100 --active-only=true
migrate-backfill-fleet --version=31 --tenant=mi-empresa
```

### Variables de entorno (lotes y alertas)

```env
MIGRATION_BATCH_SIZE=50
MIGRATION_BATCH_PAUSE=2s

# Alertas (opcional)
MIGRATION_ALERT_WEBHOOK=https://hooks.slack.com/...
MIGRATION_ALERT_EMAIL=admin@tu-dominio.com
SMTP_HOST=smtp.tu-dominio.com
SMTP_PORT=587
SMTP_USER=...
SMTP_PASSWORD=...
SMTP_FROM=noreply@tukifac.com

# Health interno
INTERNAL_API_KEY=clave-larga-secreta
FLEET_FAILED_THRESHOLD=25

# Omitir backfill en bootstrap de tenant (solo casos especiales)
SKIP_BRANCH_BACKFILL=1
```

**Nunca en producción:**

```env
AUTO_MIGRATE_DEV=true   # migra todo al arrancar — solo desarrollo local
```

---

## Primera puesta en producción (checklist)

Orden recomendado tras desplegar el binario con Migration v2:

1. **Migrar central**
   ```bash
   cd /opt/tukifac
   docker compose -f docker-compose.production.yml run --rm --no-deps backend-go ./tukifac-api migrate-central
   ```

2. **Reiniciar API** (si no lo hizo el deploy)
   ```bash
   docker compose -f docker-compose.production.yml up -d --no-deps --force-recreate backend-go
   ```

3. **Bootstrap registry V30** (idempotente, una vez)
   ```bash
   docker exec tukifac-backend-go ./tukifac-api migrate-init-versions
   ```

4. **Subir target a V31**
   ```bash
   docker exec tukifac-backend-go ./tukifac-api migrate-bump-target
   ```

5. **Activar cron fleet** (ver sección Cron)

6. **Verificar en panel central** → menú **Fleet Migrations** (`/fleet-migrations`)

7. **Health interno** (monitoring)
   ```bash
   curl -s -H "X-Internal-Key: $INTERNAL_API_KEY" http://127.0.0.1:3000/api/internal/fleet-health
   ```

8. **Smoke test:** login tenant, POS/caja, cambio de sucursal (admin).

---

## Deploy en VPS

### Flujo recomendado (sin migrate-all)

```bash
cd /opt/tukifac
bash deploy/scripts/deploy.sh          # pull → migrate-central → restart → health
bash deploy/scripts/migrate-init.sh    # solo la primera vez
bash deploy/scripts/migrate-fleet.sh   # manual o vía cron
```

El script `deploy.sh` actualizado:

1. `docker compose pull`
2. `docker compose run --rm backend-go ./tukifac-api migrate-central` (**antes** del restart, imagen nueva)
3. `docker compose up -d --force-recreate backend-go`
4. Health check

**No** ejecuta `migrate-fleet` en el deploy (puede tardar horas con miles de tenants).

### CI/CD (GitHub Actions)

El workflow `.github/workflows/deploy-production.yml`: `pull` → `migrate-central` → `restart` → health. El entrypoint del contenedor **no** migra.

---

## Cron en VPS (self-healing)

Script: `deploy/scripts/migrate-fleet.sh` (wrapper Docker) o `backend_go/scripts/run-migrate-fleet.sh` (binario directo).

### Instalación

```bash
chmod +x /opt/tukifac/deploy/scripts/migrate-fleet.sh
mkdir -p /var/log/tukifac
```

### Crontab (cada 5 minutos)

```cron
*/5 * * * * /opt/tukifac/deploy/scripts/migrate-fleet.sh >> /var/log/tukifac/cron-migrate.log 2>&1
```

### Lock de ejecución (sin procesos concurrentes)

| Capa | Mecanismo |
|------|-----------|
| Host (mismo VPS) | `flock` en `migrate-fleet.sh` — si hay otra instancia del script, sale **0** sin log |
| API (multi-nodo) | `migrate-fleet-cron` → Redis `SETNX` (`tukifac:cronlock:migrate-fleet`) si `REDIS_URL` está configurado |
| Fallback | MySQL `GET_LOCK('tukifac_fleet_migrate')` en BD central (conexión dedicada hasta `release`) |

Si el lock global no se adquiere, `migrate-fleet-cron` termina con código **0** (silencioso) para no alertar el cron.

Lease del lock: `MIGRATE_TIMEOUT_SEC` o `FLEET_LOCK_LEASE_SEC` (default 3600s). El script usa `timeout` con el mismo valor.

### Qué hace cada ciclo (`migrate-fleet-cron`)

1. `migrate-bump-target` — alinea `target_version` con el código desplegado (V31).
2. `migrate-fleet` — hasta 100 tenants pendientes, 4 workers en paralelo.
3. `migrate-backfill-fleet` — backfills run-once pendientes.

Locks por tenant (`migration_lock` + `lock_expires_at`) se liberan automáticamente si expiraron antes del fleet.

### Backoff exponencial (por tenant)

| Intento | Espera antes de reintento |
|---------|---------------------------|
| 1 | 1 min |
| 2 | 5 min |
| 3 | 15 min |
| 4+ | 1 h |

Campo `next_retry_at` en `tenant_schema_versions`. Reintento manual desde panel limpia backoff.

### Circuit breaker (fleet global)

- `FLEET_CIRCUIT_BREAKER_THRESHOLD=10` fallos **consecutivos** en un ciclo → pausa fleet (`fleet_migration_state`).
- Alerta webhook/email + banner en panel central.
- Reanudar: `POST /api/superadmin/migrations/resume-fleet` o `./tukifac-api migrate-fleet-resume`.

### Health

```bash
curl -s -H "X-Internal-Key: $INTERNAL_API_KEY" https://api.tudominio.com/fleet-health
```

Respuesta: `pending`, `running`, `failed`, `blocked`, `current_target`, `circuit_open`.

---

## Panel Super Admin — Fleet Migrations

Ruta frontend: `https://app.tukifac.cloud/fleet-migrations`

API:

| Método | Ruta |
|--------|------|
| GET | `/api/superadmin/migrations` |
| GET | `/api/superadmin/migrations/summary` |
| POST | `/api/superadmin/migrations/:tenantId/retry` |
| POST | `/api/superadmin/migrations/:tenantId/migrate` |
| POST | `/api/superadmin/migrations/:tenantId/pause` |
| POST | `/api/superadmin/migrations/:tenantId/resume` |

Estados: `completed`, `pending`, `running`, `failed`, `paused`.

**Deshabilitado en producción:**

- `POST /api/superadmin/tenants/migrate-all`
- CLI `migrate-tenants`

---

## Multi-sucursal y compatibilidad

- Tenants en **V30** y **V31** pueden convivir semanas.
- Login, middleware y POS usan **legacy mode** (`HasColumn`) si faltan columnas.
- `GET /api/session/capabilities` expone `features.multi_branch` para frontends.
- V31 DDL es idempotente (`HasColumn` antes de `AddColumn`).

No ejecutar `migrate-tenants` en producción: aplica AutoMigrate completo por tenant (lento, no escalable).

---

## Escalado (5000+ tenants)

| Parámetro | Recomendación |
|-----------|----------------|
| `migrate-fleet --limit` | 100 por ciclo de cron |
| `MIGRATION_BATCH_PAUSE` | 2s cada 50 tenants |
| Workers | 4 (ajustar según CPU MySQL) |
| Tiempo | Fleet completo en background (horas OK); deploy sigue siendo minutos |

Throughput orientativo: ~2000–4000 tenants/hora (depende de DDL y tamaño de BD).

---

## Observabilidad

| Recurso | URL / comando |
|---------|----------------|
| Logs estructurados | `tenant_migration_success`, `fleet_tenant_*` en logs del contenedor |
| Prometheus texto | `GET /metrics` — `tukifac_migration_*`, `tukifac_fleet_*` |
| Fleet health | `GET /api/internal/fleet-health` + header `X-Internal-Key` |
| Dashboard | Panel central → Fleet Migrations |

---

## Rollback de aplicación

Si un deploy falla:

```bash
cd /opt/tukifac
bash deploy/scripts/rollback.sh
```

Las columnas ya añadidas en MySQL **no** se eliminan automáticamente. El binario anterior debe seguir siendo compatible (legacy mode) o permanecer en la misma versión de esquema.

---

## Referencias en código

| Ruta | Contenido |
|------|-----------|
| `pkg/database/schema_version.go` | V30, V31 |
| `pkg/database/tenant_schema_registry.go` | Central registry, locks |
| `pkg/database/tenantmigrations/` | `V031MultiBranch` |
| `pkg/database/tenantbackfills/` | `V031BranchBackfill` |
| `pkg/database/engine/` | Fleet runner |
| `pkg/database/schema_features.go` | `SchemaAtLeast`, capabilities |
| `internal/superadmin/service/migration_fleet_service.go` | Dashboard API |

Documentos relacionados: [PRODUCTION-HARDENING.md](./PRODUCTION-HARDENING.md), [DEPLOY-VPS-UBUNTU.md](./DEPLOY-VPS-UBUNTU.md), [deploy/README.md](../deploy/README.md).
