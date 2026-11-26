# Deploy — Tukifac Backend

Archivos de despliegue en el VPS (`/opt/tukifac/`):

| Archivo / carpeta | Origen en repo |
|-------------------|----------------|
| `docker-compose.production.yml` | `backend_go/docker-compose.production.yml` |
| `.env` | copiar de `.env.production.example` (no commitear) |
| `deploy/scripts/*` | esta carpeta |
| `data/uploads`, `data/storage` | creados por `vps-init-dirs.sh` |

## Comandos rápidos (en el VPS)

```bash
cd /opt/tukifac

# Deploy completo (migrate-central → migrate-fleet → restart → health; sin migrate en entrypoint)
bash deploy/scripts/deploy.sh

# Primera vez con Migration v2 (baseline V30 + target V31)
bash deploy/scripts/migrate-init.sh

# Fleet de tenants (manual o cron nocturno)
bash deploy/scripts/migrate-fleet.sh   # flock + migrate-fleet-cron (lock Redis/DB)

# Solo BD central
bash deploy/scripts/migrate.sh

bash deploy/scripts/health-check.sh
bash deploy/scripts/rollback.sh
```

## Migraciones SaaS (importante)

**El deploy migra la BD central y el esquema de todos los tenants ANTES del restart**, con la imagen nueva. Así el código nuevo nunca corre contra un tenant sin migrar.

El cron `migrate-fleet-cron` sigue activo: cubre tenants nuevos o fallidos y corre los backfills.

- **Schema DDL:** `tenantmigrations` registry → `target_version` = `MaxVersion()` del binario.
- **Backfills datos:** registry aparte (`tenantbackfills`, hoy V31/V32). El cron itera solo backfills registrados, **no** el schema target.

Documentación completa: **[docs/MIGRATIONS-SaaS.md](../docs/MIGRATIONS-SaaS.md)**

### Cron recomendado

Una vez cada noche (08:00 UTC = 3:00 a. m. de Perú). Cada corrida revisa a fondo el esquema de 100 tenants y ejecuta hasta 100
ejecuciones por backfill pendiente. El deploy **no** corre backfills: si una versión trae uno nuevo, lanzarlo a mano tras el
deploy (cambiando `31` por la versión del backfill) o tardará varias noches en llegar a todos:

```bash
docker exec tukifac-backend-go ./tukifac-api migrate-backfill-fleet --version=31 --limit=100000 --active-only=false
```

No subir `MIGRATE_LIMIT` para esto: también amplía la revisión de esquema, que es secuencial y no entra en el tiempo máximo.

```bash
chmod +x /opt/tukifac/deploy/scripts/migrate-fleet.sh
mkdir -p /var/log/tukifac
```

```cron
0 8 * * * /opt/tukifac/deploy/scripts/migrate-fleet.sh >> /var/log/tukifac/cron-migrate.log 2>&1
```

### Panel operativo

Super Admin → **Fleet Migrations** (`/fleet-migrations`)

## CI/CD

GitHub Actions: `.github/workflows/deploy-production.yml`  
Imagen: `ghcr.io/<org>/<repo>:<sha>` y `:latest`

Documentación VPS: [docs/DEPLOY-VPS-UBUNTU.md](../docs/DEPLOY-VPS-UBUNTU.md)
