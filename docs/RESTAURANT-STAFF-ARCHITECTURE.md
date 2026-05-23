# Arquitectura Staff — Restaurante (definitiva)

Staff v2 es el **único** modelo operativo. No hay roles legacy (`vendedor`/`mozo` en JWT) ni catálogo `tenant_waiters`.

## Identidad y perfil

| Capa | Tabla | Uso |
|------|-------|-----|
| Identidad | `tenant_users` | Login email/password |
| Perfil operativo | `tenant_restaurant_staff` | Tipo empleado, PIN, permisos granulares |
| Config | `tenant_restaurant_settings` | PIN anulación, `perm_cache_version` |

Tipos: `admin`, `supervisor`, `cashier`, `waiter`, `cook`, `driver`.

## Sesiones y auditoría

- `tenant_table_sessions.staff_id` — empleado que atiende la mesa
- `tenant_table_orders.staff_id` — empleado por ronda (hereda de sesión)
- `user_id` — usuario autenticado que ejecutó la acción

Al abrir mesa: **auto-asigna** el `staff_id` del usuario logueado. Solo quien tiene permiso `s.m` puede reasignar.

## Permisos (claves cortas + cache Redis)

Ver `pkg/restaurantperm/keys.go`. Rutas protegidas con `RequireRestaurantPerm`, no roles hardcoded.

| Permiso | Uso típico |
|---------|------------|
| `t.v` | Ver salas/mesas, listar pisos |
| `t.o` | Abrir mesa / sesión |
| `o.c` | Agregar ítems al pedido (carta vía `RequireProductsViewOrRestaurantCatalog`) |
| `g.p` | Configurar pisos/mesas/productos (panel admin restaurante) |

Los permisos del **rol tenant** (`products.view`, etc.) aplican al panel del tenant; en Tukichef rige el perfil `tenant_restaurant_staff`.

## Auth

| Ruta | Descripción |
|------|-------------|
| `GET /api/restaurant/auth/config` | Home ligera (PIN habilitado si hay PINs) |
| `POST /api/restaurant/auth/pin` | JWT liviano (`employee_type`, `staff_id`, `perm_ver`) |
| `POST /api/login` | Incluye `restaurant_permissions` en body |
| `GET /api/restaurant/session/permissions` | Refresco de permisos |

## Gestión (panel tenant)

- `PUT /api/restaurant/users/:id/staff` — tipo + PIN
- `GET /api/restaurant/staff` — listado

## Migración V036

- `tenant_waiters` → `tenant_restaurant_staff` (con `legacy_waiter_id`)
- `waiter_id` en sesiones/pedidos → `staff_id`
- `staff_v2_enabled = 1` en todos los tenants

```bash
go run ./pkg/cmd migrate-bump-target
go run ./pkg/cmd migrate-fleet
go run ./pkg/cmd migrate-backfill-fleet --version=36
```

## Deprecado (no usar)

- `tenant_waiters`, `/api/restaurant/waiters`
- `tenant_user_restaurant_roles`, `/restaurant-role`
- `RequireRestaurantRole`, `restaurant_role` en JWT
- Flag `staff_v2_enabled` (siempre activo tras V036)

Ver `docs/STAFF-V2-DEPENDENCY-REPORT.md` para inventario de limpieza.
