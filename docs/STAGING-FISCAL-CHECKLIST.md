# Checklist validación fiscal — Staging

Usar este documento **antes de producción** con la arquitectura desacoplada (fases 1–4).

**Objetivo:** confirmar que ERP + facturador + Redis funcionan para al menos **2 tenants** (SUNAT directo y PSE), incluyendo resumen y guía.

---

## 0. Entorno staging mínimo

| Servicio | Requisito |
|----------|-----------|
| **backend_go** | `FISCAL_DECOUPLED=true`, Redis, `FACTURADOR_*`, `INTERNAL_API_KEY` |
| **facturador_lycet** | Migraciones OK, Redis, workers activos, certificado SOL por RUC |
| **Redis** | Compartido o instancias separadas; ping OK desde ambos servicios |
| **MySQL** | BD central + BD tenant de prueba |

### Variables críticas ERP (staging)

```env
FISCAL_DECOUPLED=true
FISCAL_QUEUE_WORKERS=4
FACTURADOR_BASE_URL=https://facturador-staging.tudominio.com
FACTURADOR_TOKEN=<igual que CLIENT_TOKEN en facturador>
INTERNAL_API_KEY=<clave compartida con facturador>
REDIS_URL=redis://...
ERP_WEBHOOK_URL=https://api-staging.tudominio.com/api/internal/fiscal/status
```

### Variables críticas facturador (staging)

```env
REDIS_URL=redis://...
ERP_WEBHOOK_URL=https://api-staging.tudominio.com/api/internal/fiscal/status
ERP_WEBHOOK_KEY=<mismo INTERNAL_API_KEY>
VALIDAPSE_BASE_URL=https://app.validapse.com
WKHTMLTOPDF_PATH=/usr/bin/wkhtmltopdf
MAILER_DSN=smtp://...
FISCAL_STORAGE_DRIVER=local   # o r2/s3 en staging multi-nodo
```

---

## 1. Pre-vuelo (Día 0)

Marcar cada ítem antes de emitir comprobantes reales.

### Infraestructura

- [ ] `GET /health` del ERP responde OK
- [ ] Facturador responde (p. ej. `GET /login` o health propio)
- [ ] Redis accesible: `redis-cli PING` desde VPS de ERP y facturador
- [ ] Migraciones facturador ejecutadas:
  ```bash
  php bin/console doctrine:migrations:migrate --no-interaction
  ```
- [ ] Workers facturador en ejecución (≥ 2 réplicas recomendado):
  ```bash
  php bin/console app:fiscal:worker
  ```
- [ ] ERP arrancado con `fiscalqueue` (logs: workers fiscales iniciados)

### Tenants de prueba

Crear **2 tenants** en staging:

| Tenant | Slug ejemplo | Modo facturación | Ambiente SUNAT |
|--------|--------------|------------------|----------------|
| **A** | `demo-sunat` | Legacy backend / SUNAT directo | Beta/pruebas |
| **B** | `demo-pse` | PSE (ValidaPSE) | Beta/pruebas |

- [ ] Tenant A: SUNAT habilitado, RUC, SOL, certificado, ubigeo fiscal
- [ ] Tenant B: PSE configurado (`pse_base_url`, `pse_token`), modo `pse`
- [ ] Certificados válidos para ambiente **pruebas** (no producción hasta checklist completo)

### Sincronización empresa → facturador

Por cada tenant:

- [ ] `POST /api/company/sync-facturador` (tenant) **o** `POST /api/superadmin/tenants/:id/sync-facturador`
- [ ] `GET /api/v1/empresas/{RUC}?token=...` en facturador devuelve la empresa
- [ ] Campos SaaS presentes en BD facturador (`empresa`): `tenant_id`, `tenant_slug`, `send_mode`, `provider`
- [ ] Tenant PSE: `send_mode=pse`, token PSE en `pse_pass`

---

## 2. Smoke tests automatizados

```bash
# Facturador — idempotencia (sin SUNAT)
php bin/console app:fiscal:stress-test --tenants=100 --per-tenant=5
# Esperado: created=100, deduped=400

# ERP — tests unitarios fiscal
go test ./pkg/fiscaldedup/... ./internal/billing/service/ -run 'Fiscal|UBL|Disabled' -count=1
```

- [ ] Stress test idempotencia OK
- [ ] Tests Go OK
- [ ] `go build ./...` OK en ERP

---

## 3. Tenant A — SUNAT directo

### 3.1 Boleta (03)

- [ ] Crear venta boleta en POS/ERP
- [ ] Emitir: `POST /api/billing/send/:saleId` (o flujo UI equivalente)
- [ ] Respuesta ERP **inmediata** (no espera SUNAT): estado `queued` / `FACTURADOR_RECEIVED`
- [ ] En facturador dashboard (`/login` → `/dashboard`): documento `queued` → `accepted`
- [ ] URLs: `xml_signed_url`, `cdr_url`, `pdf_url` (PDF si wkhtmltopdf OK)
- [ ] Webhook ERP aplicado: `tenant_invoices.pipeline_status` coherente
- [ ] **Doble clic:** reenviar misma venta → no duplica emisión (misma fingerprint / dedup)

### 3.2 Factura (01)

- [ ] Repetir flujo con cliente RUC
- [ ] CDR código `0` (aceptado) o rechazo documentado si datos inválidos

### 3.3 Nota de crédito (07)

- [ ] Anular venta / emitir NC según flujo ERP
- [ ] Documento aceptado en facturador y sincronizado en ERP

### 3.4 Guía de remisión (09)

- [ ] Crear y enviar guía (`CreateAndSendDespatch` o UI)
- [ ] Snapshot con `tipoDoc=09` o `_meta.document_kind=despatch`
- [ ] CDR + PDF generados en facturador

**Criterio éxito Tenant A:** ≥ 1 boleta + 1 factura + 1 NC + 1 guía aceptadas con evidencia (XML + CDR).

---

## 4. Tenant B — PSE (ValidaPSE)

- [ ] Confirmar `invoicing_mode=pse` en configuración tenant
- [ ] Emitir boleta → facturador usa `ValidaPseProvider` (`provider=validapse`)
- [ ] `unsigned_xml_url` presente (XML previo a firma PSE)
- [ ] `pse_response_json` registrado en `fiscal_documents`
- [ ] CDR aceptado vía PSE
- [ ] ERP **no** ejecuta `pseAdapter` legacy (sin errores UBL en logs)

**Criterio éxito Tenant B:** ≥ 2 comprobantes PSE aceptados con CDR.

---

## 5. Resumen diario (RC) y baja (RA)

> Documentos **asíncronos**: emiten ticket → cola `fiscal:status_poll` → CDR.

### Resumen (RC)

- [ ] Enviar resumen diario desde ERP (flujo existente → snapshot fiscal async)
- [ ] Estado intermedio `sent` + `ticket` en facturador
- [ ] Tras poll (30s–5min): `accepted` + CDR
- [ ] Webhook ERP actualiza estado final

### Comunicación de baja (RA)

- [ ] Enviar baja de comprobante de prueba
- [ ] Ticket → poll → aceptado
- [ ] Sin duplicar baja en reintento

**Criterio éxito:** 1 resumen + 1 baja aceptados en staging.

---

## 6. Correo fiscal

- [ ] Cliente con email en snapshot / contacto
- [ ] Tras `accepted`: cola `fiscal:email` procesada
- [ ] Registro en `outbound_email_logs` con `status=sent`
- [ ] Correo recibido (PDF/XML links o adjuntos locales)
- [ ] Venta sin email → `email_status=skipped` (no falla emisión)

---

## 7. Webhook ERP (multi-réplica)

- [ ] Header `X-Fiscal-Signature` presente (HMAC)
- [ ] Reenviar mismo `event_id` → respuesta `{ deduplicated: true }` (Redis dedup)
- [ ] Reiniciar 2 réplicas ERP y repetir webhook → sigue deduplicando

---

## 8. Resiliencia y errores

| Escenario | Acción | Esperado |
|-----------|--------|----------|
| SUNAT caído / timeout | Emitir boleta | `retrying`, reintento exponencial, máx 5 |
| PSE token inválido | Emitir en tenant B | `rejected` o `error`, mensaje claro, sin crash worker |
| Webhook ERP caído | Aceptar doc en facturador | Cola `fiscal:webhook_sync` reintenta |
| Worker facturador detenido | Encolar emisión | Documento `queued` hasta levantar worker |
| Redis caído ERP | Webhook duplicado | Fallback in-memory dedup (aceptable solo staging single-node) |

- [ ] Al menos 1 escenario de retry probado y documentado
- [ ] Logs revisados: sin panic, sin loop infinito

---

## 9. Storage y dashboard

- [ ] Archivos accesibles vía URLs públicas / `GET /fiscal-files/...`
- [ ] Si `FISCAL_STORAGE_DRIVER=r2`: objetos visibles en bucket/CDN
- [ ] Dashboard facturador filtra por `tenant_slug` y `status`
- [ ] API: `GET /api/v1/fiscal/documents?tenant_slug=...&status=accepted`

---

## 10. Concurrencia

- [ ] 10 emisiones simultáneas **mismo tenant** (distintas ventas) → todas procesadas
- [ ] 5 emisiones simultáneas **misma venta** (doble clic) → 1 aceptada, resto deduplicadas
- [ ] Métricas ERP: `FiscalQueueEnqueued` / `FiscalQueueProcessed` incrementan

---

## 11. Seguridad staging

- [ ] `INTERNAL_API_KEY` definido (no vacío en staging público)
- [ ] Webhook `/api/internal/fiscal/status` rechaza requests sin auth en prod-like env
- [ ] Tokens facturador no expuestos en frontend
- [ ] Listado empresas facturador no expone `SOL_PASS` en claro (enmascarado)

---

## 12. Criterios GO / NO-GO producción

### GO (todos obligatorios)

- [ ] Tenant SUNAT: boleta + factura aceptadas
- [ ] Tenant PSE: ≥ 2 comprobantes aceptados
- [ ] Resumen + baja: ticket → poll → accepted
- [ ] Guía remisión aceptada
- [ ] Idempotencia stress test OK
- [ ] Webhook dedup OK
- [ ] Workers estables ≥ 24h en staging sin memory leak evidente
- [ ] Plan rollback documentado (ver abajo)

### NO-GO (cualquiera bloquea)

- Duplicados SUNAT por doble emisión
- ERP genera XML/UBL con `FISCAL_DECOUPLED=true`
- PSE procesado en ERP en lugar de facturador
- Webhooks perdidos sin entrada en `fiscal:webhook_sync`
- Credenciales producción usadas en pruebas beta

---

## 13. Rollback rápido

Si staging falla de forma crítica:

1. **ERP:** `FISCAL_DECOUPLED=false` (solo si aún existe flujo legacy operativo)
2. Detener workers facturador
3. Drain colas Redis: `fiscal:emit`, `tukifac:fiscal:queue`
4. Restaurar snapshot BD staging si hubo datos corruptos

> En arquitectura fase 4 el rollback a legacy **no es recomendado** salvo emergencia; preferir fix forward.

---

## 14. Orden sugerido de ejecución (1 sesión ~2–3 h)

1. Pre-vuelo (§1) + smoke (§2)
2. Sync empresas (§1)
3. Tenant A boleta → factura → NC → guía (§3)
4. Tenant B PSE (§4)
5. Resumen + baja (§5)
6. Email + webhook dedup (§6–7)
7. Concurrencia (§10)
8. Revisión GO/NO-GO (§12)

---

## 15. Registro de ejecución

| Fecha | Ejecutor | Tenant SUNAT | Tenant PSE | Resumen/Baja | Guía | Email | GO/NO-GO | Notas |
|-------|----------|--------------|------------|--------------|------|-------|----------|-------|
| | | ☐ | ☐ | ☐ | ☐ | ☐ | | |
| | | ☐ | ☐ | ☐ | ☐ | ☐ | | |

---

## Referencias

- [ARQUITECTURA-FISCAL.md](./ARQUITECTURA-FISCAL.md)
- [PRUEBAS-FACTURACION.md](../PRUEBAS-FACTURACION.md)
- Facturador dashboard: `GET /login` (usuario admin seed)
- ERP webhook: `POST /api/internal/fiscal/status`
