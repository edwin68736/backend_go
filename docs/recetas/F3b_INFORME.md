# F3b — Compras, ingresos y carga inicial en Tukichef: informe

Fecha: 25-nov-2026 · Estado: **implementado y probado en local; sin push, sin deploy, sin tocar producción.** F4 no se empezó.

## 1. Qué quedó (Tukichef)
- Menú **Compras de insumos** (permiso `sp.p`, módulo activado y módulo `purchases`): lista, detalle, anulación y nueva compra.
- Nueva compra: buscar o crear proveedor, comprobante, forma de pago o crédito, líneas con unidad de compra e **importe total**; avisa si
  no hay caja abierta. Reintentar usa la misma clave.
- En cada insumo: **Ingreso / merma / desperdicio**, historial de compras y **carga inicial** (solo sin movimientos).

Ajustes de backend surgidos del recorrido: la clave interna del intento ya no aparece en el kardex, y el historial y el detalle muestran el
nombre de la unidad de compra («1 Saco 50 kg», antes «1 BG»).

## 2. Pruebas
- `tsc` y 134 pruebas; reglas del formulario y acceso en `utils/supplyPurchases.test.ts`. Backend de restaurante e inventario en verde.
- Recorrido en navegador (local, empresa `demo`, backend de este código en un puerto aparte):
  1. Compra de 1 «Saco 50 kg» a S/ 200.60 con IGV → stock 100 kg, promedio S/ 3.20, último S/ 3.40.
  2. Merma de 2 kg en ancho de celular → 98 kg, promedio igual; kardex con su valor.
  3. Historial de compras del insumo.
  4. Insumo nuevo y carga inicial de 12 a «8,5» → promedio S/ 8.50.
  5. Anulación de la compra → 48 kg y promedio S/ 3.00.

## 3. No probado
- Compra a crédito, crear un proveedor nuevo y dólares (no admitidos) desde pantalla; usuario sin permisos desde pantalla.
- Negocio sin módulo de compras desde pantalla. Apps compiladas. MySQL 8 y producción.

## 4. Observaciones
- Tras anular una compra, **«Último costo» conserva el de la compra anulada** (comportamiento de las compras desde antes).
- Una compra al contado se registra aunque la caja tenga S/ 0 (igual que en Tukifac).
- La carga inicial no fija «Último costo», solo el promedio.

## 5. Datos que quedaron en la BD local
`demo`: V196 y V197 aplicadas, insumo «Aceite QA F3», compra `F888-2001` anulada y una merma. Usuario de prueba desactivado.
