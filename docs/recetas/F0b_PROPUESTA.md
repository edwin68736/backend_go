# F0b — Exactitud del cobro de mesa y de la anulación: propuesta

Fecha: 25-nov-2026 · Estado: **PROPUESTA; no se implementó nada.** Espera autorización expresa.

## 1. Problema

### A. Doble cobro de la misma mesa — **confirmado**
Dos cobros de la misma mesa, con claves de idempotencia distintas (dos cajas o dos dispositivos) y **sin cerrar la mesa**, crean **dos ventas de las mismas comandas**: 10 de 10 intentos en MariaDB local, con y sin F0a (es anterior). No se probó en MySQL 8 ni se midió con qué frecuencia ocurre en producción.

**Causa** (`internal/restaurant/service/restaurant_service.go`, `BillTable`):
- Las comandas a cobrar se leen **fuera** de la transacción (`s.db`, ~línea 1196) y con ellas se calculan las líneas y los importes.
- Dentro de la transacción solo se bloquea la **sesión**. En cobro parcial la sesión vuelve a `open` al terminar, así que el segundo cobro pasa con su lista ya vieja.
- Al final se hace `UPDATE billed_at` **sin condición**, y su error se ignora (~línea 1679).

Con la mesa cerrándose, el segundo cobro debería rechazarse porque la sesión ya no está `open` (deducido del código, no probado).

### B. Riesgos del mismo origen — **deducidos del código, sin probar**
1. **Comanda agregada mientras se cierra la mesa:** el cierre borra todas las comandas de la sesión, incluida una que no entró en la venta.
2. **Anular una comanda mientras se cobra:** `cancelComandaTx` valida con datos leídos antes y actualiza sin condición; una comanda cobrada podría quedar además anulada.
3. **Doble anulación de una venta:** `Cancel` y `CancelNotaVenta` leen `status` fuera de la transacción y actualizan sin condición; dos anulaciones simultáneas podrían devolver **dos veces** el dinero y el stock. `Cancel` se dispara desde la aceptación de notas de crédito, que puede llegar por más de una vía.

## 2. Cambios propuestos

### 2.1 Cobro (`BillTable`)
Dentro de la transacción, después del reclamo de la sesión:
1. Releer **con bloqueo** (`FOR UPDATE`, ordenadas por id) las comandas que se van a cobrar.
2. Rechazar si alguna ya está cobrada o anulada, o si su cantidad cambió (en cobro parcial: si ya no alcanza).
3. Si el cobro cierra la mesa, comprobar que no haya comandas pendientes fuera de la selección.
4. `UPDATE billed_at` **condicional** (`billed_at IS NULL AND cancelled_at IS NULL`), exigiendo las filas afectadas exactas y devolviendo el error.

Mensaje para el usuario: «La mesa cambió o ya se cobró. Actualiza e inténtalo de nuevo.» Si se rechaza, se revierte todo (venta, correlativo, stock y pagos).

### 2.2 Anular comanda
`UPDATE` condicional (`billed_at IS NULL AND cancelled_at IS NULL`) verificando filas afectadas.

### 2.3 Anulación de venta (`Cancel`, `CancelNotaVenta`)
Como **primer paso** de la transacción, marcar `cancelled` solo si aún no lo está (`WHERE id = ? AND status <> 'cancelled'`). Si no afecta ninguna fila, se devuelve el mismo error de hoy («la venta ya está cancelada»), del que dependen otros flujos, y no se revierte nada. Durante la implementación se verifica que las reversas de caja, stock, series y anticipos no dependan de leer el estado anterior.

## 3. Archivos
| Archivo | Cambio |
|---|---|
| `internal/restaurant/service/restaurant_service.go` | §2.1 y §2.2 |
| `internal/sales/service/sale_service.go` | §2.3 |
| Pruebas nuevas en ambos paquetes | §4 |

Sin migraciones, sin cambios en el frontend, sin cambios en importes, comprobantes ni en lo que se envía a SUNAT.

## 4. Pruebas y criterios de aceptación
Primero se escriben las pruebas y se comprueba que **fallan** hoy.

| Caso | Resultado esperado |
|---|---|
| Misma mesa, claves distintas, sin cerrar (10/10 hoy) | 1 venta, stock descontado una vez, el otro recibe el mensaje |
| Misma mesa, cerrando | 1 venta |
| Misma clave (reintento) | Devuelve la venta original, como hoy |
| Cuenta dividida: dos selecciones distintas a la vez | Ambas ventas, o una rechazada limpiamente; nunca una comanda en dos ventas |
| Cantidad parcial simultánea | Nunca se cobra más de lo pedido |
| Agregar comanda mientras se cierra | Ninguna comanda se pierde |
| Anular comanda mientras se cobra | Cobrada o anulada, nunca ambas |
| Doble anulación (venta y nota de venta) | 1 reversa de caja y de stock |
| Flujos actuales (cobro normal, dividido, POS, anulación, NC) | Sin cambios; suite completa en verde |

Concurrencia en MariaDB estricto (y MySQL 8 cuando haya instancia).

## 5. Riesgos
- **Más rechazos legítimos:** si un mozo agrega o anula mientras la caja cobra, la caja deberá actualizar. Es el comportamiento buscado, pero el personal lo notará.
- **Espera de bloqueos:** se suman filas de comandas a la transacción del cobro.
- **Otros que llaman a `Cancel`:** notas de crédito aceptadas y comprobantes rechazados; se prueban expresamente.
- La conversión NV → comprobante simultánea (dos hijos) **no** entra en F0b: es fiscal, no de inventario.

## 6. Despliegue — atención
F0a ya está commiteada en `main` (sin push) y trae migraciones V193 y V194. **Un deploy normal de `main` llevaría F0a junto con F0b.** Para desplegar F0b sola:
1. Implementarla en una copia de trabajo basada en lo que hoy está en producción (`origin/main`, `547ed39`), con sus pruebas independientes de F0a.
2. Hacer push solo de ese commit y desplegar el backend con `skip_migrate` (no tiene migraciones).
3. Reubicar los commits locales de F0a encima (no se han publicado).

Push y deploy requieren tu autorización en cada paso. Observación sugerida: número de rechazos «la mesa cambió», errores 1205/1213 y latencia del cobro.

Esto **cambia una decisión previa** (F0b después de observar F0a): se propone invertir el orden porque el defecto ya está en producción y F0a está detenida por la red.

## 7. Opcional, requiere autorización
Consulta de solo lectura en producción para saber si ya hubo dobles cobros (ventas de la misma sesión de mesa, con las mismas líneas y pocos segundos de diferencia). No se ejecutó.

## 8. Fuera de alcance
El pool de conexiones (venta y cobro piden una segunda conexión dentro de su transacción): tarea aparte.
