# Análisis: «OBS PANEL EQUIPOS 19.11.pdf» (Gestión de Equipos, panel central)

Fecha del análisis: 23-nov-2026. Solo lectura: no se modificó código ni se tocó producción. El PDF tiene 9 páginas con 14 capturas
(resaltadas en amarillo/rojo por quien escribió las observaciones). Rutas relativas a `D:\tukifac_premium_prod`.

## 0. Hallazgo previo importante

El commit `8ea293e` (backend) y `3ba7175` (frontend_central), ambos del 16-nov, ya implementaron «retornos con guía y seguimiento de Shalom,
aviso de llegada a destino y casilla Recibido». Ambos repos están sincronizados con `origin/main`. Las capturas del PDF ya muestran ese
diseño (lápiz de guía y columna «Recibido»), por lo que **el PDF se hizo sobre una versión que ya lo incluía**. Varias observaciones de
retornos (#6, #7) son por tanto ajustes sobre algo ya hecho, no funciones nuevas. No pude comprobar contra producción qué versión corre.

## 1. Observaciones transcritas y estado

| # | Observación (texto del PDF) | Captura | Estado |
|---|---|---|---|
| 1 | Cuando un pedido es vendido en oficina, que la columna de ENVÍO pase automáticamente a **«Entregado en oficina»**. Que se actualice el filtro de envío (En tránsito, En agencia, Recogido, En retorno, Entregado en oficina). | Pedidos: pedido 200 con «Recojo en Oficina» y envío «En agencia»; pedido 199 «Por despachar»; desplegable «Todo envío» con Por despachar / En tránsito / En agencia / Recogido / En retorno | **Falta** |
| 2 | En la columna Detalle, los productos del pedido en modo lista, cada producto en una línea. | Pedidos: «1x POS-E803 – 80 MM(BLUE) \| 5x CONT. 8…» truncado; subrayado en rojo = una línea por producto | **Falta** |
| 3 | Modificar la lista de planes a 12 opciones: Microempresa / Emprendedor / Premium / Corporativo × plan mensual / semestral / anual. | Nuevo pedido, ítem «Plan Tukifac»: lista actual con 1 MES GRATIS, 3 MES FREE, Pruebas, Trial, MICROEMPRESA, MICROEMPRESA PRO, EMPRENDEDOR, PREMIUM, ILIMITADO, Pro, CORPORATIVO | **Falta** (ambigua, ver P3) |
| 4 | En lugar de que se visualice «Yape», que se visualice serie y correlativo del comprobante. | Detalle del pedido 191, «Cobros aplicados»: «S/ 20.00 Yape · anticipado», «S/ 5.00 Yape · anticipado» | **Parcial / ambigua** (ver P4) |
| 5 | En la pestaña Pedido-agencia se pueda filtrar por estado de pago. | Pedido-agencia, columna «Pago» resaltada; la barra solo tiene usuario y «Ver todos los pedidos» | **Falta** (solo UI) |
| 6 | En retornos, cuando se coloque N° de guía y código, que el estado se vincule con Shalom y notifique cuando esté en la agencia para recogerlo (todos los estados de Shalom). | Retornos: columnas «Guía y seguimiento» y «Estado» resaltadas, ambos retornos en «Solicitado» | **Parcial** (backend hecho; la pantalla no refleja «todos los estados») |
| 7 | Botón Gestionar y casilla ✔ Recibido tienen casi la misma función: que se quede el check, pero añadiéndole casilla de costo del retorno y observaciones. | Modal «Retorno N° 5» (Estado, Condición, Costo, Saldo no cobrado, Observaciones) y modal «Recibir retorno N° 5» (Buen estado / Dañado) | **Parcial** (ver P7) |
| 8 | Como «Dañado» no vuelve al stock: ¿se puede habilitar en la pestaña Stock un apartado o columna con cuántos equipos dañados tenemos, o dónde se registra esa cantidad? | (texto) | **Falta** (hoy no se registra en ningún lugar consultable) |
| 9 | Al activar «Ver inactivos» también se visualizan los activos. | Catálogo con «Ver inactivos» marcado, filas «Activo» resaltadas | **Falta** (comportamiento actual intencional) |
| 10 | En «Producto» no figura el nombre, solo se visualiza en la columna de código. | PDF «Requerimiento de equipos N° RQ-00003»: Código «EQUIPO PRUEBA», columna Producto vacía | **Falta** (ver P10) |
| 11 | En los rótulos no sale el nombre completo de la agencia. | Detalle del pedido 191 con la dirección completa de la agencia (Ancón – Carretera Serpentín de Pasamayo Mz. G…) y el rótulo que solo imprime «ANCON - CARRETERA SERPENTÍN DE» | **Falta** |

## 2. Detalle por observación (código actual, qué hacer)

### #1 Pedido vendido en oficina → «Entregado en oficina» (+ filtro)
**Hoy:** `saveShipmentTx` crea todo envío con `Status: "pendiente_envio"` (`backend_principal/internal/equipos/service/shipments.go:179`), también si
`delivery_mode` es `oficina` (`applyShipmentInput`, shipments.go:140-146). Para cerrarlo hay que marcar manualmente «Cliente recogió»
(`MarkPickedUp`, shipments.go:361-393), que ya admite oficina directo (`direct := sh.DeliveryMode != "agencia"`). La etiqueta del estado
`entregado` es «Recogido» (`frontend_central/src/services/equiposOrders.service.ts:20`; `ordersCommon.tsx:67` `ShipmentBadge` solo recibe el estado,
no la modalidad). El filtro de envío (`OrdersTab.tsx:150-153`) se arma con ese mismo diccionario y el backend filtra por `sh.status`
(`order_view.go:325`). La importación de Excel ya reconoce el texto «entregado en oficina» como modalidad oficina (`import_parse.go:332-334`),
o sea que es vocabulario real del negocio.

**Hacer:**
- Backend: al crear/confirmar un pedido con `delivery_mode = "oficina"`, dejar el envío en `entregado` con `picked_up_at` = fecha del pedido
  (en `saveShipmentTx`/`shipmentUpdateOnOrderTx` y en el camino de importación). `pendiente_recojo` (valor antiguo de «recojo en oficina») NO
  se tocaría salvo que decidan lo contrario (P1).
- Mostrar «Entregado en oficina» cuando `shipment_status = entregado` y la modalidad es oficina (badge en `ordersCommon.tsx` y en el detalle).
- Filtro: separar «Recogido» (agencia) de «Entregado en oficina»; como en BD ambos son `entregado`, hay que añadir un valor de filtro compuesto
  (p. ej. `entregado_oficina`) en `order_view.go:325` o filtrar también por `delivery_mode`.
- Efectos colaterales a revisar: `dashboard.go:130,243` cuenta como «recogido con saldo» (cobranza) todo envío `entregado` con saldo > 0; los pedidos
  de oficina con saldo pasarían a ese conteo. `orders.go:473` y `:570` tratan `entregado` como envío cerrado (no editable desde el pedido).
- Pedidos ya existentes de oficina: backfill de datos (UPDATE, sin cambio de esquema).

**Migración de esquema:** no. **Backfill:** sí (datos), tenant por tenant; requiere decisión. Repos: backend + frontend_central.

### #2 Detalle en lista, un producto por línea
**Hoy:** el backend arma `Summary` como texto unido por « | » (`order_view.go:384-391`) y la celda lo corta con `max-w-xs truncate` (`OrdersTab.tsx:191`).
**Hacer:** añadir al backend un campo aditivo `summary_items: string[]` (sin romper `summary`) y en la celda renderizar una lista con un ítem por línea,
sin truncar. Sin migración. Backend (campo nuevo) + frontend_central. Riesgo: filas más altas con pedidos de muchos ítems; considerar tope visual con «+N más».

### #3 Lista de planes en 12 opciones
**Hoy:** `ListPlans` devuelve todos los `SaasPlan` activos ordenados por precio (`service/plans.go:14-18`); el editor muestra esos nombres y el campo
aparte «Meses» (`OrderEditor.tsx:523-545`); al elegir plan propone precio = precio mensual × meses (`OrderEditor.tsx:526-529`). Los planes de la lista actual
son filas de `saas_plans` (central), incluidos «1 MES GRATIS», «Trial», etc.

**Hacer (depende de P3):** opción A (solo panel/servicio, sin tocar los planes reales): el endpoint de planes devuelve las 12 combinaciones fijas
(«Microempresa plan mensual» → plan base + 1 mes, semestral → 6, anual → 12), con precio tomado de `saas_plans` + `saas_plan_cycles`
(descuento por ciclo, `pkg/database/migrations.go:228`). Opción B: crear/renombrar planes en central (altera el catálogo de venta de Tukifac; A es más segura).
Decidir qué pasa con líneas de pedidos antiguos con nombres viejos (el editor ya conserva la descripción antigua, `OrderEditor.tsx:533-534`)
y con los reportes que agrupan por `description` + `plan_months` (`reports.go:213-247`). Sin migración de esquema en A. Repos: backend + frontend_central.

### #4 «Yape» → serie y correlativo
**Hoy:** `EquipPayment` ya guarda `invoice_series`/`invoice_number` (`pkg/database/equip_models.go:170-172`) y el detalle los muestra **además** del método
(`OrderDetail.tsx:386-387`: método · momento, referencia, `p.invoice`). El formulario de cobro lo pide como «Comprobante emitido» opcional (`PaymentModal.tsx:158`,
formato `F002-59`). En la captura los dos cobros del pedido 191 no tienen comprobante cargado, por eso solo se ve «Yape».

**Pendiente de aclarar (P4):** ¿el comprobante debe *reemplazar* al método cuando exista? ¿Debe ser obligatorio, o completarse solo (p. ej. leyéndolo de la nota de venta o comprobante emitido en Tukifac)?
Mostrar el comprobante en lugar del método es un cambio trivial de UI (`OrderDetail.tsx:386`, `PaymentsTab.tsx:28`); si debe traerse solo, es un trabajo mayor.

### #5 Pedido-agencia: filtro por estado de pago
**Hoy:** `AgencyOrdersTab.tsx` ya pasa `OrderFilter` a `listOrders` y el backend ya filtra por `payment_status` (`order_view.go:325`; el mismo select existe en
`OrdersTab.tsx:146-149`). **Hacer:** copiar el `<select>` de pago a `AgencyOrdersTab.tsx` (junto al filtro de usuario, ~línea 63) con `setF({ payment_status })`.
Solo frontend_central, sin migración. Esfuerzo mínimo.

### #6 Retornos: vincular con Shalom, todos los estados y aviso
**Hoy (backend):** `SetReturnGuide` guarda guía+código, suscribe el webhook y trae el estado (`return_tracking.go:48-110`); `applyReturnProviderState` pasa de
`solicitado` a `en_camino` y emite avisos «llegó a destino» y «entregado» (`return_tracking.go:166-200`, `notifyReturnEvent`). Shalom entrega 7 estados
(registrado, origen, tránsito, demora, destino, reparto, entregado) y `returnStage` los **agrupa en 4** (En origen / En tránsito / En destino / Entregado; `return_tracking.go:27-39`).

**Hoy (pantalla):** el estado de Shalom es una insignia bajo la guía (`ReturnsTab.tsx:191-193`), mientras la columna «Estado» sigue siendo el estado interno
(Solicitado / En camino / Recibido…). Por eso en la captura sigue «Solicitado» aunque haya guía+código.

**Brechas probables:**
- (a) no se muestran los 7 estados de Shalom tal cual (demora, reparto, registrado);
- (b) la columna Estado no refleja Shalom;
- (c) no hay sincronización periódica: el único disparo masivo es el endpoint manual `SyncProviderShipments` (`handler/provider.go:162-170`) y el webhook solo aplica si está verificado (`return_tracking.go:158-162`);
- (d) `CreateReturn` copia al retorno la guía **del pedido** (`returns.go:180`), no la del retorno;
- (e) en la captura ninguno de los dos retornos muestra insignia ni botón de actualizar (este requiere `provider_code`, `ReturnsTab.tsx:198`).

**Hacer:** mostrar el estado de Shalom crudo (7 valores) en la columna Estado o junto a ella; confirmar si el webhook está verificado en producción y/o añadir un job periódico que llame a `syncOpenReturns`;
no copiar la guía del pedido al crear el retorno (o marcarla distinta). El aviso «ya está en agencia para recogerlo» ya existe (campana `EquiposNotificationsBell.tsx`); verificar que llega.
Sin migración de esquema (los campos `provider_*` ya existen en `EquipReturn`).
**Verificar antes de programar:** si el despliegue del 16-nov llegó a producción y por qué estos dos retornos no tienen estado de Shalom (código mal guardado, integración inactiva, webhook sin verificar).

### #7 Unificar «Gestionar» y ✔ Recibido (+ costo y observaciones)
**Hoy:** ✔ abre «Recibir retorno» solo con Buen estado/Dañado (`ReturnsTab.tsx:270-285`, función `receive` línea 137); «Gestionar» abre el modal de edición completo
(`ReturnsTab.tsx:227-258`) con Estado (incluye «Desechado por agencia»), Condición, Costo, Saldo no cobrado, Fecha de recepción y Observaciones.

**Hacer:** añadir Costo del retorno y Observaciones al modal de recepción y enviarlos junto con la recepción (`updateReturn` ya acepta `return_cost` y `notes`,
`equiposControl.service.ts:145`; sin cambios de backend). Quitar «Gestionar» **solo** si se acepta perder lo que solo ese modal permite: marcar «Desechado por agencia»,
editar el saldo no cobrado, cambiar la condición sin recibir, editar costo/observaciones antes de recibir. Decisión del usuario (P7). Solo frontend_central.

### #8 Equipos dañados en Stock
**Hoy:** un retorno recibido como `danado` NO genera movimiento de stock (`service/returns.go:270` solo crea el reingreso si `buen_estado`); solo queda la fila de `equip_returns`
con `condition='danado'`, `status='recibido'` y `items_json` (`[{product_id, code, quantity}]`). No existe un lugar que sume esos equipos; tampoco hay reparación/baja/salida de dañados.

**Hacer:** opción mínima, sin migración: en `StockReport` (`service/stock.go:83`, struct `StockRow` línea 61) añadir `damaged` calculado desde `equip_returns` (recibidos en estado dañado,
por producto) y una columna «Dañados» en `StockTab.tsx` (cabecera líneas 232-242). Es derivado: no se «resta» cuando el equipo se repara o se da de baja.
Opción completa (P8): un movimiento/ubicación «dañados» (p. ej. almacén «Dañados» o tipo de movimiento) con su flujo de salida (reparar → reingreso, o baja) — requiere migración versionada en `tenantmigrations`
y más diseño. Interacción con almacenes (fase 1): hay que decir dónde cuenta lo dañado.

### #9 «Ver inactivos» muestra también activos
**Hoy:** `CatalogTab.tsx:66` y `:70` filtran `(showInactive || p.active)`: marcar la casilla *suma* los inactivos a los activos (diseño intencional, casilla en `:150`).
La observación espera ver *solo* inactivos. **Hacer:** cambiar a un selector Activos / Inactivos / Todos (por defecto Activos), aplicado a productos y combos. Solo frontend_central.

### #10 Nombre del producto en el PDF de requerimiento
**Hoy:** `lineName` oculta el nombre cuando es igual al código (`warehouseDocs.ts:138`), y el alta de producto copia el código al nombre si va vacío (`service/catalog.go:78-79`);
en el catálogo el nombre solo aparece bajo el código si es distinto (`CatalogTab.tsx:181`). «EQUIPO PRUEBA» tiene nombre = código, por eso Producto sale vacío.
Lo mismo ocurre en la transferencia (`warehouseDocs.ts:170-171`).

**Hacer (según P10):** (a) mostrar siempre el nombre en el PDF aunque coincida con el código (lo más simple, repite el texto); o (b) exigir un nombre distinto al crear/importar productos y
corregir los existentes. Sin migración. Solo frontend_central en (a). Revisar que la observación no se refiera a otra pantalla (también podría ser el catálogo).

### #11 Rótulo: nombre/dirección completos de la agencia
**Hoy:** `shippingLabel.ts:112` imprime `destination_agency` con `maxLines = 1`, o sea una sola línea cortada; el campo trae la dirección completa de la agencia (visible en el detalle del pedido).
**Hacer:** permitir 3-4 líneas (y reducir tamaño de letra / auto-ajustar), dejando que el cuadro DESTINO crezca como ya hace el cuadro CONTENIDO (`shippingLabel.ts:9-31, 81`). El rótulo es 12×6 cm
(commit baefa5a): verificar que el alto siga cabiendo; si el texto es demasiado largo, truncar con «…» solo al final. Probar imprimiendo un PDF real con el caso del pedido 191.
Solo frontend_central, sin migración.

## 3. Plan priorizado

| Prio | Ítem | Esfuerzo | Riesgo | Repos / migración |
|---|---|---|---|---|
| 1 | #5 filtro de pago en Pedido-agencia | XS (≈1 h) | Nulo | frontend_central |
| 1 | #9 filtro inactivos (selector) | XS | Bajo | frontend_central |
| 1 | #10 nombre en PDF de requerimiento/transferencia | XS | Nulo | frontend_central |
| 1 | #11 rótulo con agencia completa | S (≈2-3 h, con prueba de impresión) | Bajo-medio (que no desborde 12×6) | frontend_central |
| 2 | #2 Detalle en lista (campo `summary_items`) | S (≈2 h) | Bajo | backend + frontend_central, sin migración |
| 2 | #7 Check de recibido con costo y observaciones (quitar Gestionar) | S (≈2 h) | Medio: pérdida de funciones de Gestionar | frontend_central |
| 2 | #4 comprobante en vez de Yape | XS si es solo mostrar; M si debe autocompletarse | Bajo / medio | frontend_central (+backend si auto) |
| 3 | #1 «Entregado en oficina» + filtro + backfill | M (≈1 día) | Medio: toca cobranza y dashboard; backfill por tenant | backend + frontend_central; sin migración de esquema, backfill de datos |
| 3 | #6 estados Shalom completos y sincronización | M (≈1 día) + verificación en producción | Medio: depende de Shalom real y del webhook | backend + frontend_central |
| 3 | #3 12 planes fijos | S-M (≈3-4 h con precios) | Medio: precios y reportes por plan | backend + frontend_central |
| 4 | #8 columna Dañados (derivada) | S (≈3 h) | Bajo | backend + frontend_central, sin migración |
| 4 | #8 flujo completo de dañados (reparar/baja) | L (varios días) | Medio-alto | backend + frontend_central + **migración versionada** |

**Orden de deploy** (backend primero): 1) backend (campo `summary_items`, `damaged`, filtro `entregado_oficina`, cambios de estado de oficina) → 2) backfill de pedidos de oficina por tenant (solo si se aprueba)
→ 3) frontend_central. No hay migración versionada obligatoria con la opción mínima de #8; con la completa, la migración va antes del backend. `frontend_tenant`, Tukichef y facturador_lycet no se tocan:
este módulo vive solo en panel central + backend.

## 4. Preguntas y decisiones para el usuario

- **P1 (#1):** «vendido en oficina»: ¿es solo `oficina` (ya entregado en el momento) o también `pendiente_recojo`? ¿Automático al registrar/confirmar el pedido, o al cobrarlo totalmente?
  ¿Se corrigen en bloque los pedidos de oficina antiguos que hoy están en Por despachar / En agencia? ¿Aceptan que los pedidos de oficina con saldo entren al conteo de «recogidos con deuda»?
  ¿La etiqueta «Recogido» (agencia) se mantiene?
- **P3 (#3):** ¿las 12 opciones son un selector único (que reemplaza plan + «Meses» con 1/6/12) o solo una lista de nombres? ¿Con qué precio (precio de lista mensual × meses, con el descuento por ciclo ya
  configurado en central)? ¿Se desactivan en central los planes sobrantes (1 mes gratis, Trial, Ilimitado, Pro…) o solo se ocultan en este selector? ¿Qué pasa con «Microempresa PRO»?
- **P4 (#4):** ¿el comprobante reemplaza a «Yape» o se muestra junto a él? ¿Debe ser obligatorio al cobrar? ¿Los cobros del pedido 191 tienen un comprobante emitido que no se cargó, o quieren traerlo automáticamente?
- **P6 (#6):** ¿«todos los estados de Shalom» son los 7 de Shalom tal cual? ¿Dónde van: columna Estado o la de guía? ¿La guía de esos retornos es la del retorno o la del pedido de ida?
  ¿Está verificado el webhook de Shalom en producción? ¿Quieren sincronización automática cada X minutos?
- **P7 (#7):** ¿se elimina el botón Gestionar? Entonces, ¿cómo se marcaría «Desechado por agencia», se editaría el saldo no cobrado o se corregiría un retorno ya recibido?
  Propuesta: dejar Gestionar solo para esos casos (oculto en un menú) o aceptar perderlos.
- **P8 (#8):** ¿basta una columna informativa «Dañados» (acumulado de retornos dañados recibidos) o necesitan controlar qué pasa con ellos (reparar, dar de baja, devolver al proveedor)? ¿En qué almacén cuentan?
- **P9 (#9):** ¿«Ver inactivos» debe mostrar solo inactivos, o prefieren un selector Activos / Inactivos / Todos?
- **P10 (#10):** ¿qué debe aparecer en «Producto» cuando el nombre es igual al código (repetir el código, o exigir nombres distintos al crear)? ¿Aplica igual a las transferencias y al catálogo?
- **P11 (#11):** ¿qué debe ir en el rótulo exactamente: la dirección completa de la agencia (como en el detalle)? ¿Cuántas líneas aceptan en 12×6 cm?
- **General:** ¿las capturas son de producción? Si lo son, ¿ya estaban desplegados los commits `8ea293e` / `3ba7175`?

## 5. Qué NO se verificó
- No se probó nada en vivo (ni local, ni beta, ni producción): es análisis estático de código y de las capturas.
- No se consultó producción: no se sabe qué versión corre ni los estados reales de Shalom de los retornos de la captura.
- No se verificaron los datos de `saas_plans` (nombres/precios reales) ni cuántos pedidos de oficina existen para el backfill.
- Ningún cambio de este módulo toca facturación electrónica SUNAT, pero #1 debe revisarse contra cobranza.
