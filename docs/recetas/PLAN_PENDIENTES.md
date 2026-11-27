# Insumos y recetas: plan para continuar en otra PC

Escrito el 25-nov-2026. **Empieza por aquí.**

## 1. Dónde está el código
En los cuatro repositorios, **rama `insumos-recetas`** (no `main`):

| Repositorio | Contiene |
|---|---|
| backend (`backend_go`) | F0a a F5a, migraciones V193–V199, todos los informes |
| `frontend_restaurant_tenant` (Tukichef) | Insumos, Compras de insumos, Recetas (versión 1.6.0); arreglo de gaveta 58 mm |
| `frontend_tenant` (Tukifac) | insumos rotulados en compras, traslados e inventario; arreglo de gaveta |
| `frontend_central` | interruptor «Insumos y recetas» por empresa |

```bash
git fetch origin
git checkout insumos-recetas
```

- **No fusionar a `main` ni desplegar hasta terminar** (decisión: se sube todo junto cuando funcione al 100 %).
- `main` queda como estaba: se pueden desplegar otros arreglos sin arrastrar el módulo.
- La rama también lleva dos arreglos que no son del módulo y siguen sin desplegar: **una compra ya no se anula dos veces** y
  **un traslado se confirma una sola vez**. Si urgen, se aplican aparte sobre `main` (el de traslados necesita F0a).
- F0b (doble cobro de mesa) ya está en `main`, pero **todavía no se había desplegado**.
- Los números de commit que citan los informes son los de la PC original; en esta rama pueden ser distintos (los mensajes son los mismos).

## 2. Qué está hecho
| Fase | Qué | Informe |
|---|---|---|
| F0a | bloqueo de la fila de stock, índice único de saldos (V193, V194), reconciliador | `F0a-1..3_INFORME.md` |
| F0b | doble cobro de mesa y doble anulación | `F0b_INFORME.md` |
| F1a, F1b | catálogo de insumos (V195), pantallas e interruptor | `F1a_INFORME.md`, `F1b_INFORME.md` |
| F2a, F2b | traslados una sola vez; costo promedio (V196) | `F2a_INFORME.md`, `F2b_INFORME.md` |
| F3a, F3b | compras, ingresos, mermas y carga inicial (V197) | `F3a_INFORME.md`, `F3b_INFORME.md` |
| F4a, F4b | recetas, costo, precio sugerido, disponibilidad, aplicar precio (V198) | `F4a_INFORME.md`, `F4b_INFORME.md` |

| F5a | descuento y reversa de insumos al vender, en el backend (V199) | `F5a_INFORME.md` |

El módulo nace apagado (`recipes_enabled`, lo enciende el superadmin) y **el descuento al vender también** (`recipes_consume`, lo
enciende el restaurante desde Tukichef → Recetas).

| F5b | pantallas del descuento en Tukichef (porciones, aviso de insumos, revisión y encendido) | `F5b_INFORME.md` |

## 3. Lo que falta, en orden

### 3.1 F5a — descuento de insumos al cobrar (backend): HECHA el 26-nov-2026, ver `F5a_INFORME.md`
Propuesta completa en `F5_PROPUESTA.md`. Las 6 decisiones quedaron **aceptadas como se recomiendan** (26-nov-2026); falta implementarla:
- [x] F5a (backend) y F5b (pantallas) por separado
- [x] El descuento lo enciende el administrador del restaurante, con lista de verificación
- [x] Insumo insuficiente: por defecto vender y quedar en negativo; «bloquear» como opción
- [x] Combo con algún componente sin receta: costo de la línea vacío
- [x] NC parcial sobre comprobante nacido de una NV: no repone (igual que hoy)
- [x] Costo de receta en el reporte de utilidad: en F6

Trabajo:
- [x] V199: registro de consumos y de reversas (únicos), `recipe_cost` en la línea de venta, `recipes_consume` (apagado), política, tipos de movimiento
- [x] `ConsumeForSaleTx` en `SaleService.Create` y en `BillTable` (usando solo la transacción recibida)
- [x] Plato con receta deja de descontar su stock directo (venta, validación previa y agregar pedido)
- [x] Combos: los componentes deben saber de qué línea salieron
- [x] Reversa en `Cancel`, `CancelNotaVenta` y NC parcial
- [x] Interruptor de emergencia por variable de entorno
- [x] Prueba dorada (apagado = igual que hoy), flujos reales, `payload_json` idéntico, simultaneidad en MariaDB estricto, suite completa

### 3.2 F5b — pantallas del descuento (Tukichef): HECHA el 27-nov-2026, ver `F5b_INFORME.md`
- [x] Interruptor con lista de verificación (platos sin receta, insumos sin stock o sin costo) y política
- [x] Aviso de insumo insuficiente al pedir y porciones disponibles en el POS

### 3.3 F6 — reportes (Tukichef, por sucursal): HECHA el 27-nov-2026, ver `F6a_INFORME.md` y `F6b_INFORME.md`
- [x] Existencias y valorización · historial de compras por insumo · consumo de ingredientes
- [x] Costo por plato · food cost · margen por plato (con `recipe_cost`) · platos de mayor costo · insumos de mayor consumo
- [x] Disponibilidad · mermas
- [x] Quién ve la utilidad: `/api/sales/profit-detail` exige `reports.profit`

### 3.4 Antes de dar el módulo por terminado
- [ ] Decidir la **carga inicial masiva (Excel)**: la propuesta la mencionaba (§13); hoy se carga insumo por insumo
- [ ] Subir la versión de Tukichef y compilar las apps (Android y escritorio)
- [ ] Probar todo en **MySQL 8** (producción). El 26-nov-2026 pasaron en MySQL 8.0.30 local las pruebas de inventario, restaurante e
  insumos; en ventas, dos de anulación solo fallan si comparten la base con otras (pasan en una base nueva). Falta probar en 8.0.46 y las pantallas.
- [x] Probar el interruptor del panel central en pantalla (27-nov-2026)
- [ ] Gaveta 58 mm: probar con impresora real

### 3.5 Para una versión 2 (no ahora)
Preparaciones intermedias y subrecetas, extras y modificadores con insumo, desglose de combos, consumo teórico vs real, costos adicionales,
rendimiento de compra, importación por Excel, vista previa del costo antes de guardar.

## 4. Pruebas en pantalla (27-nov-2026, local, empresa desechable)
Probado: descuento con combos, delivery, para llevar, mozo, cajera y cocina, dos sucursales, apagado y anulación; recetas (quitar,
aviso de precio, precio propio de sucursal, incompleta, merma y rendimiento); compras e insumos; panel central; Tukifac (nueva nota
de venta, punto de venta, anulación, traslado y orden de compra). «Editar sin ver costos» no existe como rol en Tukichef.

Reportes (F6b): las seis pantallas, Excel, PDF, módulo apagado, selector de sucursal y celular.

**Sin probar:** factura y boleta con receta desde la pantalla, SUNAT, MySQL 8.0.46, apps compiladas, impresora real.

Observado y sin cambiar: la cajera no ve el aviso de faltantes al cobrar una mesa ya pedida; quitar una receta devuelve el plato a
su stock propio sin avisarlo; menos de 0.5 g de un insumo en kg no se descuenta (3 decimales).

## 5. Defectos conocidos sin corregir
- Anular una compra revierte stock según el control de stock **actual** del producto, no el de cuando se compró.
- Tras anular una compra, «último costo» conserva el de la compra anulada.
- La reposición de una NC parcial no es idempotente para productos normales, y no repone si el comprobante nació de una NV.
- **Corregido el 26-nov-2026:** venta, anulación, cobro de mesa, registro de pagos, cobro de cuentas y pago a proveedores ya no piden una
  segunda conexión con la transacción abierta (pruebas `TestUnaConexion_*` y `TestMySQL_cobrosSimultaneosConTresConexiones`). Pasa a `main`
  junto con el módulo, así que **producción sigue con el defecto** hasta entonces.
- La **emisión de nota de crédito** todavía pide una segunda conexión (serie y correlativo dentro de la transacción): pendiente, con
  propuesta aparte porque toca la numeración.
- El pago a proveedor simultáneo con la anulación de la compra no está protegido.
- El retorno a la versión anterior del despliegue no funciona como está descrito: revisarlo antes de desplegar el módulo.
- `pkg/saas` tiene 13 pruebas que fallan desde antes; en MariaDB fallan dos de anulación en `sales/service`.

## 6. Despliegue (cuando todo esté terminado)
1. Fusionar `insumos-recetas` en `main` en los cuatro repositorios.
2. Backend **con** migración (V193 en adelante), en horario de poco movimiento; después panel central y Tukifac; por último Tukichef y apps.
3. V193 omite el índice en empresas con saldos duplicados: revisar con el comando `stock-unique-index`.
4. Activar primero **una empresa de prueba**; emitir un comprobante real y revisar el guardado y su CDR.
5. Antes de volver atrás, desactivar los insumos creados: una versión anterior los vería como productos con precio 0.

El plan detallado con los comandos del servidor (`PLAN_DESPLIEGUE.md`) **no se subió** porque los repositorios son públicos: está en la PC
original.

## 7. Preparar la otra PC
- Go 1.25 o superior, Node 22, MariaDB 10.4 (XAMPP) y los cuatro repositorios en una misma carpeta.
- Los `.env` no están en git: copiar `backend/.env.example` y completar (o traerlos de la PC original por un medio seguro).
- La base local no viaja: crear una empresa de prueba, migrarla (`go run . migrate-tenant <slug>`) y activarle el módulo.
- Frontends: `npm install` y `npm run dev`. En Windows, el proxy de Vite no resuelve `*.localhost`.

Pruebas:
```bash
go test $(go list ./... | grep -v '/pkg/saas$') -count=1      # backend
npx tsc -b && npx vitest run                                  # Tukichef
```
MySQL con el `sql_mode` de producción (la base se borra entera: su nombre debe contener `test`; correr paquete por paquete):
```bash
export TUKIFAC_TEST_MYSQL_DSN="root:@tcp(127.0.0.1:3306)/tukifac_x_test?charset=utf8mb4&parseTime=True&loc=Local&interpolateParams=true&sql_mode='ONLY_FULL_GROUP_BY,STRICT_TRANS_TABLES,NO_ZERO_IN_DATE,NO_ZERO_DATE,ERROR_FOR_DIVISION_BY_ZERO,NO_ENGINE_SUBSTITUTION'&innodb_lock_wait_timeout=5"
go test ./internal/restaurant/ -run 'TestRecipe|TestSupply' -count=1
```

## 8. Reglas de trabajo (el `CLAUDE.md` de la carpeta raíz no está en git)
- Por fase: diagnóstico → archivos → riesgos → propuesta → **esperar autorización** → implementar solo esa fase → pruebas → MariaDB estricto →
  auditoría de efectos → informe y commit propio. Sin avanzar solo y sin desplegar sin permiso.
- Producción: solo lectura salvo permiso explícito.
- Fiscal: probar por el flujo real, verificar el `payload_json`/XML final, test de regresión y decir qué no se probó.
- Columnas nuevas o ensanchadas: migración versionada antes de desplegar. La base local no es estricta y trunca en silencio.
- La siguiente migración libre es **V201**.

## 9. Lo que no está en la rama
- **Ya subido** (trabajo de otras sesiones): en Tukichef, el commit «Ajustes de impresoras y series» (**sin revisar**); en Tukifac, el
  tutorial de stock y almacenes. Un guardado antiguo de junio de Tukifac quedó aparte en la rama `respaldo/stash-2026-06-01`
  (no está aplicado ni probado).
- **No se sube porque los repositorios son públicos:** `docs/arquitectura/`, `PLAN_DESPLIEGUE.md`, los documentos de la carpeta raíz y las
  notas del asistente. Están en `copiar_a_otra_pc.zip`, en la carpeta raíz de la PC original.
- Los `.env` y las bases de datos locales se copian aparte.

## 10. Antes de desplegar (revisión)
- Todo lo que otras sesiones habían confirmado ya estaba en `main`; compara con lo que hay desplegado, porque `main` tiene cambios sin
  desplegar además de este módulo.
- El flujo de despliegue se lanza a mano desde GitHub: no desplegar `insumos-recetas` hasta terminar y fusionar.
