# Informe de despliegue — aislamiento por sucursal (10-nov-2026)

**Nada está pusheado ni desplegado.** Este documento resume lo que hay que subir, en qué orden, cómo comprobarlo y cómo volver atrás.

## 1. Commits por repositorio (sin push)

### backend_principal (16 commits)
`22a70bf` V175 cuentas contables · `4319b24` prueba de suscripciones · `b505872` auditoría y diseño ·
`23fc0cb` **3A** cierre de fugas · `dbcd79f` **3B** permisos `branches.*`, BranchContext, modo ALL, V176 ·
`8163b69` **3C** scope GORM · `9a604f5` endurecer `ResolveWriteBranchID` · `cdeb86e` `products.manage_global` + V177 ·
`2115c5a` informe `branch_id=0` · `3605148` fix guardas fiscales fuera del scope · `c14f85f` fix reglas globales (serie única, contactos) ·
`9265814` fix transferencia con series bajo scope · `b1a6039` docs deuda técnica y ecommerce · `e08f8d0` fix scope con `Table("x alias")` ·
`cd3a058` "ver todas" por BD y no por JWT · `2bd295f` CORS `X-Branch-Mode`.

### frontend_tenant — Tukifac (8)
`d463947` `ea95e42` `3547059` `f271eee` `8a08dba` (lote previo: Contabilidad en Ajustes, desglose de comprobantes, arreglos) ·
`8357629` **Fase 4** cambio de sucursal sin F5 · `ce1d892` etiquetas `branches.*` · `4966863` modo "Todas", formularios fijos a la activa, catálogo global.

### frontend_restaurant_tenant — Tukichef (5)
`56233d6` `35d623d` `e30c3b7` (lote previo: stock en tablet, desglose de comprobantes, versión 1.4.7) ·
`51030e6` **Fase 4** cambio de sucursal sin F5 · `ebbcaae` modo "Todas" y catálogo global.

Tamaño: backend 104 archivos (+9100/−159), Tukifac 102 (+3978/−337), Tukichef 54 (+3641/−167).

## 2. Migraciones de tenant pendientes (aditivas, idempotentes, sin borrar datos)
Producción estaba en **V171** (5-nov). Pendientes: **V172** (limpia contacto "Clientes Varios" de empresa) · **V173** (permisos finos de Caja) ·
**V174** (módulo Contabilidad) · **V175** (cuentas contables por defecto) · **V176** (permisos `branches.view_all|switch|manage`; Administrador los recibe;
roles con usuarios `can_switch_branch` reciben `switch`; usuarios sin asignación reciben su sucursal de origen o la principal) ·
**V177** (permiso `products.manage_global`; lo reciben los roles que ya tenían `products.create|edit|delete|manage`).
No hay migración de ecommerce (solo diseño).

## 3. Pruebas ejecutadas (resultados reales)
- **Backend:** `go build ./...` y `go test ./... -count=1`: 92 paquetes ok, 0 fallos (última corrida tras el commit `2bd295f`).
  Tests nuevos: aislamiento por módulo (3A), permisos/middleware/V176/anti-escalada (3B), `pkg/branchscope` (3C, incl. alias, estricto, transacciones),
  guardas bajo scope, reglas globales, transferencia con series, V177, `RequireGlobalCatalog`, CORS.
  Cada fix posterior a 3C se reprodujo con un test que **fallaba antes** del arreglo.
- **Tukifac:** `tsc --noEmit` 0 · `vitest` 9 archivos / 54 tests ok · `vite build` ok.
- **Tukichef:** `tsc -p tsconfig.app.json --noEmit` 0 · `vitest` 6 archivos / 53 tests ok · `vite build` ok.
- **En vivo (backend local + MySQL, tenant demo):** POS con carrito → confirmación → cancelar/descartar; carrito no se cobra en otra sucursal;
  Tukichef con mesa abierta → confirmación → descarta, redirige y el servidor rechaza operar la sesión anterior (403); login real de usuario con 2 sucursales
  y selector; cambio sin F5 en ambas apps; modo "Todas" en ambas apps (banner, datos consolidados 241 vs 235, escrituras 403 `BRANCH_READ_ONLY`, POS bloqueado,
  salida del modo); 35 endpoints con vendedor y administrador sin 500; log de auditoría sin accesos sin contexto en solicitudes.
- **No probado:** Android y Tauri (sin entorno), diálogo nativo `window.confirm` en el navegador integrado (suprimido; probada la lógica y el texto),
  reportes uno a uno en modo "Todas", flujos de escritura reales de ConvertOrder/Despatch/Retención/Nota independiente con sucursal fija,
  concurrencia de stock, producción real.

## 4. Orden exacto de despliegue
1. **Backup** de la BD central y de una muestra de tenants (o snapshot del VPS de BD).
2. **Backend** (imagen nueva). Compatible con tenants sin V176/V177: conserva la regla por rol "Administrador", no bloquea usuarios sin sucursal y no restringe el catálogo global.
   `BRANCH_SCOPE_STRICT` **sin definir (apagado)**. Verificar `/health` y el log (`[branchscope]`).
3. **`migrate-fleet`** V172→V177 (V173 antes del frontend web de Tukifac). Verificar por muestreo: permisos `branches.*` en el rol Administrador,
   usuarios con fila en `tenant_user_branches`, `products.manage_global` en roles que editaban productos.
4. **Frontend web de Tukifac** (después de V173, V176 y V177).
5. **Apps Tukichef** (web/Tauri/Android 1.4.7): pueden salir cuando el backend esté arriba; el modo "Todas" y el catálogo global requieren V176/V177.
6. **Vigilancia** (varios días): log `[branchscope] AUDITORÍA`; errores 403 `BRANCH_FORBIDDEN`/`BRANCH_READ_ONLY`; quejas de roles que perdieron acciones.
7. **Modo estricto:** solo después de revisar la auditoría, anotar con `System` lo legítimo y completar pruebas de integración (ver DEUDA_TECNICA.md).

## 5. Riesgos conocidos
- **Scope GORM global (3C):** ya rompió 4 flujos que cruzan sucursales (corregidos). Pueden existir más: vigilar auditoría y errores de stock/series/transferencias.
  Límites documentados: `Raw()`/`Exec()` y JOIN no pasan por callbacks (FASE_3C.md, DEUDA_TECNICA.md §5).
- **Escrituras de administradores endurecidas:** clientes viejos (apps Tukichef/Tukifac anteriores) que envíen un `branch_id` distinto de la sucursal activa recibirán 403.
- **Permisos en el JWT** (`products.manage_global`, módulos): un cambio de permisos de rol aplica al renovar el token (hasta 24 h). "Ver todas" ya sí se decide por BD (10 s).
- **Token en la URL del SSE, revocación de sesión, emisor único de JWT, vigencia del token PIN:** sin resolver (DEUDA_TECNICA.md); no empeoran con estos cambios, salvo que cambiar de sucursal renueva 24 h.
- **Roles personalizados con `company.edit`** pierden crear/eliminar sucursales sin `branches.manage`.
- **Ecommerce:** la tienda pública sigue exponiendo `stock_by_branch` de todas las sucursales (hotfix E0 propuesto, no aplicado).
- **Retenciones/percepciones** se crean sin `branch_id` (5 filas en 3 tenants; informe aparte, sin reasignar).
- **`doriconta`** tiene dos sucursales marcadas principales.

## 6. Rollback
- **Interruptor inmediato sin redeploy de código:** variable `BRANCH_SCOPE_DISABLED=1` y reiniciar → apaga los callbacks de 3C (el resto de guardas siguen).
- **Backend:** volver a la imagen anterior. Las migraciones V172–V177 son aditivas y compatibles hacia atrás (solo agregan permisos y asignaciones); no hace falta revertirlas.
  El backend anterior ignora los permisos nuevos y los registros añadidos.
- **Frontends:** redeploy del build anterior (web) o versión anterior de las apps; los frontends antiguos funcionan con el backend nuevo salvo el punto de escrituras endurecidas.
- **Datos:** no hay migraciones destructivas ni reasignaciones; si V176 asignó una sucursal incorrecta a un usuario, se corrige desde Usuarios. Restaurar desde el backup del paso 1 solo ante corrupción.
