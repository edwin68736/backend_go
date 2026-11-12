# Exportación de servicios a clientes del exterior (operaciones 0201, 0202 y 0208)

Rama `feature/exportacion-servicios` (backend, frontend Tukifac y facturador Lycet). Nada de esto está en `main`.

## Qué resuelve
Hasta ahora la factura exigía RUC. Una empresa que presta un servicio a alguien del exterior (un hotel con un huésped extranjero, una consultoría
con un cliente en EE. UU.) no podía facturar. Esta rama agrega las **exportaciones de servicios**: IGV 0 %, sin RUC del adquirente.

| Operación (catálogo 51) | Para qué | Dato que pide el comprobante |
|---|---|---|
| **0201** | Servicio prestado íntegramente en el país a un no domiciliado | País donde se usa el servicio |
| **0202** | Hospedaje (con alimentación) a no domiciliados | Huésped, documento, países, ingreso al país, ingreso/salida del hotel |
| **0208** | Servicio realizado parcialmente en el extranjero | País donde se usa el servicio |

No incluye la exportación de bienes (0200: requiere DAM e Incoterm) ni la 0401 (ver «Pendiente»).

## Cómo se usa
1. **Ajustes → Comprobantes → Exportación de servicios**: activar el interruptor (apagado por defecto; `tenant_company_configs.exports_enabled`).
2. **Nuevo comprobante** (factura o boleta): en «Tipo operación» aparecen tres opciones «Exportación – …».
3. Un solo recuadro con los datos de esa operación. Las líneas pasan solas a **afectación 40** (IGV 0) y la factura no exige RUC.

## Base legal (leída de fuentes oficiales, 12-nov-2026)
- Ley del IGV (TUO, DS 055-99-EF) **art. 33.4**: el hospedaje con alimentación a no domiciliados es exportación si no pasa de **60 días** por cada ingreso al país y
  se presenta el pasaporte. DS 122-2001-EF (modificado por DS 199-2013-EF y 342-2017-EF): hotel inscrito en el Registro Especial de SUNAT y con certificado de
  categorización; leyenda **«EXPORTACIÓN DE SERVICIOS - DECRETO LEGISLATIVO Nº 919»**; solo hospedaje y alimentación en esa factura; si no califica, se restituye el IGV.
- Reglamento de Comprobantes de Pago, art. 4.1.1 d) y 4.1.2: la factura de exportación se emite sin RUC. RS 097-2012 art. 17.2 y anexo de la RS 123-2022:
  el adquirente se identifica con **«-»**.
- Catálogo 51 (0201/0202/0208) y catálogo 55 «Beneficio Hospedajes» 4000–4009 (anexo de la RS 108-2026); país de uso del servicio: campo 51 de la guía XML.

## Qué se envía al facturador
- `tipoOperacion` = 0201/0202/0208; líneas con `tipAfeIgv` 40 (tributo 9995 EXP); en **factura** el adquirente es `tipoDoc "-" / numDoc "-"` (la boleta conserva su identificación).
- **0202**: por línea, `atributos` 4000–4009 (países, fechas a mediodía hora Perú, días, huésped); razón social = nombre del huésped; leyenda del DL 919 en el PDF.
- **0201/0208**: atributo reservado `EXP_PAIS_USO`, que **Lycet** convierte en `/Invoice/cac:Delivery/cac:DeliveryLocation/cac:Address/cac:Country` (`TukifacInvoiceBuilder::injectExportCountry`).

## Orden de despliegue
Backend (migración **V186**: `tenant_sale_exports` + `exports_enabled`) → facturador Lycet (otro VPS) → frontend. Sin el cambio de Lycet, 0201/0208 enviarían
el atributo reservado como propiedad del ítem y SUNAT lo rechazaría; 0202 funciona sin tocar Lycet.

## Verificado
- Go: reglas de validación, venta (0201/0202, IGV 0, rechazos), payload, fechas, migración V186 en MariaDB y por el runner de la flota.
- PHP (Lycet): XML con país de uso, orden UBL, atributos 4000–4009, XML idéntico a Greenter sin atributo reservado; deserialización JSON → JMS → XML con el contrato real.
- En pantalla (local): activar el interruptor, emitir boleta 0202 y facturas 0201/0202; el payload capturado hacia el facturador sale como se describe arriba.

## Pendiente
1. **Probar en el ambiente beta de SUNAT** (no se hizo): confirmar el adquirente «-», la leyenda y los atributos; en boleta se mantiene la identificación del cliente (a confirmar).
2. Notas de crédito/débito sobre una exportación.
3. Reportes contables (registro de ventas / PLE / SIRE) para ventas de exportación.
4. Boleta 0202/0201: confirmar con SUNAT el tipo de documento del adquirente.
5. Decidir qué hacer con la operación **0401** existente (factura sin RUC a no domiciliados): no se encontró norma que la respalde y nunca se emitió con éxito en producción.
6. Antes de que un hotel real use el 0202: inscripción en el Registro Especial de SUNAT, certificado de categorización y archivo de copias del pasaporte/TAM.
7. En el XAMPP local no se probó MySQL 8 (producción): V186 es un `AutoMigrate` de una tabla nueva y una columna.
