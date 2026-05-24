# Roadmap eliminación legacy fiscal ERP

**Estado:** legacy ERP fiscal **eliminado** en path `FISCAL_DECOUPLED=true`. Rollout producción pendiente.

## Fase A — Staging (actual)

- [x] `FISCAL_DECOUPLED=true` en staging
- [x] UBL Go → **paquete eliminado** (stub only)
- [x] `pseAdapter` → **eliminado** (`disabledPseAdapter`)
- [x] `sendToLycet` sync HTTP → **eliminado** (solo enqueue)
- [ ] Checklist staging 100% verde (ver STAGING-FISCAL-CHECKLIST.md + STAGING-RESULTS.md)

## Fase B — Rollout producción tenant por tenant

1. Activar `FISCAL_DECOUPLED=true` en ERP prod
2. Sincronizar empresa: `POST /api/company/sync-facturador`
3. Verificar 10 emisiones SUNAT + 5 PSE por tenant
4. Monitorear dashboard 48h
5. Siguiente tenant

**Rollback por tenant:** desactivar SUNAT en tenant + drenar colas; NO volver a legacy salvo emergencia.

## Fase C — Eliminación código (path FISCAL_DECOUPLED=true) — **EJECUTADO 2026-05-23**

| Módulo ERP | Acción | Estado |
|------------|--------|--------|
| `internal/billing/ubl/` | Eliminar paquete | ✅ |
| `pseAdapter` en `invoicing_adapters.go` | Eliminar → `disabledPseAdapter` | ✅ |
| Sync HTTP legacy en `billing_service.sendToLycet` | Eliminar rama post-decouple | ✅ |
| Storage fiscal ERP local (sync venta) | Eliminado en sendToLycet | ✅ |
| Retries SUNAT legacy sync venta | Eliminado | ✅ |
| `saveInvoiceFile` NC/resumen/baja/guía | **Pendiente** — migrar a facturador | ⏳ |

## Fase D — Flag permanente

- Remover `FISCAL_DECOUPLED` como opcional → siempre desacoplado
- Documentar en README que ERP no emite SUNAT

## NO eliminar hasta

- Staging checklist completo
- 30 días prod sin duplicados SUNAT
- Dashboard y webhook audit operativos
- Backup/restore fiscal_documents validado
