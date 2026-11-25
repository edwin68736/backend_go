# F0b — Exactitud del cobro de mesa y de la anulación: informe de entrega

Fecha: 25-nov-2026 · Autorizada por el dueño · Propuesta: `F0b_PROPUESTA.md`. Implementada sobre lo que está en producción (`547ed39`), **independiente de F0a**. No desplegada; sin consultar ni modificar producción; sin migraciones.

## 1. Archivos
| Archivo | Cambio |
|---|---|
| `internal/restaurant/service/bill_guard.go` | **Nuevo.** Verificación dentro de la transacción del cobro y marca condicional de lo cobrado |
| `internal/restaurant/service/restaurant_service.go` | Llama a la verificación; `billed_at` condicional con su error; anular comanda bloquea la mesa y actualiza solo si sigue sin cobrar ni anular |
| `internal/sales/service/sale_service.go` | `claimCancellationTx`: primer paso de `Cancel` y `CancelNotaVenta`; ganchos solo para pruebas |
| Pruebas nuevas (3 archivos) | Ver §3 |

## 2. Comportamiento
- El cobro rechaza con **«La mesa cambió o ya se cobró. Actualiza e inténtalo de nuevo.»** si otra operación cobró, anuló o cambió las comandas, o (al cerrar) si hay comandas pendientes fuera de la selección. Se revierte todo.
- Anular una comanda ya cobrada o anulada: «la comanda ya fue cobrada o anulada; actualiza la mesa». Anular bloquea primero la mesa (mismo orden que el cobro; refinamiento respecto a la propuesta para evitar bloqueos mutuos).
- La segunda anulación simultánea de una venta falla con el mensaje de siempre («la venta ya está cancelada») y no revierte nada.
- Dos selecciones **distintas** a la vez siguen cobrándose ambas.
- Sin cambios en importes, comprobantes, SUNAT ni frontend.

## 3. Pruebas
**Antes del arreglo** (código de producción) fallaban: doble cobro sin cerrar la mesa, selección repetida, comanda agregada mientras se cierra (se perdía), anulada mientras se cobra, anular una ya cobrada y **doble anulación** (venta y nota de venta). Quedan confirmados los riesgos que la propuesta marcaba como deducidos. La cantidad parcial ya estaba protegida.

| Prueba | Resultado |
|---|---|
| 10 pruebas deterministas (SQLite), con ganchos que ejecutan la otra operación justo en el intervalo | OK |
| MariaDB 10.4 estricto: 15 intentos de dos cajas sobre la misma mesa y 15 de cobrar+anular, 10 repeticiones | OK: siempre una venta; nunca cobrada y anulada; sin deadlocks |
| Control: con la verificación desactivada, el doble cobro vuelve a aparecer | Confirmado |
| Suite completa | Ver §5 |

**No probado:** MySQL 8.0.46; el flujo por la pantalla de Tukichef; la doble anulación disparada por dos avisos reales de SUNAT.

## 4. Riesgos y despliegue
- El personal verá el mensaje cuando un mozo agregue o anule mientras la caja cobra: es el comportamiento buscado.
- Backend solamente, **sin migraciones** (usar `skip_migrate`).
- Para desplegar F0b **sin** F0a: hacer push solo del commit de F0b sobre `origin/main`, desplegar, y después reubicar los commits locales de F0a encima (no están publicados). Requiere autorización.
- Observar: rechazos «La mesa cambió», errores 1205/1213 y latencia del cobro.
- Reversión: volver a la imagen anterior (no hay cambios de esquema ni de datos).

## 5. Suite completa
`go test ./...`: 98 paquetes OK, incluidos restaurante, ventas y facturación. **Falla `pkg/saas`** (cobros de suscripción del panel central): `database is locked (SQLITE_BUSY)` en `TestApprovePayment_*` y `TestListInvoices*`.
- No lo causa F0b: ninguno de los paquetes modificados está en sus dependencias, y la misma prueba falla **igual en una copia limpia de `origin/main` (547ed39) sin ningún cambio**.
- Causa aparente `[I]`: `SubmitPayment` lee los ajustes por una conexión distinta dentro de su transacción y SQLite (archivo en disco) se bloquea. Hoy más temprano el paquete había pasado en el repositorio principal; no se determinó por qué cambió. Queda como asunto aparte.
