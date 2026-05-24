# Arquitectura fiscal SaaS — Fase 4 (completa)

## Resumen

| Componente | Rol |
|------------|-----|
| **backend_go** | ERP/POS: snapshots JSON, cola fiscal, sync empresa SaaS → facturador, **sin UBL/XML** |
| **facturador_lycet** | Motor fiscal único: todos los tipos doc, PDF, poll tickets, SSOT |
| **Redis** | Colas + dedup webhook + claims idempotencia |

## Tipos de documento soportados (fiscal async)

| tipoDoc / kind | Greenter | Flujo |
|----------------|----------|-------|
| 01, 03 | Invoice | Emisión sync → CDR |
| 07, 08 | Note | Emisión sync → CDR |
| 09 / despatch | Despatch | Emisión sync → CDR + PDF |
| RC / summary | Summary | Ticket → `fiscal:status_poll` → CDR |
| RA / voided | Voided | Ticket → poll → CDR |
| RR / reversion | Reversion | Ticket → poll → CDR |

Snapshot puede indicar `_meta.document_kind`: `invoice`, `note`, `despatch`, `summary`, `voided`, `reversion`.

## PDF fiscal

- `FiscalPdfService` usa Greenter Report (wkhtmltopdf) tras emisión
- Se almacena junto a XML/CDR (local o S3/R2)
- Solo factura/boleta, NC/ND y guía

## Sync empresa SaaS

ERP `SyncFacturador` envía vía `SyncEmpresa`:
- `tenant_id`, `tenant_slug`
- `send_mode`, `provider`, `pse_pass` (token PSE)
- `automatic_send`, `email_enabled`, `retry_enabled`, `enabled`

Facturador `EmpresasService` persiste en tabla `empresa`.

## Colas Redis facturador

```txt
fiscal:emit
fiscal:retry
fiscal:pse_retry
fiscal:email
fiscal:webhook_sync
fiscal:status_poll   ← nuevo fase 4
```

## ERP — código legacy desactivado

Con `FISCAL_DECOUPLED=true`:
- `NewUBLGenerator` → stub (error explícito)
- `pseAdapter` → bloqueado
- `sendToLycet` sync → bloqueado

## Activación producción

Ver fase 3 + asegurar `WKHTMLTOPDF_PATH` en facturador para PDF.

## Validación pre-producción

Ver checklist completo: **[STAGING-FISCAL-CHECKLIST.md](./STAGING-FISCAL-CHECKLIST.md)** (tenants SUNAT + PSE, resumen, guía, email, webhook, concurrencia).

```bash
# Idempotencia BD
php bin/console app:fiscal:stress-test --tenants=100 --per-tenant=5

# Carga multi-tenant encolado
php bin/console app:fiscal:load-test --tenants=100 --docs-per-tenant=2 --dup-factor=3

# Workers (incluye status_poll)
php bin/console app:fiscal:worker

# ERP cola 100 tenants concurrente
go test ./pkg/fiscalqueue/ -run ConcurrentEnqueue100Tenants -v

# ERP unitarios
go test ./pkg/fiscaldedup/... ./internal/billing/service/ -run 'Fiscal|UBL|Disabled' -count=1
```

Ver también: [FISCAL-OPERATIONS.md](./FISCAL-OPERATIONS.md), [REPORTE-FINAL-FISCAL.md](./REPORTE-FINAL-FISCAL.md)

## Migración 100% completada

El paquete `internal/billing/ubl` fue **eliminado** (2026-05-23). El ERP no genera XML; SSOT fiscal = `facturador_lycet`.
