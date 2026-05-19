# Deploy — Tukifac Backend

Archivos de despliegue en el VPS (`/opt/tukifac/`):

| Archivo / carpeta | Origen en repo |
|-------------------|----------------|
| `docker-compose.production.yml` | raíz del repo |
| `.env` | copiar de `.env.production.example` (no commitear) |
| `deploy/scripts/*` | esta carpeta |
| `data/uploads`, `data/storage` | creados por `vps-init-dirs.sh` |

## Comandos rápidos (en el VPS)

```bash
cd /opt/tukifac
bash deploy/scripts/deploy.sh
bash deploy/scripts/migrate.sh
bash deploy/scripts/health-check.sh
bash deploy/scripts/rollback.sh
```

## CI/CD

GitHub Actions: `.github/workflows/deploy-production.yml`  
Imagen: `ghcr.io/<org>/<repo>:<sha>` y `:latest`

Documentación completa: [docs/DEPLOY-VPS-UBUNTU.md](../docs/DEPLOY-VPS-UBUNTU.md)
