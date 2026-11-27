# Cierre, punto 2: MySQL 8 y actualización desde `main`

27-nov-2026. Rama `insumos-recetas`. Sin desplegar. MySQL **8.0.30** local, con el mismo `sql_mode` que producción.

## 1. Pruebas contra MySQL
Solo 9 paquetes saben usar MySQL; cada uno corrió con una base nueva. El resto de pruebas usa SQLite en memoria.

734 pasan, 7 fallan, 2 se omiten. Ninguna falla es del módulo:
- 4 de XML (y 1 de las omitidas) buscan el facturador por ruta relativa: con él al lado, las 5 pasan.
- 2 de anulación en `sales/service` cuentan movimientos de pruebas anteriores que comparten la base: solas pasan.
- 1 de cupo de comprobantes (`pkg/saas/docusage`) falla igual en `main` (tarea aparte).
- La otra omitida (`TestBillTable_concurrentOnlyOneSucceeds`) está desactivada en el código.

## 2. Suite completa del backend
103 paquetes bien. Fallan dos, igual que en `main`:
- `pkg/saas`: las mismas 46 pruebas en la rama y en `main` (`database is locked` en SQLite).
- `internal/superadmin/service`: una prueba de simultaneidad falla 9 de 10 veces en ambas.

## 3. Simulacro de actualización
Empresa creada y usada con el código de `main`: ventas, boleta, anulación, compras (una anulada), traslados (uno en camino),
mesa cobrada y mesa abierta, ingresos con costos 12.35, 0.01 y 987654.32. Segunda empresa con un saldo de stock duplicado.
Después se corrieron `migrate-central` y `migrate-fleet` con las opciones del despliegue (V193 a V200).

| Comprobación | Resultado |
|---|---|
| Datos que ya existían | ninguna fila perdida ni cambiada en las empresas; en la central solo fechas y versión de esquema |
| Columnas nuevas | vacías o neutras; módulo y descuento apagados |
| `unit_cost` de ingresos (cambia de 2 a 6 decimales) | valores intactos |
| Empresa con duplicados | índice omitido y avisado; saldos intactos; sigue vendiendo |
| Reporte de utilidad antes y después | idéntico (10 líneas) |
| Repetir la migración | no aplica nada |
| Código de `main` sobre la base migrada | venta, combo, boleta, compra, anulaciones, mesa y producto nuevo: bien |
| Código de la rama, lo pendiente de antes | anular venta y compra, recibir traslado, cobrar mesa: bien y con el stock correcto |
| `audit-stock-reconcile` | sin diferencias entre saldos y kardex (solo el duplicado sembrado) |
| Estructura actualizada contra empresa nueva | una diferencia, corregida (abajo); después, ninguna |

## Corregido
En una empresa **nueva**, `tenant_products.is_supply` admitía vacío; en una actualizada, no. Se marcó `not null` en el modelo
(`TestEmpresaNueva_marcaDeInsumoNoAdmiteVacio`). No necesita migración: las actualizadas ya quedan así con la V195.

## Sin probar
- MySQL 8.0.46.
- Volumen real: cuánto tarda la migración y cuánto bloquea. La V194 copia `tenant_inventory_document_details` y no deja
  escribir en ella mientras tanto; la V193 y la V197 crean índices.
- Operaciones **al mismo tiempo** que la migración (aquí fueron antes y después).
- Una empresa con datos reales de producción.

## Antes de desplegar
Revisar en producción, solo lectura: tamaño de esas tablas y cuántas empresas tienen saldos duplicados
(`stock-unique-index`, sin `--apply`).
