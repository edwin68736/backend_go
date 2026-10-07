# Checklist de despliegue — release de performance (oct-2026)

Alcance: lo que hay entre la imagen en producción (`ae5cdcc`, 05-oct-2026) y `main` de `backend_principal` /
`frontend_tenant`. Incluye las 8 fases de performance **y** el trabajo previo aún sin desplegar (resumen fiscal, dashboard,
tienda virtual, compras con presentaciones, CCI, cotizaciones, v154–v158). **No** incluye cambios de Cloudflare, DNS, IPv6,
Argo ni infraestructura.

Orden: **backend → validar → frontend → validar → observar**. Cada despliegue es un workflow manual de GitHub Actions.

---

## Antes de empezar (T-0)

- [ ] `git push` de `backend_principal` y `frontend_tenant` (los dos en `main`).
- [ ] Ventana de baja carga (Lima): tráfico de oficina 09–17 h; mejor después de las 19:00 o antes de las 08:00.
- [ ] Anotar el punto de retorno:
  - Backend en producción: imagen `ghcr.io/edwin68736/backend_go` revisión `ae5cdcc` → etiqueta **`sha-ae5cdcc`** en GHCR.
  - `ssh deploy@72.62.83.69 'docker inspect tukifac-backend-go --format "{{.Image}} {{.Created}}"'`
- [ ] Respaldo de la carpeta del frontend (el workflow usa `rsync --delete`, no queda copia):
  `ssh deploy@72.62.83.69 'cp -a /opt/tukifac/frontend-tenant /opt/tukifac/frontend-tenant.bak-$(date +%F)'`
- [ ] Línea base (para comparar luego), en el VPS:
  ```bash
  docker logs --since 1h tukifac-backend-go 2>&1 | grep -c '"route":"/api/billing/events"'   # hoy ≈ 2.400/h en total (reconexión cada ~3 s por página abierta)
  docker logs --since 1h tukifac-backend-go 2>&1 | grep -c '"status":429'
  docker stats --no-stream tukifac-backend-go                                               # memoria hoy ~910 MiB de 1 GiB
  ```
- [ ] Confirmar que existe un respaldo reciente de las bases de datos (tenants y central).

> **Aviso:** en el VPS solo existe `deploy/scripts/migrate-fleet.sh`. El `rollback.sh` que menciona el workflow **no está
> instalado**; el retorno es manual (ver "Retorno").

---

## FASE A — Desplegar el backend

1. GitHub → `backend_go` → Actions → **Deploy Production** → *Run workflow* (**sin** marcar `skip_migrate`).
   El workflow: construye imagen → `migrate-central` → recrea el contenedor → espera `/health`.
2. **Inmediatamente** después, en el VPS, migrar los tenants (no esperar al cron):
   ```bash
   cd /opt/tukifac && MIGRATE_LIMIT=1000 bash deploy/scripts/migrate-fleet.sh
   tail -n 25 /opt/tukifac/logs/migrate-fleet.log     # esperar: "Fleet completed: success=N failed=0"
   ```
   Por qué: el cron migra 100 tenants cada 5 min y estas migraciones agregan columnas que el código nuevo ya escribe:
   | Migración | Rompe (hasta migrar ese tenant) |
   |---|---|
   | v154 | crear/editar cotizaciones (métodos de pago de referencia) |
   | v157 | registrar compras (columna `presentation_id`) |
   | v158 | crear/editar cuentas bancarias (columna `cci`) |
   | v155, v156 | solo índice / reparación de datos (sin riesgo) |
   Síntoma de que falta migrar: `Error 1054 ... Unknown column`. Las migraciones son aditivas: el código anterior las ignora.
3. Super Admin → *Fleet Migrations*: todos los tenants en la versión objetivo (158), 0 fallidos.

## FASE B — Validar el backend (antes de tocar el frontend)

Si algo de esta fase falla → ir a "Retorno", no avanzar.

| # | Qué | Cómo | Esperado |
|---|---|---|---|
| B1 | Salud | `curl -fsS https://api.tukifac.com/api/health/live` y en el VPS `curl -fsS http://127.0.0.1:3000/health` | 200 |
| B2 | Contenedor | `docker inspect tukifac-backend-go --format '{{.State.Status}} restarts={{.RestartCount}} rev={{index .Config.Labels "org.opencontainers.image.revision"}}'` | `running`, rev = el nuevo commit |
| B3 | Errores | `docker logs --since 10m tukifac-backend-go 2>&1 \| grep -cE 'Unknown column\|"level":"ERROR"\|panic'` | 0 (o solo los habituales) |
| B4 | **SSE** | Abrir una página de facturación (p. ej. Documentos SUNAT) en un navegador y dejarla 1 min. `docker logs --since 2m ... \| grep -c '"route":"/api/billing/events"'` | **1 por página abierta** (antes ≈ 20/min por página) |
| B5 | **RUM** | `curl -s -o /dev/null -w '%{http_code}' -X POST https://doriconta.tukifac.com/api/public/rum -H 'Content-Type: application/json' -d '{"nav":{"dns":1,"connect":120,"tls":110,"ttfb":300,"load":900},"protocol":"h2"}'` y luego `docker logs --since 1m ... \| grep rum_sample` | `204`; una línea `rum_sample` **sin** IP/UA/token |
| B6 | **client_ip** | ver sección siguiente | `client_ip` = tu IP real |
| B7 | **Rate limit** | `curl -sI https://doriconta.tukifac.com/api/zz-x \| grep -i x-ratelimit` | `x-ratelimit-limit: 300` (sin cambio) |
| B8 | **ETag** | Con la sesión del navegador (DevTools → Network): recargar y ver `company/config` y `company/branches` → 2ª carga con tamaño transferido ≈ 0.3 KB (304) | 304 |
| B9 | API general | Iniciar sesión (frontend anterior sigue activo), abrir Ventas, Productos, Compras; registrar una compra y una cuenta bancaria con CCI | sin errores |
| B10 | Memoria | `docker stats --no-stream tukifac-backend-go` | ≤ ~950 MiB (límite 1 GiB; vigilar) |

### Verificación de IP real (B6) — cadena Cliente → Cloudflare → NPM → backend

Qué ve cada capa hoy (verificado en producción el 07-oct-2026):

| Capa | Qué ve / hace |
|---|---|
| Cloudflare | Conoce la IP real. Envía `CF-Connecting-IP`. Rechaza (403) peticiones que ya traen `CF-Connecting-IP` falsificado. Un `X-Real-IP`/`X-Forwarded-For` enviado por el cliente **no** llega a determinar la IP que ve el backend (probado). |
| NPM (`*.tukifac.com`, `/api/`) | `X-Forwarded-For = $remote_addr` (**sobrescribe**; = IP del edge de Cloudflare) y `X-Real-IP = $remote_addr`. `CF-Connecting-IP` pasa intacto. |
| NPM (`api.tukifac.com`, otros paths) | `X-Forwarded-For = $proxy_add_x_forwarded_for` (lo del cliente + `$remote_addr` al final). |
| Backend | Fiber confía en `X-Forwarded-For` solo si el par TCP es privado/loopback (la red Docker). `c.IP()` = ese header completo (campo de log `ip` = IP del edge, sin cambio). `ClientIP()` toma el **último** elemento (lo que NPM vio como par real) y solo si pertenece a un rango de Cloudflare usa `CF-Connecting-IP`. |

Pruebas (desde tu PC; ejecutar y mirar el log en el VPS):
```bash
MI_IP=$(curl -s -4 https://api.ipify.org); echo "mi IP: $MI_IP"

# 1) Vía Cloudflare: client_ip debe ser MI_IP e ip debe ser un edge de Cloudflare
curl -s -o /dev/null https://doriconta.tukifac.com/api/zz-verify-cf

# 2) Directo al origen con CF-Connecting-IP FALSO: client_ip debe ser MI_IP (la del par), NO 1.2.3.4
curl -sk -o /dev/null --resolve doriconta.tukifac.com:443:72.62.83.69 \
     https://doriconta.tukifac.com/api/zz-verify-direct -H 'CF-Connecting-IP: 1.2.3.4'

# 3) Directo al origen, X-Forwarded-For con una IP de Cloudflare falsa: client_ip debe ser MI_IP
curl -sk -o /dev/null --resolve doriconta.tukifac.com:443:72.62.83.69 \
     https://doriconta.tukifac.com/api/zz-verify-direct2 -H 'X-Forwarded-For: 172.64.222.1' -H 'CF-Connecting-IP: 1.2.3.4'
```
En el VPS:
```bash
docker logs --since 3m tukifac-backend-go 2>&1 | grep zz-verify | sed -E 's/.*"route":"([^"]*)".*"ip":"([^"]*)".*"client_ip":"([^"]*)".*/\1  ip=\2  client_ip=\3/'
```
Criterios:
- [ ] `zz-verify-cf`: `client_ip` = `MI_IP` y `ip` ∈ rangos de Cloudflare (172.64–71.x, 104.16–31.x, 162.158–159.x, 198.41.128–255.x…).
- [ ] **`client_ip` nunca es una IP de Cloudflare** (`docker logs --since 10m ... | grep client_ip | grep -E '"client_ip":"(172\.6[4-9]|172\.7[01]|104\.(1[6-9]|2[0-9]|3[01])|162\.15[89]|198\.41\.)'` → sin resultados, salvo health checks de Cloudflare).
- [ ] `zz-verify-direct` / `-direct2`: `client_ip` = `MI_IP` (el `CF-Connecting-IP` falso se ignora).
- [ ] Si `client_ip` sale igual al edge para tráfico normal → NPM no reenvía `CF-Connecting-IP`: el comportamiento queda **como antes** (sin empeorar) pero hay que revisar NPM antes de dar el cambio por bueno.
- [ ] IPv6: un cliente IPv6 aparece como `xxxx:xxxx:xxxx:xxxx::/64`.

Endurecimiento recomendado en NPM (**no aplicado**, requiere autorización): hoy `set_real_ip_from` confía también en rangos de
CloudFront (no solo Cloudflare) y el origen acepta conexiones directas en 80/443. Dejar solo rangos de Cloudflare y/o
restringir 80/443 del VPS a Cloudflare eliminaría la posibilidad (remota) de falsificar `X-Real-IP` desde un CDN ajeno.

## FASE C — Desplegar el frontend (solo si la Fase B está en verde)

1. GitHub → `frontend_tenant` → Actions → **Deploy Tenant Frontend** → *Run workflow*.
   (`npm ci` + `npm run build` + rsync `--delete` a `/opt/tukifac/frontend-tenant`.)
2. Las pestañas ya abiertas piden archivos con hash antiguos que dejan de existir: **recargar** si algo falla al abrir una
   vista (error al cargar un módulo). Por eso conviene la ventana de baja carga.
3. Cloudflare: `index.html` no se cachea (`cf-cache-status: DYNAMIC`); los `/assets/*` llevan hash e `immutable`. No hace falta purgar.

## FASE D — Validar el frontend

- [ ] Login (tenant de pruebas `doriconta`): entra y no hay errores en consola.
- [ ] Network en la carga inicial: **sin** `vendor-charts` ni `vendor-jspdf` (solo `index`, `vendor-react`, `vendor-common`).
- [ ] Dashboard: se dibujan los gráficos (donut de utilidades, barras, etc.).
- [ ] POS (contrato de `docs/POS-PERFORMANCE-CONTRACT.md`): en Network, `sessions/open` ×1, `products` ×1, `categories` ×1 salen a la vez;
      **no** aparece `session/context`; `payment-methods`, `bank-accounts`, `contacts` salen **después** de ver los productos.
- [ ] Venta completa: agregar producto → Cobrar → datos de cobro cargados (método de pago, "Clientes Varios", serie) → Finalizar → recibo.
- [ ] Cobrar inmediatamente tras abrir el POS (antes de 1 s): el cobro se abre solo.
- [ ] PDF: vista previa A4 y ticket del comprobante; PDF de una cotización (con Observaciones y CCI en cuentas).
- [ ] Gráficos de Reportes → Utilidades.
- [ ] Cambio de sucursal (menú de usuario → sucursal): el POS recarga caja/productos de la nueva sucursal y los datos de cobro; no queda stock/series de la anterior.
- [ ] Home: banners se ven nítidos (WebP) y rotan.
- [ ] Compras: registrar compra con producto con presentaciones; columna "Pago" correcta.
- [ ] RUM: tras 1–2 min con la app abierta, `docker logs ... | grep -c rum_sample` > 0 (envío cada 60 s y al cerrar la pestaña).

## FASE E — Observar (primeras 2 h y 24 h)

```bash
# cada hora, en el VPS (los logs rotan: 5 archivos × 20 MB ≈ 12 h; guardar lo que se quiera conservar)
docker logs --since 1h tukifac-backend-go 2>&1 | grep -c '"status":429'
docker logs --since 1h tukifac-backend-go 2>&1 | grep -c '"route":"/api/billing/events"'
docker logs --since 1h tukifac-backend-go 2>&1 | grep -E 'Unknown column|"level":"ERROR"' | cut -c1-200 | sort | uniq -c | sort -rn | head
docker stats --no-stream tukifac-backend-go
docker logs --since 12h tukifac-backend-go 2>&1 | grep rum_sample | node backend_principal/scripts/rum-report.mjs   # ver docs/RUM-TELEMETRY.md
```
- [ ] 429: igual o menor que la línea base (esperado: bajan, ya no comparten cupo por IP de Cloudflare).
- [ ] `billing/events`: de ≈2.400/h a ≈ número de páginas de facturación abiertas.
- [ ] Latencias del backend (`latency_ms` p95) sin cambio notable (antes: p50 16 ms, p95 80 ms).
- [ ] Memoria del contenedor estable.
- [ ] A las 24 h: `rum-report.mjs` → comparar conexión `v6` vs `v4`; con eso se decide lo de IPv6/Cloudflare.

---

## Retorno (si hay que volver atrás)

**Backend** (las migraciones de tenant y de la BD central son aditivas: la versión anterior las ignora, no hay que revertirlas):
```bash
ssh deploy@72.62.83.69
cd /opt/tukifac
sed -i 's|^TUKIFAC_IMAGE=.*|TUKIFAC_IMAGE=ghcr.io/edwin68736/backend_go:sha-ae5cdcc|' .env
docker compose -f docker-compose.production.yml pull backend-go
docker compose -f docker-compose.production.yml up -d --no-deps --force-recreate backend-go
curl -fsS http://127.0.0.1:3000/health
```
**Frontend:** `ssh deploy@72.62.83.69 'rm -rf /opt/tukifac/frontend-tenant && cp -a /opt/tukifac/frontend-tenant.bak-AAAA-MM-DD /opt/tukifac/frontend-tenant'`
(o volver a ejecutar el workflow desde el commit anterior).

Criterios para volver atrás: errores `Unknown column` que no cierran tras la migración fleet; `client_ip` vacío o roto con 429 masivos;
POS que no carga productos; reinicios del contenedor; memoria > 1 GiB sostenida.
