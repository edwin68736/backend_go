# Despliegue — Documentos fiscales, Operaciones fiscales y resumen por tenant (2026-10-06)

Tres repos dependen entre sí. Nada de esto se ha subido: el usuario hace el `git push`.

## Qué cambia en cada repo

| Repo | Cambios |
|---|---|
| `facturador_lycet` (PHP) | Vistas `pending/needs_action/processing/history/accepted/attended` y `stats.views`; atendidos/aceptados fuera de pendientes; fix de filtros de email; alertas con reconocer/resolver y auto-cierre; cola con "atascados"; `FiscalClock` (hora de Perú, ver `facturador_lycet/docs/ZONA-HORARIA.md`); cancelar deja el documento atendido |
| `backend_principal` (Go) | Tablas centrales `tenant_fiscal_daily` y `tenant_fiscal_health`; job del resumen cada 15 min; `GET/POST /superadmin/fiscal/tenant-summary…`; proxy para reconocer/resolver alertas; migración de tenant **v155** (índice en `tenant_sales.billing_status`) |
| `frontend_central` | Documentos fiscales (Pendientes/Historial), Operaciones (franja de estado, alertas, cola en vivo, "Comprobantes por tenant") |

## Orden recomendado

1. **`migrate-central`** (el workflow de deploy ya lo corre antes del restart): crea `tenant_fiscal_daily` y `tenant_fiscal_health`. Si el backend arranca antes, el job no hace nada y el endpoint responde 503 con un mensaje claro; no rompe nada.
2. **Facturador** (`facturador_lycet`): no tiene migraciones de BD ni de configuración nueva. Es retrocompatible con el panel viejo salvo dos detalles: la cola ya no devuelve el grupo `failed` en `counts` (devuelve `needs_action`) y las fechas salen con offset `-05:00`.
3. **Backend principal**: despliegue normal. El primer ciclo del job hace una **pasada profunda** (historial completo de los ~600 tenants, solo lectura) porque la tabla de salud está vacía; revisar en los logs la línea `fiscal_snapshot_done` (tenants, fallidos, `deep=true`, `ms`) y la siguiente (`deep=false`) para ajustar el intervalo.
4. **Frontend central**: al final, cuando el facturador y el backend ya responden lo nuevo.
5. **Fleet** (`migrate-fleet-cron`, lo hace el cron cada 5 min o a mano): aplicará v155 (índice). En el VPS el cron está documentado como roto desde el 09-sep; correrlo a mano:
   `docker exec tukifac-backend-go ./tukifac-api migrate-fleet-cron --workers=4 --limit=400 --active-only=false`.
   El índice no cambia datos; hasta que se aplique todo funciona igual, solo más lento en tenants grandes.

Pendientes de despliegue de sesiones anteriores que el mismo fleet aplicará a la vez: idempotencia del cobro (v150), cotizaciones (v151) y referencia de pago de cotizaciones (v154). Conviene revisar que el orden de migrate-central → imagen → fleet no deje una ventana con columnas faltantes (incidente del 09-sep).

## Permisos del panel central (verificado en producción, solo lectura, 2026-10-06)

| Rol | Usuarios | Permisos fiscales actuales |
|---|---|---|
| Admin | 1 | bulk, cancel, retry, view |
| Gestión | 1 | bulk, retry, view |
| Apoyo Gestion | 1 | bulk, retry, view |
| Soporte / Finanzas / Ventas | 1 / 0 / 1 | ninguno |

- **`fiscal.attend` existe en el catálogo pero NINGÚN rol lo tiene.** Solo el `superadmin` (bypass) puede hoy "Marcar atendido" y, con lo nuevo, **Reconocer/Resolver alertas**. Los roles Admin, Gestión y Apoyo Gestion recibirían un 403.
- **Reenviar pendientes** usa `fiscal.retry`: lo tienen Admin, Gestión y Apoyo Gestion.
- Decisión pendiente (cambia datos de producción, no se hizo): otorgar `fiscal.attend` a los roles que ya tienen `fiscal.retry`, desde Roles en el panel o con un backfill central.

## Verificación después del deploy

- `GET /api/superadmin/fiscal/tenant-summary` responde 200 y trae los tenants; la columna "Verificado" no supera ~15 min.
- Operaciones: "Pendientes ahora" ya no se limita a lo creado hoy; las horas de la gráfica y las fechas coinciden con la hora de Lima.
- Una alerta de prueba (o real) se puede reconocer y resolver con un usuario con `fiscal.attend`.
- Logs del backend: ninguna línea `fiscal_snapshot_tenant_failed` repetida para el mismo tenant (indica esquema viejo o timeout).
