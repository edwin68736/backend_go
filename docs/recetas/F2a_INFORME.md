# F2a — Traslados entre sucursales: informe

Fecha: 25-nov-2026 · Estado: **implementado y probado en local; sin push, sin deploy, sin tocar producción.** F2b no se empezó.
Alcance: `F2_PROPUESTA.md` §5, parte F2a. Afecta a **todos los tenants** con traslados; sin migración ni cambios de pantalla.

## 1. Defectos reproducidos (MariaDB 10.4 estricto, antes de corregir)
| Caso | Resultado antes |
|---|---|
| 8 confirmaciones a la vez de un traslado de 10 | las 8 tienen éxito: el destino recibe 80 |
| 8 cancelaciones a la vez | las 8 tienen éxito: el origen recupera 80 |
| confirmar contra cancelar | 4 operaciones exitosas |
| 8 anulaciones de un traslado antiguo | las 8 tienen éxito |
| anular por línea un traslado con cabecera (pendiente) | se aceptaba y movía stock del destino |
| traslado directo que falla en la entrada | la salida quedaba hecha |

La reproducción usa llamadas simultáneas al servicio. No se probó con doble clic desde la pantalla.

## 2. Corrección (`internal/inventory/service/inventory_service.go`)
- Confirmar y cancelar cambian el estado **solo si sigue pendiente**, dentro de la transacción; si no, devuelven el mensaje de siempre.
- Anular un traslado antiguo reclama la línea y revierte en la misma transacción; los traslados con cabecera ya no se anulan por línea.
- El traslado directo es una sola transacción.
- Saldos bloqueados en orden estable y reintento ante interbloqueo (mecanismos de F0a).
- Los errores al reasignar números de serie ya no se ignoran.

## 3. Pruebas
- `transfer_concurrency_test.go` (6 casos): fallaban antes, pasan tres veces seguidas después. Requieren MariaDB/MySQL.
- MariaDB estricto: inventario (servicio y handler), restaurante e insumos en verde.
- SQLite: 101 paquetes en verde. `pkg/saas` se excluyó (fallos previos, ver `F1a_INFORME.md`).
- La confirmación de series por el usuario del destino (con filtro por sucursal) sigue pasando.

**No probado:** MySQL 8, producción, traslado desde pantalla.

## 4. Datos ya afectados
El arreglo evita casos nuevos; **no corrige** traslados que ya se hayan duplicado. No se consultó producción. Consulta de solo lectura
para detectarlos, por empresa (probada en local, sin resultados):

```sql
SELECT m.transfer_id, COUNT(*) AS movimientos,
       (SELECT COUNT(*) FROM tenant_transfer_logs l WHERE l.transfer_id = m.transfer_id) AS lineas,
       SUM(m.quantity) AS unidades
FROM tenant_stock_movements m
WHERE m.transfer_id IS NOT NULL AND m.reference IN ('TRANSFER-CONFIRMED','TRANSFER-CANCELLED')
GROUP BY m.transfer_id
HAVING movimientos > lineas;
```

No detecta anulaciones repetidas de traslados antiguos (no llevan `transfer_id`).

## 5. Despliegue (no autorizado)
Solo backend. En el historial local va **encima de F0a, F1a y F1b**, que tienen migraciones. Para desplegar F2a sola habría que aplicar su
commit sobre `origin/main` (usa funciones de F0a: `runInventoryTx` y `LockStockRowsTx`, así que necesita al menos F0a-3).
