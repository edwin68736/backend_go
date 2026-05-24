# Comandos y workers fiscales — Guía de ejecución

Documentación práctica para operar documentos fiscales en **backend_go** (ERP) y **facturador_lycet** (motor fiscal).

> **Importante:** la emisión fiscal **no usa cron jobs clásicos**. Usa **workers de larga duración** que consumen colas Redis. Los reintentos también los procesa el worker (sorted sets Redis), no un cron aparte.

---

## Vista general

```
POS / ERP (backend_go)
    │
    │  POST /api/billing/send/:saleId  →  encola job
    ▼
Redis: tukifac:fiscal:queue          ← workers ERP (embebidos en la API)
    │
    │  POST /api/v1/fiscal/emit
    ▼
facturador_lycet
    │
    │  encola fiscal:emit
    ▼
Redis: fiscal:emit | fiscal:email | fiscal:webhook_sync | fiscal:status_poll
    │
    │  php bin/console app:fiscal:worker  (proceso separado, N réplicas)
    ▼
SUNAT / PSE → storage → webhook ERP → email cliente
```

| Componente | Qué corre | Dónde |
|------------|-----------|-------|
| Worker ERP → facturador | Automático al iniciar API | `backend_go` (proceso `tukifac-api serve`) |
| Worker facturador (emit, email, webhook, poll) | Comando Symfony | `facturador_lycet` (supervisor/systemd) |
| Reintentos SUNAT/PSE/webhook/poll | Dentro del worker facturador | No requiere cron |
| Migraciones tenants ERP | Cron cada 5 min | `migrate-fleet-cron` (no es emisión fiscal) |

---

## 1. backend_go (ERP)

### 1.1 Worker fiscal (automático)

Al arrancar la API, `runtime.Init()` inicia workers fiscales si:

- `FISCAL_DECOUPLED=true`
- `REDIS_URL` configurado
- `FACTURADOR_BASE_URL` + `FACTURADOR_TOKEN` configurados

```bash
# Producción — el worker fiscal va DENTRO del proceso API
./tukifac-api serve
# o con Docker:
docker compose up -d backend-go
```

**Variables clave:**

```env
FISCAL_DECOUPLED=true
FISCAL_QUEUE_WORKERS=4          # goroutines que consumen tukifac:fiscal:queue
REDIS_URL=redis://tukifac-redis:6379/0
FACTURADOR_BASE_URL=https://facturador.midominio.com
FACTURADOR_TOKEN=<igual que CLIENT_TOKEN en facturador>
INTERNAL_API_KEY=<clave compartida con facturador>
```

**Cola Redis ERP:**

| Key | Uso |
|-----|-----|
| `tukifac:fiscal:queue` | Jobs `{tenant_db, tenant_id, sale_id, idempotency_key}` |
| `tukifac:fiscal:claim:{key}` | Evita doble encolado (TTL 2 min) |

**Qué hace cada job:** construye snapshot JSON de la venta y llama `POST /api/v1/fiscal/emit` al facturador. **No emite a SUNAT desde Go.**

**Logs al arrancar (buscar):**

```
runtime_initialized fiscal_decoupled=true billing_async=true
```

**Escalar workers ERP:** subir `FISCAL_QUEUE_WORKERS` y/o réplicas del contenedor API (comparten la misma cola Redis).

---

### 1.2 Flujo desde el POS (sin comando manual)

```bash
# El frontend/POS llama:
POST /api/billing/send/:saleId
Authorization: Bearer <token tenant>
```

Respuesta rápida (~202): venta queda `PENDING_FISCAL`. El worker ERP procesa en background.

**Sincronizar empresa con facturador (una vez por tenant o tras cambio SOL/PSE):**

```bash
POST /api/company/sync-facturador
# o superadmin:
POST /api/superadmin/tenants/:id/sync-facturador
```

**Webhook que recibe estados desde facturador:**

```
POST /api/internal/fiscal/status
Headers: X-Internal-Key, X-Fiscal-Signature, X-Fiscal-Event-Id
```

---

### 1.3 Comandos CLI backend_go (relacionados)

El binario `tukifac-api` **no tiene** subcomando fiscal dedicado. Los workers fiscales solo corren con `serve`.

| Comando | Uso fiscal |
|---------|------------|
| `./tukifac-api serve` | **Inicia workers fiscales** + API HTTP |
| `./tukifac-api migrate-fleet-cron` | Migraciones BD tenants (cron infra, no emisión) |

**Script tests automatizados (local/CI):**

```powershell
# Windows
.\scripts\fiscal-staging\run-automated.ps1

# Linux/macOS — equivalente manual:
cd backend_go
go build ./...
go test ./pkg/fiscalqueue/... ./pkg/fiscaldedup/... ./internal/billing/service/... -count=1
go test ./pkg/fiscalqueue/ -run ConcurrentEnqueue100Tenants -v
```

**Monitoreo Redis ERP:**

```bash
redis-cli LLEN tukifac:fiscal:queue
redis-cli KEYS "tukifac:fiscal:claim:*" | wc -l
```

---

### 1.4 Cron del ERP (NO fiscal, pero convive en prod)

Estos cron **no emiten comprobantes**; son infraestructura SaaS:

```cron
# /etc/cron.d/tukifac — migraciones fleet tenants cada 5 min
*/5 * * * * /opt/tukifac/deploy/scripts/migrate-fleet.sh >> /var/log/tukifac/cron-migrate.log 2>&1
```

Ver [MIGRATIONS-SaaS.md](./MIGRATIONS-SaaS.md).

---

## 2. facturador_lycet (motor fiscal)

### 2.1 Worker principal — OBLIGATORIO en producción

```bash
cd /opt/facturador_lycet

# Migraciones (incluye fiscal_documents, webhook audit, etc.)
php bin/console doctrine:migrations:migrate --no-interaction

# Worker fiscal — proceso de larga duración (NO cron)
php bin/console app:fiscal:worker
```

**Opciones:**

| Opción | Descripción |
|--------|-------------|
| `--once` | Un ciclo (útil debug/cron puntual de rescate) |
| `--queue=fiscal:emit` | Solo una cola específica |

**Ejemplos:**

```bash
# Producción — loop infinito (default)
php bin/console app:fiscal:worker

# Debug — procesa jobs pendientes y sale
php bin/console app:fiscal:worker --once

# Solo cola de emisión
php bin/console app:fiscal:worker --queue=fiscal:emit

# Solo emails
php bin/console app:fiscal:worker --queue=fiscal:email
```

**Colas que consume (orden por ciclo):**

| Cola Redis | Procesador | Qué hace |
|------------|------------|----------|
| `fiscal:emit` | `FiscalEmitProcessor` | Greenter → SUNAT directo o ValidaPSE |
| `fiscal:email` | `FiscalEmailProcessor` | SMTP + adjuntos PDF/XML/CDR |
| `fiscal:webhook_sync` | `FiscalWebhookSyncProcessor` | POST estado al ERP |
| `fiscal:status_poll` | `FiscalStatusPollProcessor` | Consulta ticket (resumen, baja, guía async) |

**Colas de reintento (sorted set, procesadas al inicio de cada ciclo):**

| Cola | Uso |
|------|-----|
| `fiscal:retry` | Reintento SUNAT directo |
| `fiscal:pse_retry` | Reintento PSE |
| `fiscal:webhook_sync` | Reintento webhook ERP (también como cola lista) |
| `fiscal:status_poll` | Reintento consulta ticket |

> No necesitas cron para retries: el worker llama `processDueRetries()` en cada iteración.

---

### 2.2 Variables de entorno facturador

```env
# Redis (obligatorio para workers)
REDIS_URL=redis://tukifac-redis:6379/0

# Webhook hacia ERP
ERP_WEBHOOK_URL=https://api.midominio.com/api/internal/fiscal/status
ERP_WEBHOOK_KEY=<mismo INTERNAL_API_KEY del ERP>

# API token (ERP lo usa como FACTURADOR_TOKEN)
CLIENT_TOKEN=...

# PSE
VALIDAPSE_BASE_URL=https://app.validapse.com

# PDF
WKHTMLTOPDF_PATH=/usr/bin/wkhtmltopdf

# Email
MAILER_DSN=smtp://user:pass@smtp.example.com:587
FISCAL_MAIL_FROM=facturacion@midominio.com

# Storage (local o S3/R2)
FISCAL_STORAGE_DRIVER=local
# FISCAL_STORAGE_DRIVER=r2
# FISCAL_S3_BUCKET=...
# FISCAL_S3_PUBLIC_URL=...
```

---

### 2.3 systemd — producción (recomendado)

Crear **≥ 2 instancias** del worker para alta disponibilidad.

**/etc/systemd/system/facturador-fiscal-worker@.service**

```ini
[Unit]
Description=Facturador fiscal worker %i
After=network.target redis.service
Wants=redis.service

[Service]
Type=simple
User=www-data
WorkingDirectory=/opt/facturador_lycet
ExecStart=/usr/bin/php bin/console app:fiscal:worker
Restart=always
RestartSec=5
EnvironmentFile=/opt/facturador_lycet/.env

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now facturador-fiscal-worker@{1..2}
sudo systemctl status facturador-fiscal-worker@1
journalctl -u facturador-fiscal-worker@1 -f
```

**API HTTP facturador** (separado del worker):

```bash
# Ejemplo con PHP built-in (dev) o nginx+php-fpm (prod)
php -S 0.0.0.0:8000 -t public
```

---

### 2.4 Supervisor (alternativa)

```ini
[program:fiscal-worker]
command=php /opt/facturador_lycet/bin/console app:fiscal:worker
directory=/opt/facturador_lycet
user=www-data
autostart=true
autorestart=true
numprocs=2
process_name=%(program_name)s_%(process_num)02d
stdout_logfile=/var/log/facturador/fiscal-worker.log
stderr_logfile=/var/log/facturador/fiscal-worker.err.log
```

---

### 2.5 Comandos Symfony — listado completo fiscal

```bash
php bin/console list app:fiscal
```

| Comando | Propósito | ¿Producción? |
|---------|-----------|--------------|
| `app:fiscal:worker` | Consume colas fiscales | **Sí — siempre activo** |
| `app:fiscal:stress-test` | Idempotencia N tenants × duplicados | Solo QA/staging |
| `app:fiscal:load-test` | Carga multi-tenant encolado | Solo QA/staging |

**Stress test (sin SUNAT real):**

```bash
php bin/console app:fiscal:stress-test --tenants=100 --per-tenant=5
```

**Load test (sin SUNAT real):**

```bash
php bin/console app:fiscal:load-test --tenants=100 --docs-per-tenant=2 --dup-factor=3
```

**Setup empresas (no fiscal emit, pero necesario):**

```bash
php bin/console app:empresas:import-from-json
php bin/console doctrine:migrations:migrate --no-interaction
```

---

### 2.6 Monitoreo Redis facturador

```bash
redis-cli LLEN fiscal:emit
redis-cli LLEN fiscal:email
redis-cli LLEN fiscal:webhook_sync
redis-cli LLEN fiscal:status_poll
redis-cli ZCARD fiscal:retry
redis-cli ZCARD fiscal:pse_retry
```

**Dashboard operacional:**

```
GET https://facturador.midominio.com/login
# Tras login con usuario admin (app:admin:seed) → /dashboard
```

La API fiscal desde el navegador usa **cookie de sesión**. El `CLIENT_TOKEN` (`?token=`) es **solo** para `backend_go` y scripts.

**Acciones manuales desde API/dashboard:**

| Acción | Endpoint |
|--------|----------|
| Reenviar | `POST /api/v1/fiscal/documents/{uuid}/send` |
| Reintentar | `POST /api/v1/fiscal/documents/{uuid}/retry` |
| Forzar emisión | `POST /api/v1/fiscal/documents/{uuid}/force` |
| Reenviar email | `POST /api/v1/fiscal/documents/{uuid}/email` |
| Poll ticket async | `POST /api/v1/fiscal/documents/{uuid}/poll` |

---

## 3. Secuencia de arranque recomendada (staging/prod)

### Día 0 — infraestructura

```bash
# 1. Redis accesible desde ERP y facturador
redis-cli PING

# 2. Facturador — BD
cd facturador_lycet
php bin/console doctrine:migrations:migrate --no-interaction

# 3. Facturador — workers (≥ 2)
systemctl start facturador-fiscal-worker@{1..2}

# 4. ERP — API (incluye workers fiscales)
cd backend_go
./tukifac-api serve
# o docker compose up -d backend-go

# 5. Por tenant — sync empresa
curl -X POST https://api.midominio.com/api/company/sync-facturador \
  -H "Authorization: Bearer <tenant_token>"
```

### Verificación rápida

```bash
# Health ERP
curl https://api.midominio.com/health

# Stats facturador
curl "https://facturador.midominio.com/api/v1/fiscal/stats" \
  -H "Authorization: Bearer $FACTURADOR_TOKEN"

# Colas vacías o drenándose tras prueba
redis-cli LLEN tukifac:fiscal:queue
redis-cli LLEN fiscal:emit
```

---

## 4. Documentos asíncronos (resumen, baja, guía)

Estos **no reciben CDR inmediato**. Flujo:

1. `fiscal:emit` → SUNAT devuelve **ticket**
2. Worker encola `fiscal:status_poll` con backoff
3. `FiscalStatusPollProcessor` consulta ticket hasta CDR
4. Tras `accepted` → `fiscal:email` + `fiscal:webhook_sync`

**No hay cron de poll.** Todo lo maneja `app:fiscal:worker`.

Si un documento queda atascado en `polling`:

```bash
# Desde dashboard o API:
POST /api/v1/fiscal/documents/{uuid}/poll
```

---

## 5. Troubleshooting operativo

| Síntoma | Acción |
|---------|--------|
| Ventas en `PENDING_FISCAL` sin avanzar | Verificar `LLEN tukifac:fiscal:queue`, logs ERP, `FISCAL_DECOUPLED=true` |
| Documentos en `queued` en facturador | Reiniciar `app:fiscal:worker`; verificar `REDIS_URL` |
| SUNAT timeout | Revisar `fiscal:retry` / timeline en dashboard |
| ERP no actualiza estado | Verificar `fiscal:webhook_sync`, `ERP_WEBHOOK_URL`, `INTERNAL_API_KEY` |
| Email no llega | Cola `fiscal:email`, `MAILER_DSN`, logs `outbound_email_logs` |
| Redis reiniciado | Reencolar desde dashboard (`/send` o `/retry`) — jobs en memoria se pierden |
| Worker crash mid-emit | Idempotencia evita duplicado; reintentar con `/retry` |

---

## 6. Resumen: qué NO es cron fiscal

| Proceso | Tipo | Comando |
|---------|------|---------|
| ERP → facturador | Worker embebido API | `./tukifac-api serve` |
| Emit / email / webhook / poll | Worker Symfony | `php bin/console app:fiscal:worker` |
| Reintentos backoff | Dentro del worker | (automático) |
| Migraciones tenants ERP | **Cron real** | `migrate-fleet-cron` cada 5 min |
| Vencimiento suscripciones | Goroutine 24h en API | `cron.StartExpirationChecker()` |

---

## Referencias

- [FISCAL-OPERATIONS.md](./FISCAL-OPERATIONS.md) — flujos técnicos
- [STAGING-FISCAL-CHECKLIST.md](./STAGING-FISCAL-CHECKLIST.md) — QA pre-prod
- [ARQUITECTURA-FISCAL.md](./ARQUITECTURA-FISCAL.md) — diseño
- [facturador_lycet/README.md](../../facturador_lycet/README.md) — instalación base Lycet
