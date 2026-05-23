# Reporte de dependencias — transición Staff v2 definitivo

Generado antes del cleanup legacy. **No eliminar tablas** hasta completar V036 y validar datos.

## 1. `tenant_waiters` — ELIMINAR como entidad operativa

| Dependencia | Archivos | Acción V036+ |
|-------------|----------|--------------|
| CRUD API | `routes.go` L46-49, `restaurant_handler.go`, `restaurant_service.go` L305-327 | **Eliminar** endpoints; sustituir por `GET /staff` |
| FK sesiones | `tenant_table_sessions.waiter_id` | **Migrar** → `staff_id` vía `legacy_waiter_id` |
| FK pedidos | `tenant_table_orders.waiter_id` | **Migrar** → `staff_id` |
| JOIN listado mesas | `restaurant_service.go` L253 | **Cambiar** JOIN `tenant_restaurant_staff` |
| Nombre mozo sesión | `restaurant_service.go` L374-376, `restaurant_order_service.go` L266-268 | **Cambiar** a `display_name` staff |
| UI dropdown | `SalasPage.tsx` L17-51, L243-253 | **Eliminar**; auto-asignar staff autenticado |
| Panel CRUD | `RestaurantWaitersPage.tsx`, `ModulesPage.tsx`, `App.tsx` ruta waiters | **Eliminar** página; gestión en Usuarios |
| Docs API | `api-restaurant.md`, `api-tenant.md` | **Actualizar** |

**Riesgo datos:** mozos sin `user_id` requieren usuario sintético en migración V036 (email `waiter-{id}@internal.tukichef`).

## 2. `tenant_user_restaurant_roles` — ELIMINAR

| Dependencia | Archivos | Acción |
|-------------|----------|--------|
| Modelo | `migrations.go` L1142-1150 | Mantener tabla histórica; dejar de escribir |
| Backfill V035 | `v035_restaurant_staff.go` | Conservar migración histórica |
| Get/Set role | `restaurant_service.go` L44-99 | **Eliminar** |
| Endpoints | `routes.go` L23-26, `restaurant_handler.go` L34-64 | **Eliminar** |
| Sync staff | `staff/service.go` `syncLegacyRestaurantRole` | **Eliminar** |
| Permisos fallback | `staff/service.go` L87-93 | **Eliminar** |
| Panel tenant | `UsersPage.tsx` `setUserRestaurantRole` | **Solo** `PUT /users/:id/staff` |
| V036 migración | Nueva | Copiar roles → staff si falta fila |

## 3. `restaurant_role` / `RequireRestaurantRole` — ELIMINAR

| Dependencia | Archivos | Acción |
|-------------|----------|--------|
| JWT claim | `middleware/auth.go` L28, L157 | **Reemplazar** por `employee_type` |
| Middleware rutas | `routes.go` L15-71 | **Reemplazar** por `RequireRestaurantPerm` |
| `RequireRestaurantRole` | `auth.go` L247-272 | **Eliminar** |
| Login body | `auth_handler.go` L257,289 | **Eliminar**; enviar `employee_type` |
| PIN login | `staff_auth_handler.go` | JWT con `employee_type` |
| Session perms API | `staff_auth_handler.go` L157 | Quitar `restaurant_role` |
| Frontend RequireAuth | `App.tsx` L40 | Validar `permissions.length > 0` |
| Frontend labels | `UserDropdown.tsx` ROLE_LABELS | Map `employee_type` |

## 4. `staff_v2_enabled` flag — ELIMINAR (siempre activo)

| Archivo | Acción |
|---------|--------|
| `staff/service.go` checks `staffV2` | Remover condicionales |
| `staff_handler.go` SetStaffV2Enabled | Deprecar o fijar `true` |
| `RestaurantSettingsPage.tsx` toggle | **Eliminar** UI |
| `AuthContext.tsx` `staffV2Enabled` | **Eliminar** |
| `HomePage.tsx` `pin_login_enabled` | Siempre true si hay PINs |
| V036 | `UPDATE tenant_restaurant_settings SET staff_v2_enabled = 1` |

## 5. Frontend `canAccess` legacy — ELIMINAR

| Archivo | Acción |
|---------|--------|
| `AuthContext.tsx` `legacyCanAccess` | **Eliminar** |
| `restaurantPermissions.ts` fallback sin permisos | Denegar si array vacío |
| `App.tsx` `DefaultRedirect` | Solo permisos |

## 6. Código muerto confirmado

- `staff/service.go` `LinkLegacyWaiter` — 0 callers
- `staff/service.go` `EffectiveLegacyRole` — 0 callers externos
- `GET /api/restaurant/me` — solo expone JWT legacy
- `restaurantperm.LegacyRoleToKeys` — tras eliminar fallback

## 7. Mantener (arquitectura final)

- `tenant_users` — identidad
- `tenant_restaurant_staff` — perfil operativo
- `tenant_restaurant_settings` — PIN anulación, `perm_cache_version`
- `pkg/restaurantperm` — claves cortas + cache
- `/api/restaurant/auth/config`, `/auth/pin`
- `/api/restaurant/session/permissions`
- `/api/restaurant/staff`, `PUT /users/:id/staff`
- JWT: `user_id`, `staff_id`, `employee_type`, `perm_ver`, `am`
- Home + PIN + auditoría `user_id` en sesiones/pedidos

## 8. Orden de ejecución seguro

1. Desplegar V036 (datos + columnas `staff_id`)
2. Desplegar backend Staff-only
3. Desplegar frontends
4. Monitorear; **no** `DROP tenant_waiters` hasta ciclo sin referencias (opcional V037)
