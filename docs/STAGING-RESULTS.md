# Resultados staging fiscal — Reporte honesto

**Fecha:** 2026-05-23  
**Entorno:** desarrollo local (Windows) + arquitectura desacoplada  
**Estado global:** ⚠️ **NO listo para producción 100%** — falta E2E real SUNAT/PSE en VPS staging

---

## Resumen ejecutivo

| Área | Automatizado local | E2E staging VPS | Veredicto |
|------|-------------------|-----------------|-----------|
| Build ERP + tests cola/dedup | ✅ PASS | — | OK |
| Concurrencia 100 tenants (cola ERP) | ✅ PASS | ⏳ pendiente | Parcial |
| Idempotencia unitaria | ✅ PASS | ⏳ pendiente doble-click POS real | Parcial |
| SUNAT directo E2E | ⏳ | ⏳ **requiere credenciales beta** | **NO probado** |
| PSE ValidaPSE E2E | ⏳ | ⏳ **requiere tenant PSE real** | **NO probado** |
| Failover (Redis/worker/webhook/SMTP) | ⏳ | ⏳ manual en VPS | **NO probado** |
| Dashboard operacional | ✅ código | ⏳ validación soporte real | Parcial |
| Email fiscal adjuntos | ✅ código | ⏳ emit real + SMTP | Parcial |
| Cleanup legacy ERP | ✅ ejecutado | — | OK (FISCAL_DECOUPLED) |

---

## 1. Qué se probó REALMENTE (automatizado)

Ejecutado con `scripts/fiscal-staging/run-automated.ps1`:

| Test | Resultado | Detalle |
|------|-----------|---------|
| `go build ./...` | **PASS** | Compilación ERP sin paquete UBL legacy |
| `go test ./pkg/fiscalqueue/...` | **PASS** | Cola fiscal Redis |
| `go test ./pkg/fiscaldedup/...` | **PASS** | Dedup webhook/idempotencia |
| `go test ./internal/billing/service/...` | **PASS** | UBL stub, modo desacoplado |
| `ConcurrentEnqueue100Tenants` | **PASS** | 100 tenants, ~31ms enqueue, 0 errores |

**Métricas concurrencia cola ERP (local):**

- Tenants simulados: 100
- Jobs encolados: 100
- Errores: 0
- Latencia enqueue (pico): ~31 ms
- Nota: no incluye worker Greenter ni SUNAT real

---

## 2. Qué NO se probó (requiere VPS staging)

### 2.1 Tenant SUNAT directo — E2E

| Documento | Estado | Notas |
|-----------|--------|-------|
| Boleta (03) | ⏳ PENDIENTE | Credenciales SOL beta + certificado |
| Factura (01) | ⏳ PENDIENTE | |
| Nota crédito (07) | ⏳ PENDIENTE | |
| Nota débito (08) | ⏳ PENDIENTE | |
| Resumen boletas (RC) | ⏳ PENDIENTE | Async + status_poll |
| Baja (RA) | ⏳ PENDIENTE | |
| Guía remisión (09) | ⏳ PENDIENTE | |

**Flujo a validar manualmente:** ERP → PENDING_FISCAL → facturador → worker → SUNAT → CDR → storage → webhook → tenant DB → dashboard → email.

### 2.2 Tenant PSE (ValidaPSE)

| Escenario | Estado |
|-----------|--------|
| Aceptado | ⏳ PENDIENTE |
| Rechazado | ⏳ PENDIENTE |
| Timeout | ⏳ PENDIENTE |
| Credenciales inválidas | ⏳ PENDIENTE |
| SUNAT caída | ⏳ PENDIENTE |
| PSE caída | ⏳ PENDIENTE |

### 2.3 Concurrencia multi-tenant REAL

| Métrica | Local (cola) | Staging real |
|---------|--------------|--------------|
| 100 tenants simultáneos | ✅ cola ERP | ⏳ emit + SUNAT |
| POS no bloqueado | ⏳ | ⏳ medir p95 respuesta Enqueue |
| Tenant bleed | ✅ tests unitarios | ⏳ auditar Redis keys |
| Duplicados fiscales | ✅ dedup unit | ⏳ doble-click POS |
| Queue lag | N/A | ⏳ |
| Worker throughput | N/A | ⏳ |

**Comando staging (VPS):**

```bash
php bin/console app:fiscal:load-test --tenants=100 --docs-per-tenant=3
php bin/console app:fiscal:stress-test
```

### 2.4 Idempotencia REAL

| Escenario | Estado |
|-----------|--------|
| Doble click POS | ⏳ PENDIENTE |
| Retry timeout | ⏳ PENDIENTE |
| Worker crash mid-emit | ⏳ PENDIENTE |
| Webhook duplicado | ⏳ PENDIENTE (dedup unit OK) |
| Reenvío accidental | ⏳ PENDIENTE |

### 2.5 Failover

| Fallo | Estado | Comportamiento esperado |
|-------|--------|-------------------------|
| SUNAT caída | ⏳ | retry + FAILED, sin duplicar |
| PSE caída | ⏳ | pse_retry queue |
| Redis restart | ⏳ | workers reconectan, jobs persisten en DB |
| Worker restart | ⏳ | claim idempotente |
| Webhook ERP offline | ⏳ | webhook_sync retry |
| SMTP fail | ⏳ | email queue retry |
| Storage unavailable | ⏳ | emit failed, retry |

---

## 3. Cleanup legacy ERP — ejecutado

Con `FISCAL_DECOUPLED=true` (path producción):

| Módulo | Acción | Estado |
|--------|--------|--------|
| `internal/billing/ubl/` | **Eliminado** | ✅ |
| `realUBLGenerator` | **Eliminado** — stub only | ✅ |
| `pseAdapter` ValidaPSE Go | **Eliminado** — `disabledPseAdapter` | ✅ |
| Sync HTTP directo `sendToLycet` | **Eliminado** — solo enqueue | ✅ |
| Billing sync legacy | Webhook único path desacoplado | ✅ |

**Compatibilidad tenant DB:** campos `sunat_status`, `xml_url`, `cdr_url`, `pdf_url`, `sunat_hash`, `ticket` siguen sincronizados vía `POST /api/internal/fiscal/status` desde facturador.

**Pendiente eliminar (no crítico):** `saveInvoiceFile` en NC/resumen/baja/guía dentro de `billing_service.go` — rutas sync legacy documentos no-venta; migrar a facturador en rollout.

---

## 4. Dashboard operacional

**Código verificado:** KPIs, filtros, timeline, attempts, webhooks, emails, descargas PDF/XML/CDR, acciones retry/email/poll.

**Pendiente validación soporte real:** caso "mi factura no llegó" con documento staging real en dashboard.

---

## 5. Qué falló / bloqueadores

| Item | Detalle |
|------|---------|
| E2E SUNAT/PSE | Sin VPS staging accesible desde este entorno |
| PHP facturador local | `vendor/` no instalado en dev Windows — tests PHP en VPS |
| Producción 100% | **NO recomendado** hasta checklist §2 completo |

---

## 6. Riesgos que quedan

1. **Greenter + certificados SOL** no validados en emit real post-cleanup.
2. **S3/R2 multi-nodo** sin prueba de descarga email/dashboard cross-node.
3. **Resumen/baja async** — lag `fiscal:status_poll` desconocido bajo carga.
4. **NC/ND/guía desde ERP** — parte del sync legacy en `billing_service.go` aún presente para documentos no desacoplados.

---

## 7. Checklist final producción

- [ ] STAGING-FISCAL-CHECKLIST.md 100% marcado en VPS
- [ ] 2 tenants (SUNAT + PSE) 48h sin incidentes
- [ ] Failover Redis + worker probado
- [ ] Webhook audit con entregas OK en dashboard
- [ ] Email con adjuntos reales recibido
- [ ] `FISCAL_DECOUPLED=true` en prod ERP
- [ ] Workers facturador ≥ 2 réplicas
- [ ] Monitoreo: queue lag, SUNAT errors, webhook failures

---

## 8. Rollout recomendado

```
5 tenants (SUNAT directo, bajo volumen)
  ↓ 72h monitoreo: Redis lag, webhook, cero duplicados
20 tenants (+ mix PSE)
  ↓ 1 semana
50 tenants
  ↓ 1 semana
100% tenants
```

**Monitorear:** Redis memory, worker throughput, queue lag, tenant bleed (auditoría RUC en keys), SUNAT/PSE error rate, webhook failures, email failures.

**Rollback por tenant:** desactivar SUNATLayer tenant + drenar colas; NO reactivar legacy ERP (eliminado).

---

## 9. Veredicto honesto

**Arquitectura y código:** ~98% implementado; legacy ERP fiscal eliminado en path desacoplado.  
**Validación producción:** ~35% — solo tests automatizados locales; **E2E fiscal real pendiente**.  
**Recomendación:** ejecutar `STAGING-FISCAL-CHECKLIST.md` en VPS con credenciales beta antes de rollout 5 tenants.

Log automatizado: `docs/STAGING-RESULTS-AUTOMATED.log`
