# Informe consolidado para decidir la ventana de despliegue

Fecha: 10-nov-2026. **Nada está pusheado ni desplegado. `BRANCH_SCOPE_STRICT` apagado. No se tocó producción** salvo consultas de solo lectura.
Cuatro repos con commits locales: backend 31 · Tukifac 10 · Tukichef 6 · frontend_central 1.

## 1. Entregas independientes (commits no mezclados)

### A. Sucursales / sesión / acceso (backend)
`23fc0cb` 3A fugas · `dbcd79f` 3B permisos+V176 · `8163b69` 3C scope GORM · `9a604f5` escrituras del admin · `cdeb86e` `products.manage_global`+V177 ·
`3605148` `c14f85f` `9265814` `e08f8d0` `cd3a058` `2bd295f` (6 correcciones halladas en la verificación) · `4f2be24` emisor JWT · `7c8141a` versión de sesión V178+SSE ·
`fb1e0a3` acceso maestro con código · `e019ca5` principal única y huérfanos V179 · docs: `b505872` `2115c5a` `b1a6039` `8232f41` `5422676` `088123f` `36d1f48` `60762f5` `ea463a2` `a427c51`.
Frontends: Tukifac `8357629` `ce1d892` `4966863` `509dba1` `4368931` · Tukichef `51030e6` `ebbcaae` `1cdad54` · central `e9fcc58`.

### B. Fiscal (independiente, solo backend)
`c7703a9` fechas de retención/percepción en el formato del facturador · `999eb9c` procedimiento de verificación del JMS (docs). **No depende de nada de sucursales.**

### C. Ecommerce
`7aaecb6` hotfix E0 (API pública sin stock por sucursal ni series) · `f81a3e4` (docs). **E1 solo propuesta** (`docs/aislamiento-sucursal/PROPUESTA_E1_ECOMMERCE.md`), sin implementar.

### D. Lote previo no relacionado (ya existente antes de esta iniciativa)
Backend `22a70bf` `4319b24` (V175, prueba de suscripciones); Tukifac `d463947` `ea95e42` `3547059` `f271eee` `8a08dba`; Tukichef `56233d6` `35d623d` `e30c3b7` (versión 1.4.7).

## 2. Migraciones de tenant pendientes en producción (prod en V171)
V172 limpia contacto "Clientes Varios" · V173 permisos finos de Caja · V174 módulo Contabilidad · V175 cuentas contables por defecto · **V176** permisos `branches.*` y asignación de sucursal · **V177** `products.manage_global` · **V178** versión de sesión 0→1 · **V179** principal única y huérfanos → principal.
Todas aditivas e idempotentes. **Validadas en MySQL 8.0.46 (misma versión que producción) con el esquema real de producción (solo DDL) y datos sintéticos: 48 aplicaciones (6 tenants × 8 migraciones), 0 errores, idempotentes, ≈160 ms por tenant**; V179 además en 5 tenants-tipo con resultados idénticos en MariaDB.
Límite: los *backfills* de permisos (V173/V176/V177) no se ejercitaron con roles reales (el clon es solo esquema).

## 3. Dry-run de V179 en producción (788 tenants, solo lectura)
785 tenants no cambian. `doriconta`: dos principales (conserva la 1; ⚠ confirmar). `bernapom` y `naturales`: sin principal (V179 marca la sucursal 1; ⚠ confirmar `naturales`). 5 huérfanos (retenciones de `doriconta`, `industrialrafaz`×3, `col`) → principal; 0 conflictos únicos. Detalle: `INFORME_V179_DRYRUN.md`, `REPORTE_CONFIRMACION_PRINCIPALES.md`.

## 4. Pruebas ejecutadas (resultados reales)
| Área | Pruebas | Resultado |
|---|---|---|
| Backend | `go build`, `go vet` y `go test ./... -count=1` (95+ paquetes) tras cada entrega | 0 fallos en la última corrida (c7703a9) |
| Fiscal | unitarias + integración del payload + **contrato contra el JMS/Greenter reales (PHP 8.2, sin red)** + regresión que falla con el código anterior | todo en verde |
| V179 / cadena | MySQL 8.0.46 real | ver §2 |
| Tukifac | `tsc`, 98 tests, `vite build` | verde |
| Tukichef | `tsc -p tsconfig.app.json`, 86 tests, build | verde |
| frontend_central | 129 tests, build (su `tsc` de proyecto ya traía ~521 errores de alias antes de estos cambios) | verde |
| En vivo (backend local+MySQL, demo) | cambio de sucursal con carrito/mesa y confirmación; usuario con 2 sucursales; modo "Todas" en ambas apps; SSE por cabecera; 409→refresh sin cerrar sesión; acceso maestro con código; API pública sin stock por sucursal; 35 endpoints sin 500 | OK |
| **Modo "Todas" vs una sucursal (API, 26 endpoints de listados/reportes/dashboard/contabilidad)** | `X-Branch-Mode: all` vs sucursal activa | **todos 200**; consolidado ≥ una sucursal donde difieren (ventas 235→241, movimientos de caja 255→268, sesiones de caja 8→12, movimientos de inventario 624→632); contabilidad y membresías 403 por módulo no incluido en el plan demo |
| Producción (solo lectura) | colas de facturación: Redis `billing`/`fiscal` con 0 elementos; 5 retenciones `FAILED` sin reintento programado | OK; cola propia del facturador **no verificada** |

## 5. Riesgos pendientes
1. **Scope GORM (3C)** ya rompió 4 flujos que cruzan sucursales (corregidos con test que reprodujo cada fallo); pueden quedar otros: vigilar `[branchscope] AUDITORÍA` y 403 `BRANCH_FORBIDDEN`/`BRANCH_READ_ONLY`. `Raw()`/`Exec()`/JOIN no pasan por los callbacks (documentado). **STRICT apagado** hasta revisar auditoría y esas consultas.
2. **Clientes antiguos**: una app que mande un `branch_id` distinto de la sucursal activa en una operación recibe 403.
3. **Expulsión por revocación**: cambiar rol/permisos/contraseña sube la versión de sesión; los frontends nuevos refrescan, los antiguos (y los PIN sin refresh) obligan a volver a entrar. → no editar roles/permisos/contraseñas hasta que los frontends nuevos estén arriba.
4. **Redis necesario en producción** para el acceso maestro con varias instancias (existe `tukifac-redis`).
5. **Fiscal**: el facturador del VPS debe usar el mismo JMS (3.32.7); verificación pendiente de acceso (§6.1). Retenciones ya encoladas en el facturador: no verificado. Las 5 existentes tienen además datos de proveedor/comprobante inválidos: **no reenviar sin autorización por cliente**.
6. **No probado**: Android y Tauri (streaming de `fetch` del SSE, diálogo nativo `confirm`), Redis real de producción para el canje, backfills de permisos con roles reales, concurrencia de stock, producción.
7. **Doriconta/Naturales**: confirmar principal con el cliente (no se cambió nada).
8. **Reintentos históricos**: 181 y 183 reintentos en dos retenciones antes de caer en FAILED → revisar el límite del *worker* de reenvío (pendiente, no urgente).

## 6. Autorizaciones y datos que faltan
1. **Acceso de solo lectura al VPS del facturador** (llave dedicada, instalada por el dueño) para: comparar JMS y YAML con la referencia, ejecutar el verificador PHP y consultar su cola (`docs/fiscal/VERIFICACION_FACTURADOR_VPS.md`).
2. Respuesta del cliente sobre sucursal principal (`doriconta`, `naturales`).
3. Decisiones de la propuesta E1 (6 datos al final del documento).
4. Autorización para producción (backup + ejecución de V172→V179 + backend) cuando se fije la ventana.

## 7. Ventana de despliegue propuesta (no ejecutada)
**Prerrequisitos (todos antes de la ventana):** confirmación de principales; acceso/verificación del facturador; backup de la BD central y snapshot del VPS de BD; ventana de baja actividad.
1. **Backup/snapshot.**
2. **Backend** (imagen nueva) con `BRANCH_SCOPE_STRICT` sin definir y `SSE_ALLOW_QUERY_TOKEN`/`MASTER_ACCESS_LEGACY_TOKEN` en su valor por defecto (puentes activos). Verificar `/health` y logs.
3. **`migrate-fleet` V172→V179**: primero 1–2 tenants pequeños (verificar permisos `branches.*`, `products.manage_global`, usuarios con fila en `tenant_user_branches`, principal única), luego el resto. Revisar el log `V179: registros sin sucursal…` (esperado: ninguno).
4. **Frontends** (Tukifac web, Tukichef 1.4.8, central) en la misma ventana; **no** antes del paso 3 (Tukifac web requiere V173/V176/V177). Tukichef 1.4.8 y Tukifac 2.1.3 **no se publican hasta que el dueño lo ordene** (pendiente la validación funcional restante: Android/Tauri).
5. **Verificación posterior** (30–60 min): login con vendedor de 1 sucursal, de 2 sucursales y admin; POS con carrito; mesa abierta; modo "Todas"; SSE de facturación; una venta y su envío; `/retention/xml` en entorno de pruebas del facturador (con autorización).
6. **Vigilancia 3–7 días**: auditoría del scope, 403 de sucursal, quejas de roles; luego decidir `STRICT`.
7. **Segunda entrega (24 h–1 semana después)**: `RejectLegacyZeroSessionVersion=true`, apagar puentes (`SSE_ALLOW_QUERY_TOKEN=0`, `MASTER_ACCESS_LEGACY_TOKEN=0`), retirar `?master_sso=`.
**Rollback:** imagen anterior del backend y builds anteriores de los frontends; `BRANCH_SCOPE_DISABLED=1` apaga el scope sin cambiar código; las migraciones no se revierten (aditivas). El vínculo de las 5 retenciones a la principal se deshace a mano (ids en el informe de V179).

---

## 8. Guía para el despliegue de esta noche (10-nov-2026)

**Push seguro:** los workflows (`deploy-production.yml` del backend, `deploy.yml` de Tukifac y de central) son solo `workflow_dispatch` (manuales); ninguno se dispara con `push`. Subir hoy no despliega nada. Tukichef no tiene workflow (se compila a mano).
**Estado final de los repos (todos en `main`, sin cambios sueltos, sin nada nuevo en el remoto):** backend 33 commits · Tukifac 10 · Tukichef 7 (incluye `9398ca9` versión 1.4.8, código 14008) · central 1.
**Versiones para compilar:** Tukichef **1.4.8** (14008; `tukichef.version.json` sincronizado a `package.json`, `Cargo.toml`, `Cargo.lock`, `tauri.conf.json` y `build.gradle`: exe `tukichef-1.4.8.exe`); Tukifac **2.1.3** (102013, ya estaba fijada y sin compilar).

**Orden de la noche**
1. Push de los 4 repos (cuando quieras; no despliega).
2. **Backend → workflow "Deploy Production" con las opciones por defecto** (`skip_migrate=false`, `allow_fleet_failures=false`). El workflow construye la imagen, ejecuta `migrate-central` y **`migrate-fleet` sobre todos los tenants ANTES de reiniciar** (V172→V179 por cada tenant, ≈160 ms cada uno; espera del candado hasta 15 min). Si `migrate-fleet` falla en algún tenant, **el deploy se aborta y sigue corriendo la versión anterior**. No hay migraciones centrales nuevas. No definir `BRANCH_SCOPE_STRICT` ni cambiar las variables de los puentes (`SSE_ALLOW_QUERY_TOKEN`, `MASTER_ACCESS_LEGACY_TOKEN`).
3. **Frontend de Tukifac** ("Deploy" de `frontend_tenant`) y **frontend central** ("Deploy" de `frontend_central`), **después** del paso 2 (Tukifac web requiere V173/V176/V177). Hacerlos seguidos (el backend nuevo ya sube la versión de sesión al cambiar roles; los frontends nuevos refrescan en lugar de cerrar sesión).
4. **No editar roles, permisos ni contraseñas** hasta que terminen los pasos 2 y 3.
5. Compilar **Tukichef 1.4.8** (Tauri y Android) y **Tukifac 2.1.3**.

**Verificación rápida tras el backend (5 min):** `/health`; en el log: `V179 … registros sin sucursal` (esperado: ninguno) y errores de `migrate-fleet` (esperado: ninguno); iniciar sesión como admin y como vendedor de 1 sucursal; ver Empresa → Sucursales (una sola "principal"); `doriconta`: la principal debe ser "Principal" (id 1).
**Verificación tras los frontends (10 min):** cambiar de sucursal con un carrito en el POS (debe pedir confirmación); selector "Todas las sucursales" como admin (solo lectura, banner); abrir una venta/nota de venta y su PDF; la tienda pública (sin `stock_by_branch`); acceso maestro desde central (abre con `?master_code=`).
**Corrección fiscal:** viaja con el backend; no emite nada por sí sola. Las 5 retenciones siguen en error y **no se reenvían** sin autorización por cliente.
**Rollback:** el propio workflow imprime la pista de rollback (volver a la imagen anterior); `BRANCH_SCOPE_DISABLED=1` apaga el scope sin cambiar código; las migraciones no se revierten (aditivas e idempotentes).
