# Deploy producción — Tukifac Backend (VPS Ubuntu + Docker + GHCR)

Guía completa para desplegar el backend Go multi-tenant en producción real.

## Arquitectura

| Componente | Ubicación | Notas |
|------------|-----------|--------|
| API Go | VPS Backend — Docker `tukifac-backend-go` | Puerto **solo** `127.0.0.1:3000` |
| Nginx Proxy Manager | Mismo VPS (Docker aparte) | `api.tudominio.com` → `127.0.0.1:3000` |
| MySQL | VPS Database (nativo) | Remoto, firewall por IP |
| Facturador Lycet | VPS dedicado | `FACTURADOR_BASE_URL` en `.env` |
| Código fuente | GitHub | El VPS **no** clona el repo; solo imagen GHCR |

## Decisiones técnicas (proyecto real)

| Tema | Decisión |
|------|----------|
| Migraciones | **Migration v2** — ver [MIGRATIONS-SaaS.md](./MIGRATIONS-SaaS.md): deploy = central; fleet = cron |
| Persistencia | Volúmenes `data/uploads` y `data/storage` |
| Imagen | GHCR con tag **`sha`** (rollback) + `latest` |
| Downtime | ~2–5 s al recrear contenedor (un solo réplica) |
| Deploy | `pull` → `migrate-central` → `up --force-recreate` → `health` |
| Puerto público | **No** exponer 3000; solo NPM |

---

## A. Instalación VPS backend

### A.1 Requisitos

- Ubuntu 22.04 / 24.04 LTS
- Acceso SSH con usuario sudo
- Firewall (UFW) activo

### A.2 Instalar Docker

```bash
sudo apt-get update
sudo apt-get install -y ca-certificates curl
sudo install -m 0755 -d /etc/apt/keyrings
curl -fsSL https://download.docker.com/linux/ubuntu/gpg | sudo gpg --dearmor -o /etc/apt/keyrings/docker.gpg
sudo chmod a+r /etc/apt/keyrings/docker.gpg
echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/ubuntu $(. /etc/os-release && echo "$VERSION_CODENAME") stable" | sudo tee /etc/apt/sources.list.d/docker.list > /dev/null
sudo apt-get update
sudo apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
sudo usermod -aG docker "$USER"
newgrp docker
docker --version && docker compose version
```

### A.3 Estructura de carpetas

```bash
sudo mkdir -p /opt/tukifac
sudo chown -R "$USER:$USER" /opt/tukifac
cd /opt/tukifac
```

Copiar al VPS (desde tu PC o CI):

- `docker-compose.production.yml` → `/opt/tukifac/`
- Carpeta `deploy/scripts/` → `/opt/tukifac/deploy/scripts/`
- `.env.production.example` → renombrar a `.env` y completar

Inicializar datos persistentes:

```bash
bash deploy/scripts/vps-init-dirs.sh
chmod +x deploy/scripts/*.sh
```

Estructura final:

```text
/opt/tukifac/
├── docker-compose.production.yml
├── .env
├── .deploy/
│   ├── current-image
│   └── previous-image
├── deploy/scripts/
│   ├── deploy.sh
│   ├── migrate.sh
│   ├── migrate-init.sh
│   ├── migrate-fleet.sh
│   ├── rollback.sh
│   └── health-check.sh
└── data/
    ├── uploads/          → /app/uploads
    └── storage/
        └── invoices/     → /app/storage/invoices
```

### A.4 Login GHCR (paquete privado)

```bash
echo "TU_PAT_READ_PACKAGES" | docker login ghcr.io -u TU_USUARIO_GITHUB --password-stdin
```

Crear PAT en GitHub: Settings → Developer settings → PAT → `read:packages`.

---

## B. Nginx Proxy Manager (Docker aparte)

Instalación típica (en el mismo VPS o stack separado):

```bash
mkdir -p ~/nginx-proxy-manager
cd ~/nginx-proxy-manager
```

Crear `docker-compose.yml` oficial de NPM (desde [nginx-proxy-manager](https://nginxproxymanager.com/setup/)) o su stack habitual.

Puertos NPM: `80`, `443`, panel admin `81` — abrir en firewall solo lo necesario.

**No** instalar NPM en el mismo `docker-compose` del backend; mantener stacks separados.

---

## C. Configuración Proxy Host

En el panel NPM (puerto 81):

| Campo | Valor |
|-------|--------|
| Domain Names | `api.tudominio.com` |
| Scheme | `http` |
| Forward Hostname / IP | `127.0.0.1` |
| Forward Port | `3000` |
| Websockets Support | Activado (restaurante / tiempo real si aplica) |
| Block Common Exploits | Activado |
| Client Max Body Size | `12m` o superior |

**Custom locations / Advanced** — asegurar cabeceras de proxy (NPM suele enviarlas por defecto):

- `X-Forwarded-For`
- `X-Forwarded-Proto`
- `Host`

El backend usa `TrustProxy` para IP real y HTTPS.

---

## D. SSL (Let's Encrypt)

En NPM → pestaña **SSL**:

1. SSL Certificate: **Request a new SSL Certificate**
2. Force SSL: activado
3. HTTP/2: activado
4. Email para Let's Encrypt válido

El DNS de `api.tudominio.com` debe apuntar a la IP del VPS **antes** de solicitar el certificado.

---

## E. Variables `.env`

```bash
cd /opt/tukifac
cp .env.production.example .env   # primera vez
nano .env
```

Mínimo obligatorio:

```env
TUKIFAC_IMAGE=ghcr.io/TU_ORG/backend_principal:latest
APP_ENV=production
APP_DOMAIN=app.tudominio.com
PORT=3000

DB_HOST=IP_VPS_MYSQL
DB_PORT=3306
DB_USER=tukifac_app
DB_PASSWORD=...
CENTRAL_DB_NAME=tukifac_saas

JWT_SECRET=...      # openssl rand -hex 32
SA_JWT_SECRET=...

INVOICE_STORAGE_PATH=/app/storage/invoices
FACTURADOR_BASE_URL=https://facturador.tudominio.com
FACTURADOR_TOKEN=...

FRONTEND_URL=https://app.tudominio.com
CENTRAL_FRONTEND_URL=https://app.tudominio.com
```

**Producción:** preferir imagen fijada por commit:

```env
TUKIFAC_IMAGE=ghcr.io/TU_ORG/backend_principal:abc123def456...
```

`PORT` debe ser **3000** (mapeo compose `127.0.0.1:3000:3000`).

---

## F. Primer deploy

### F.1 MySQL (VPS Database)

```sql
CREATE DATABASE IF NOT EXISTS tukifac_saas CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
CREATE USER 'tukifac_app'@'IP_VPS_BACKEND' IDENTIFIED BY 'password_fuerte';
GRANT ALL PRIVILEGES ON tukifac_saas.* TO 'tukifac_app'@'IP_VPS_BACKEND';
GRANT ALL PRIVILEGES ON `saas_tenant_%`.* TO 'tukifac_app'@'IP_VPS_BACKEND';
GRANT CREATE ON *.* TO 'tukifac_app'@'IP_VPS_BACKEND';
FLUSH PRIVILEGES;
```

Firewall MySQL: solo IP del VPS backend en puerto 3306.

### F.2 Levantar backend + migrar

```bash
cd /opt/tukifac
docker compose -f docker-compose.production.yml pull
docker compose -f docker-compose.production.yml up -d
docker exec tukifac-backend-go ./tukifac-api migrate
bash deploy/scripts/health-check.sh
```

O con script unificado:

```bash
bash deploy/scripts/deploy.sh
```

### F.3 Verificar

```bash
curl -s http://127.0.0.1:3000/health | jq .
curl -s https://api.tudominio.com/health | jq .
```

---

## G. Actualización backend (deploy normal)

### Flujo recomendado (manual)

```bash
cd /opt/tukifac
docker compose -f docker-compose.production.yml pull backend-go
docker compose -f docker-compose.production.yml up -d --no-deps --force-recreate backend-go
docker exec tukifac-backend-go ./tukifac-api migrate
curl -s http://127.0.0.1:3000/health | jq .
```

### Script (recomendado)

```bash
cd /opt/tukifac
bash deploy/scripts/deploy.sh
```

Con imagen explícita (como CI):

```bash
TUKIFAC_IMAGE=ghcr.io/TU_ORG/backend_principal:SHA bash deploy/scripts/deploy.sh
```

Sin migraciones (solo reinicio, sin cambios de esquema):

```bash
SKIP_MIGRATE=1 bash deploy/scripts/deploy.sh
```

### Estrategia downtime

Con **una réplica**, `force-recreate` implica ~2–5 s sin servicio. Los volúmenes **no** se pierden.

Orden óptimo (implementado en `deploy/scripts/deploy.sh`):

1. `pull` (sin detener)
2. `migrate-central` — BD central (`docker compose run ... migrate-central`)
3. `up --force-recreate` (downtime breve)
4. `health`
5. Fleet tenants en background: `deploy/scripts/migrate-fleet.sh` (cron)

Para cambios de esquema breaking: ventana de mantenimiento o deploy en horario valle.

### GitHub Actions (automático)

Push a `main` → `.github/workflows/deploy-production.yml`:

1. Build + push GHCR (`latest` + `sha`)
2. SSH al VPS
3. `deploy/scripts/deploy.sh` con `TUKIFAC_IMAGE=ghcr.io/...:sha`
4. Health check

**Secrets en GitHub:**

| Secret | Descripción |
|--------|-------------|
| `VPS_HOST` | IP o hostname del VPS |
| `VPS_USER` | Usuario SSH (ej. `deploy`) |
| `VPS_SSH_KEY` | Clave privada SSH |
| `VPS_GHCR_TOKEN` | Opcional: PAT `read:packages` si GHCR es privado |

---

## H. Cómo migrar

**Guía completa:** [MIGRATIONS-SaaS.md](./MIGRATIONS-SaaS.md)

| Comando | Cuándo |
|---------|--------|
| `./tukifac-api migrate-central` | Deploy — BD central (antes del restart) |
| `./tukifac-api migrate-init-versions` | Una vez — registry baseline V30 |
| `./tukifac-api migrate-bump-target` | Tras release con nueva versión de esquema |
| `./tukifac-api migrate-fleet` | Cron — tenants pendientes (V30→V31, etc.) |
| `./tukifac-api migrate-backfill-fleet` | Cron — backfills run-once |
| `./tukifac-api migrate-tenant slug` | Emergencia bootstrap un tenant |
| `./tukifac-api migrate-tenants` | **Bloqueado en producción** |

En Docker:

```bash
bash deploy/scripts/migrate.sh          # solo central
bash deploy/scripts/migrate-init.sh   # primera vez
bash deploy/scripts/migrate-fleet.sh  # fleet + backfill (cron)
```

Lotes (cientos de tenants):

```env
MIGRATION_BATCH_SIZE=50
MIGRATION_BATCH_PAUSE=2s
```

Si fallan tenants individuales, el comando **continúa** y muestra resumen FAILED al final.

---

## I. Cómo ver logs

```bash
# Tiempo real
docker compose -f docker-compose.production.yml logs -f backend-go

# Últimas 200 líneas (JSON estructurado en producción)
docker compose -f docker-compose.production.yml logs --tail=200 backend-go

# Filtrar errores (ejemplo)
docker logs tukifac-backend-go 2>&1 | grep '"status":500'
```

---

## J. Rollback

### Automático (script)

Guarda la imagen anterior en `.deploy/previous-image` durante cada `deploy.sh`.

```bash
cd /opt/tukifac
bash deploy/scripts/rollback.sh
```

### Manual

```bash
# En .env fijar tag anterior
TUKIFAC_IMAGE=ghcr.io/TU_ORG/backend_principal:sha-anterior
docker compose -f docker-compose.production.yml pull backend-go
docker compose -f docker-compose.production.yml up -d --no-deps --force-recreate backend-go
curl -s http://127.0.0.1:3000/health
```

**Nota:** rollback de imagen no revierte cambios de esquema MySQL. Si migrate aplicó ALTER irreversibles, restaurar backup B2.

---

## K. Troubleshooting

| Síntoma | Causa probable | Acción |
|---------|----------------|--------|
| `health` → `mysql: down` | Firewall / credenciales / MySQL caído | Probar `mysql -h DB_HOST -u ...` desde VPS |
| 502 en NPM | Contenedor parado o puerto incorrecto | `docker ps`, `curl 127.0.0.1:3000/health` |
| 429 Too Many Requests | Rate limit | Ajustar `RATE_LIMIT_*` en `.env` |
| 403 token/empresa | `X-Tenant-Slug` ≠ JWT | Corregir frontend |
| Tabla no existe | Falta migrate-central post-deploy | `docker exec ... ./tukifac-api migrate-central` |
| migrate tenant fallido | Permisos MySQL en esa BD | `migrate-tenant slug` y revisar error |
| GHCR pull denied | Sin login | `docker login ghcr.io` |
| Archivos perdidos | Sin volúmenes | Verificar `./data/uploads` y `./data/storage` en compose |

```bash
docker inspect tukifac-backend-go --format='{{json .State.Health}}' | jq .
docker compose -f docker-compose.production.yml exec backend-go ./tukifac-api help
```

---

## L. Seguridad recomendada

### Firewall (UFW) en VPS backend

```bash
sudo ufw default deny incoming
sudo ufw default allow outgoing
sudo ufw allow OpenSSH
sudo ufw allow 80/tcp
sudo ufw allow 443/tcp
# NO: ufw allow 3000
sudo ufw enable
```

### Fail2ban

```bash
sudo apt install fail2ban -y
sudo systemctl enable fail2ban
```

Protege SSH; opcional jail para NPM si expone logs.

### Puertos

| Puerto | Exponer |
|--------|---------|
| 22 | Solo IPs admin (idealmente VPN) |
| 80, 443 | Público (NPM) |
| 3000 | **NO** (solo localhost) |
| 81 | Panel NPM — restringir por IP o VPN |

### MySQL remoto

- `bind-address` en MySQL: IP privada o `0.0.0.0` + firewall estricto
- Solo IP del VPS backend
- Considerar TLS MySQL en tráfico entre datacenters
- Backups diarios (ej. rclone → B2)

### Secretos

- `.env` nunca en git
- Rotar `JWT_SECRET` implica re-login de todos los usuarios
- Usuario MySQL dedicado (no root)

---

## Referencias

- Hardening: [PRODUCTION-HARDENING.md](./PRODUCTION-HARDENING.md)
- Scripts: [deploy/README.md](../deploy/README.md)
- CI: [.github/workflows/deploy-production.yml](../.github/workflows/deploy-production.yml)
