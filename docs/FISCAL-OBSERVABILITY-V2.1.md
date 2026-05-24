# Fiscal Observability V2.1

Observabilidad production-grade para emisión fiscal SaaS **sin modificar el core de emisión V2**.

## Principios

- SSOT sigue en `facturador_lycet.empresa`
- ERP envía solo `tenant_id + ruc + documento`
- Audit/logging **async** vía cola Redis `fiscal:audit`
- Nunca persistir secretos, XML completo ni tokens
- Hooks de observabilidad envueltos en try/catch — nunca fallan emisión

## Componentes

| Capa | Artefacto | Rol |
|------|-----------|-----|
| DB | `fiscal_audit_logs` | Timeline y forensics por documento/tenant |
| DB | `fiscal_alerts` | Alertas proactivas (sin envío externo aún) |
| DB | `fiscal_tenant_metrics` | KPIs diarios por tenant/provider |
| Redis | `fiscal:audit` | Buffer async de audit |
| Redis | `fiscal:worker:heartbeat` | Health worker |
| PHP | `FiscalAuditService` | Encola + drena audit |
| PHP | `FiscalStructuredLogger` | JSON unificado |
| PHP | `FiscalOperationsController` | API operaciones |
| Go BFF | `/api/superadmin/fiscal/*` | Proxy panel central |
| UI | `/fiscal-operations` | Dashboard Operaciones Fiscales |

## Eventos de audit

| event_type | status | Cuándo |
|------------|--------|--------|
| `fiscal_document_queued` | queued | Encolado en emit |
| `fiscal_processing_started` | processing | Worker inicia |
| `fiscal_provider_selected` | processing | Tras resolver provider |
| `fiscal_emit_success` | success | Aceptado SUNAT/PSE |
| `fiscal_emit_failed` | failed | Error/rechazo |
| `fiscal_retry_scheduled` | retrying | Retry programado |
| `fiscal_connection_test` | success/failed | Test conexión |
| `fiscal_configuration_updated` | success | company-sync |
| `fiscal_document_cancelled` | cancelled | Cancelación operativa |

## Endpoints (facturador_lycet)

```
GET  /api/v1/fiscal/health
GET  /api/v1/fiscal/operations/summary
GET  /api/v1/fiscal/operations/tenants
GET  /api/v1/fiscal/operations/queue
GET  /api/v1/fiscal/alerts
GET  /api/v1/fiscal/documents/{uuid}/audit-timeline
POST /api/v1/fiscal/documents/{uuid}/cancel
```

Panel central (BFF): `/api/superadmin/fiscal/...` (mismas rutas relativas).

## Migración

```bash
cd facturador_lycet
php bin/console doctrine:migrations:migrate --no-interaction
```

Migración: `Version20260530000000` (audit, alerts, metrics + índices).

## Worker

El worker fiscal debe estar corriendo para:

1. Procesar emisiones
2. Drenar cola audit → MySQL
3. Actualizar heartbeat

```bash
php bin/console app:fiscal:worker
```

## Alertas (detección automática)

- Certificado vencido (`connection_status = certificate_expired`)
- Tenant desconectado
- Errores consecutivos (≥5)
- Cola saturada (≥500 jobs emit)
- Retries anormales (≥50/24h por tenant)

Arquitectura preparada para email/WhatsApp — **envío no implementado**.

## Checklist producción

- [ ] Migración `Version20260530000000` aplicada
- [ ] `REDIS_URL` configurado en facturador
- [ ] Worker fiscal activo (systemd/supervisor, N≥1)
- [ ] `FACTURADOR_BASE_URL` + `FACTURADOR_TOKEN` en backend_go
- [ ] Panel `/fiscal-operations` accesible solo superadmin
- [ ] Logs JSON visibles en agregador (campo `event`)
- [ ] Retención audit definida (ej. 90 días — job futuro)
- [ ] Monitoreo externo sobre `GET /api/v1/fiscal/health`

## Smoke tests

```bash
# 1. Health
curl -H "Authorization: Bearer $TOKEN" \
  http://localhost:8000/api/v1/fiscal/health

# 2. Summary operaciones
curl -H "Authorization: Bearer $TOKEN" \
  http://localhost:8000/api/v1/fiscal/operations/summary

# 3. Emit de prueba → verificar audit
curl -X POST -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"tenant_id":1,"tenant_slug":"demo","sale_id":999,"ruc":"20600000001","document":{...}}' \
  http://localhost:8000/api/v1/fiscal/emit

# 4. Timeline
curl -H "Authorization: Bearer $TOKEN" \
  http://localhost:8000/api/v1/fiscal/documents/{uuid}/audit-timeline

# 5. Worker drena audit
php bin/console app:fiscal:worker --once
```

## Pruebas multi-tenant

1. Configurar 2+ empresas en `empresa` con distintos `tenant_slug`
2. Emitir documentos desde cada tenant
3. Verificar tabla tenants en `/fiscal-operations` muestra KPIs separados
4. Forzar error en un tenant → alerta `consecutive_errors` o fila en errores 24h
5. Confirmar que tenant A no ve datos de tenant B (panel superadmin ve todos)

## Qué NO se tocó (V2 intacto)

- `FiscalProviderResolver`
- Lógica de emisión en providers
- Payload ERP en emit
- Legacy fiscal eliminado en fases previas
