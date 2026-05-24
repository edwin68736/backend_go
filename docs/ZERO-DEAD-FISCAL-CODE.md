# Zero Dead Fiscal Code — Reporte cleanup técnico

**Fecha:** 2026-05-23  
**Alcance:** `backend_go`, `facturador_lycet`, `frontend_central`, `frontend_tenant`  
**Objetivo:** eliminar artefactos muertos fiscales sin cambiar lógica de negocio.

---

## 1. Archivos eliminados

| Archivo | Motivo |
|---------|--------|
| `backend_go/internal/billing/service/ubl_generator.go` | Stub UBL ERP (siempre error); sin callers |
| `backend_go/internal/billing/service/ubl_generator_test.go` | Test del stub eliminado |

*(Archivos eliminados en fase anterior: `legacy_adapter.go`, `fiscal_mode.go`, `config_service.go` — ver `LEGACY-FISCAL-REMOVED.md`)*

---

## 2. Configs eliminadas (`config.Config`)

| Campo | Estado |
|-------|--------|
| `TukifacBaseURL` | Eliminado |
| `TukifacAPIToken` | Eliminado |
| `FiscalDecoupledEnabled` | Eliminado (fase anterior) |
| `LegacyInvoiceEndpoint` | Eliminado (fase anterior) |

---

## 3. Variables de entorno eliminadas

| Variable | Archivos |
|----------|----------|
| `TUKIFAC_BASE_URL` | `.env.example` |
| `TUKIFAC_API_TOKEN` | `.env.example` |
| `FISCAL_DECOUPLED` | `.env.example` (fase anterior) |
| `LEGACY_INVOICE_ENDPOINT` | `.env.production.example` (fase anterior) |

**Conservadas (no son legacy fiscal):**

- `TUKIFAC_IMAGE` en `.env.production.example` — imagen Docker del backend ERP
- `FACTURADOR_*`, `FISCAL_QUEUE_WORKERS`, `INTERNAL_API_KEY` — runtime V2

---

## 4. Dead code removido

### backend_go

| Elemento | Acción |
|----------|--------|
| `BillingService.useLycet` | Eliminado → `facturadorConfigured()` |
| `TukifacInvoiceRequest`, `TukifacItemRequest`, `TukifacResponse` | Eliminados |
| Rama `provider = "tukifac"` en storage NC | Eliminada (rama muerta) |
| `lycetResponseToJSON` | Renombrado → `facturadorResponseToJSON` |
| `Client.SyncEmpresa`, `SyncEmpresas`, `EmpresaSyncOptions`, `EmpresaPayload` | Eliminados (sin callers; sync vía `CompanySync`) |
| `Client.SyncConfiguration`, `SyncConfigurationWithFiles` | Eliminados (API `/configuration/` legacy) |
| `fiscalqueue.EmitDirect` | Eliminado (sin callers) |
| `mapEnvToLycetAmbiente` | Renombrado → `mapEnvToFacturadorAmbiente` |

### facturador_lycet

| Elemento | Acción |
|----------|--------|
| `FiscalQueueService::QUEUE_SYNC` | Eliminado (alias deprecated) |
| Comentario `legacy pse_pass` en `Empresa` | Neutralizado |

### frontend_central

| Elemento | Acción |
|----------|--------|
| `listConectadosSunat()` | Eliminado → `listConectadosFacturador()` |
| `syncPSECredentials()` | Eliminado |
| `TenantConectadoSunat` type alias | Eliminado |
| `EmpresasSunatPage` | Usa `TenantConectadoFacturador` |

### frontend_tenant

| Elemento | Acción |
|----------|--------|
| `SunatConfig.tukifac_token_set` | Eliminado del tipo |

---

## 5. Grep report — referencias restantes (runtime)

```text
# Código fuente (.go, .ts, .tsx, .php en src/) — post-cleanup:

legacy_backend     → SOLO migraciones Doctrine (data cleanup one-shot)
invoicing_mode     → SOLO v039 tenant migration (DROP column list)

useLycet           → 0
Tukifac* structs   → 0
TUKIFAC_BASE_URL   → 0 en runtime (solo TUKIFAC_IMAGE docker deploy)
FISCAL_DECOUPLED   → 0 en código (docs históricos pendientes)
UBLGenerator       → 0
SyncEmpresa        → 0
EmitDirect         → 0
syncPSECredentials → 0
```

### Referencias intencionales conservadas (no dead code)

| Referencia | Motivo |
|------------|--------|
| `LycetResponseJSON` (columna BD) | Nombre de columna histórico; renombrar requiere migración |
| `en_lycet`, `ambiente_lycet` (JSON API) | Contrato API estable con frontend |
| Subcarpeta `"lycet"` en `saveInvoiceFile` | Paths de disco existentes |
| `Tukifac` en branding UI / dominios | Producto SaaS, no arquitectura fiscal |
| `pse_pass` columna + aliases en company-sync | Schema SSOT facturador (campo heredado) |
| Docs `FISCAL-*.md` antiguos | Documentación histórica (no runtime) |

---

## 6. Verificación

- [x] `go build ./...` — OK
- [x] `frontend_central` npm build — OK
- [x] Sin cambios en paths de emisión fiscal V2
- [x] `facturadorConfigured()` equivale semánticamente a `useLycet == true`

---

## Confirmación

**Zero dead fiscal code** en runtime: no quedan flags, adapters, DTOs Tukifac, ni rutas legacy de emisión en el código activo. Lo único con naming histórico son columnas/API JSON/storage paths que requieren migración aparte para renombrar sin romper datos en producción.
