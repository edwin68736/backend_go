# Caja B+ — Sesiones por usuario (preparado para Fase C)

## Decisión arquitectónica

| Regla | Descripción |
|-------|-------------|
| **Por sucursal** | Varias sesiones abiertas simultáneas en la misma `branch_id` |
| **Por usuario** | Máximo **1** sesión `open` por `(branch_id, user_id)` |
| **Cobros** | Siempre contra la sesión del **usuario autenticado** |
| **Prohibido** | `getOpenSession(branch)` sin `user_id` para operaciones de cobro |

### Fase C (futuro, sin implementar CRUD)

Columnas nullable en `tenant_cash_sessions`:

- `register_code` — ej. `principal`, `barra`, `delivery`
- `register_name` — etiqueta humana

Cuando exista `tenant_cash_registers`, la restricción pasará a **1 sesión abierta por `(register_id)`**.

---

## Modelo actual vs objetivo

```
ANTES                          DESPUÉS (B+)
─────────────────────────────────────────────────
1 open / branch                N open / branch (1 por user)
GetOpenSession(branch)         GetOpenSession(branch, userID)
Cobro → primera caja abierta   Cobro → caja del user logueado
```

---

## Cambios BD (V038)

| Columna | Tipo | Notas |
|---------|------|-------|
| `register_code` | `VARCHAR(50) NULL` | Fase C |
| `register_name` | `VARCHAR(100) NULL` | Fase C |

Sin índice único parcial en MySQL (validación en aplicación). Opcional índice `(branch_id, status, user_id)` para listados.

---

## Cambios API

| Método | Ruta | Cambio |
|--------|------|--------|
| GET | `/api/cashbank/sessions/open` | Sesión abierta del **usuario actual** en sucursal activa |
| GET | `/api/cashbank/sessions/open/list` | **Nuevo**: todas las sesiones abiertas de la sucursal (solo lectura) |
| POST | `/api/cashbank/sessions` | Valida `(branch_id, user_id)` sin otra `open` |
| POST | `.../close`, `.../movements` | Solo sesión propia (o admin tenant / `s.m` restaurante) |
| POST | `/api/restaurant/sessions/:id/bill` | Resuelve/valida `cash_session_id`; bloquea efectivo a `waiter` |

### Acceso restaurante (PIN)

Rutas de sesión de caja aceptan:

- Permisos tenant `cashbank.*`, **o**
- Staff restaurante con `c.v` (`CashView`) vía `LoadRestaurantPermissions`

---

## Validaciones obligatorias

`ValidateCashSessionForUser(sessionID, userID, branchID)`:

1. Sesión existe
2. `status = open`
3. `user_id` (o `opened_by`) = usuario actual
4. `branch_id` = sucursal activa

`RecordPayment` / `BillTable` / ventas:

- Si pago `destination_type=cash` → sesión obligatoria y validada
- No aceptar `cash_session_id` de otro usuario
- Waiter + efectivo → **403**

---

## Compatibilidad y migración

| Escenario | Comportamiento |
|-----------|----------------|
| Tenants con 1 caja abierta legacy | Sigue funcionando; es la del usuario que la abrió |
| Segundo usuario en misma sucursal | Puede abrir **su** caja (antes: error) |
| Frontends antiguos sin `user` en getOpenSession | API usa `userID` del JWT automáticamente |
| `cash_session_id` enviado de otro user | Backend rechaza |

**Sin backfill** de datos: las sesiones abiertas existentes ya tienen `user_id`.

---

## Riesgos

| Riesgo | Mitigación |
|--------|------------|
| PIN sin permiso tenant `cashbank.*` | Middleware híbrido + `c.v` |
| Dos pestañas abren caja doble | Error claro en `OpenSession` |
| Mozo cobra efectivo por API directa | Validación en `BillTable` + front |
| Reportes mezclan usuarios | Filtros `user_id`, `session_id` en reportes |
| 10k tenants | Sin locks globales; índice por branch+status; validación por fila |

---

## Paneles

### Tenant — Caja y bancos

- `activeBranchId` real (fix `branch_id: 1`)
- Sección **Mi caja** + listado **Cajas abiertas en sucursal**
- Ventas/POS tenant: `cash_session_id` de sesión propia

### Restaurante

- Contexto `CashSessionProvider`
- Modal **Abrir mi caja** (cashier/admin sin sesión)
- Header: estado de caja del usuario
- POS/Mesa: `getOpenSession(branch, self)`; waiter sin efectivo

---

## Listo para Fase C

- Columnas `register_*` en modelo y API JSON
- `OpenSession` aceptará `register_code` opcional sin romper B+
- Listado por sucursal ya separado por `user_id` → migración a “punto de caja” = agrupar por `register_code`
