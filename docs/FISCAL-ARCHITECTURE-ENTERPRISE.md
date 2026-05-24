# Arquitectura fiscal enterprise (multi-tenant)

Documento de referencia para el flujo de emisión electrónica SaaS: **backend_go (ERP por tenant)** + **facturador_lycet (SSOT fiscal)** + **Redis**.

## Principios

1. **Post-commit**: la cola fiscal se dispara solo después de persistir la venta/comprobante en la BD del tenant.
2. **No bloquear UI**: crear venta responde de inmediato; SUNAT ocurre en background.
3. **SSOT**: el facturador es la fuente de verdad del estado SUNAT; el ERP sincroniza vía webhook, sync puntual y reconcile.
4. **Idempotencia**: antes de enqueue/send/worker/reconcile → `SyncSaleWithSSOT(saleId)` + lock Redis por documento.
5. **Aislamiento tenant**: BD independiente por tenant; claves Redis incluyen `tenant_id`.

## Flujo end-to-end (automático)

```txt
POST /api/sales (01/03/07/…)
  → SaleService.Create (commit tenant DB)
  → MaybeAutoEnqueueAfterSaleCommit (si sunat_enabled && automatic_send)
    → PrepareFiscalOperation (lock + sync SSOT)
    → EnqueueSendToSUNAT
      → tukifac:fiscal:queue (preferida) o tukifac:billing:queue (legacy)
  → respuesta HTTP inmediata al cliente

Worker backend_go (fiscal/billing)
  → PrepareFiscalOperation
  → EmitFiscal / ProcessSendToSUNAT
  → POST facturador /fiscal/emit

Worker facturador (app:fiscal:worker)
  → fiscal:emit → SUNAT/PSE → webhook → ERP

Webhook / reconcile / SSE
  → SyncService.ApplyStatus → tenant_invoices + tenant_sales.billing_status
  → Redis PUBLISH tenant:{id}:billing_updates → SSE tenant UI
```

## Envío manual

`POST /api/billing/send/:saleId` y `resend` **encolan** (no esperan 90s). Respuestas:

| status | Significado |
|--------|-------------|
| `queued` | Encolado; usar SSE o GET `/api/billing/status/:saleId` |
| `already_accepted` | SSOT ya aceptó; ERP sincronizado |
| `already_processing` | Lock o pipeline activo; no reenviar |

## Configuración por tenant

| Campo | Ubicación | Default |
|-------|-----------|---------|
| `automatic_send` | `tenant_company_configs` (ERP) | `true` |
| `sunat_enabled` | panel central | `false` |

Si `automatic_send=false`: la venta queda `billing_status=pending` hasta envío manual.

Migración fleet: **V041** (`automatic_send_column`).

## Máquina de estados

### Pipeline (`tenant_invoices.pipeline_status`)

Estados ordenados: `DRAFT` → `PENDING_FISCAL|PENDING_QUEUE` → `PROCESSING|RETRYING` → `SENDING_*` → `FACTURADOR_RECEIVED` → terminales `SUNAT_ACCEPTED|OBSERVED|SUNAT_REJECTED|FAILED|DEAD_LETTER`.

Transiciones validadas en `pkg/billingstate/state.go` (`CanTransition`). **No** se permite `SUNAT_ACCEPTED → SENDING`.

### UX (`display_phase` en StatusView)

| display_phase | Cuándo |
|---------------|--------|
| `queued` | PENDING_QUEUE, PENDING_FISCAL |
| `sending` | PROCESSING, SENDING_*, FACTURADOR_RECEIVED |
| `retrying` | RETRYING |
| `accepted` | SUNAT_ACCEPTED |
| `rejected` | SUNAT_REJECTED |
| `error` | FAILED, DEAD_LETTER |

`billing_status` en venta sigue siendo `pending|sent|accepted|rejected|error` (compatibilidad listados).

## Anti-duplicidad

### Lock Redis

Clave: `tukifac:billing:lock:{tenantId}:{saleId}` — TTL 45s.

Protege: auto-enqueue, manual, resend, workers, reconcile.

### Sync puntual (no masivo)

`SyncSaleWithSSOT(saleId)` → GET `/api/v1/fiscal/documents?sale_id=` (vía UUID lookup).

### Claims de cola

- `tukifac:fiscal:claim:{idemKey}`
- `tukifac:billing:claim:{idemKey}`

## Reconcile worker

- Intervalo: 7 min (`pkg/cron/fiscal_reconcile.go`)
- Lock global cron: `fiscal:billing_reconcile`
- Batch: **100** ventas
- Edad mínima: **2 min**
- Solo estados activos (`pending`, `sent`, pipeline no terminal)
- Lock por venta durante sync
- Emite SSE al corregir desvío

## Redis — claves multi-tenant

| Clave | Uso |
|-------|-----|
| `tukifac:billing:lock:t{id}:{saleId}` | Lock documento |
| `tukifac:tenant:{id}:billing_updates` | Pub/Sub SSE |
| `tukifac:fiscal:queue` | Cola ERP → facturador |
| `tukifac:billing:queue` | Cola legacy SUNAT en ERP |
| `fiscal:emit` | Cola facturador PHP |

## Observabilidad

Logs estructurados `fiscal_operation` con: `tenant_id`, `sale_id`, `source`, `status`, `document_type`.

Sources: `auto_create`, `manual`, `manual_resend`, `queue`, `fiscal_queue`, `reconcile`.

## Procesos requeridos en producción

| Proceso | Comando |
|---------|---------|
| ERP + workers | `go run .` en backend_go |
| Facturador API | Symfony / PHP built-in |
| Worker fiscal PHP | `php bin/console app:fiscal:worker` |
| Redis compartido | `REDIS_URL` igual en ambos |

## Compatibilidad

- Panel central: `automatic_send` en PUT sunat-config tenant.
- **Notas de crédito (07)**, **notas de débito (08)**, **guías (09/31)**, **retenciones (20)** y **percepciones (40)** usan el mismo pipeline de cola (`EnqueueSendToSUNAT` → facturador SSOT).
- NC de anulación: la venta original se cancela automáticamente al aceptar SUNAT (webhook/reconcile).
- Guías / retenciones / percepciones: registros auxiliares vinculados con `sale_id` para webhook/SSE/reconcile.
- Resúmenes / bajas / reversiones — fuera de este cambio (sync legacy donde aplique).
