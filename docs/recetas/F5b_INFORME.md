# F5b — Pantallas del descuento de insumos (Tukichef): informe

Fecha: 27-nov-2026 · Ramas `insumos-recetas` de backend y Tukichef · Sin desplegar. Propuesta: `F5_PROPUESTA.md` §4.

## Qué hace
- **Recetas → «Descuento al cobrar»** (permiso `s.m`): estado, política si falta un insumo, revisión antes de encender (insumos sin stock
  o sin costo, combos incompletos, platos que dejan de descontar su stock, platos sin receta) y encender/apagar con confirmación.
- **POS y mesa:** los platos con receta muestran «Porciones: N» en vez de su stock; en cero, en rojo.
- **Presentaciones:** las que tienen receta muestran porciones y no se bloquean como «Agotado» por el stock propio (antes sí).
- **Aviso bajo el carrito:** qué insumos no alcanzan para el pedido completo (platos que comparten insumo, combos y, en la mesa, lo ya
  pedido sin cobrar), y si se podrá cobrar según la política.
- Sin el módulo activado no se consulta nada; con el descuento apagado las pantallas quedan como antes.

## Backend añadido
`GET /api/restaurant/recipe-availability` y `POST /api/restaurant/recipe-check` (quien toma pedidos; sin costos) y
`GET /api/restaurant/recipe-consumption/checklist` (`s.m`). No escriben nada.

## Probado
- Backend: `TestRecipeConsumptionScreens` (permisos, apagado, porciones, insumo compartido, lo pendiente de la mesa, combo y
  presentación) en SQLite y MySQL 8.0.30; batería completa.
- Tukichef: tipos, compilación, pruebas (incluida `recipeStock.test.ts`) y lint sin avisos nuevos.
- **En pantalla** (navegador, empresa de prueba local): encender con su revisión, porciones en POS y mesa, aviso por insumo
  compartido, venta rápida con descuento (stock y costo verificados en la base; el ticket no cambia), política «no dejar cobrar»
  (cobro de mesa rechazado, sin venta ni cambio de stock), presentaciones y vista de celular.

## No probado
- MySQL 8.0.46, apps compiladas (Android y escritorio), impresión real.
- En pantalla: combos, delivery y para llevar, usuario mozo o cajero con PIN, apagar el descuento, varias sucursales.

## A tener en cuenta
- Con «no dejar cobrar», en la mesa la comanda **ya fue a cocina** cuando el cobro se rechaza: hay que reponer el insumo, anular la
  comanda o cambiar la política. Es el riesgo 2 de la propuesta.
- Las porciones son por plato; dos platos que comparten insumo muestran cada uno su máximo. El aviso del carrito sí los suma.
- Las porciones se refrescan al cargar la pantalla, cambiar de sucursal y tras cobrar en este equipo; no al cobrar otro.
- Cada cambio del carrito hace una consulta (con 0.4 s de espera), solo con el descuento encendido.
- El panel Tukifac («Nueva venta») no se cambió: puede seguir mostrando o avisando por el stock propio del plato. Falta revisarlo.
