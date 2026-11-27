# Cierre, punto 1: factura y boleta de un plato con receta

27-nov-2026. Rama `insumos-recetas`. Sin desplegar. Sin cambios de código.

## Cómo se probó
Todo desde las pantallas, en local, contra **SUNAT beta**: empresa, clientes y platos inventados; certificado y credenciales de
prueba; facturador local con su cola y su aviso de estado al sistema, con las direcciones de producción bloqueadas.
Hamburguesa con receta (150 g de carne y 1 pan) y chicha sin receta.

## Resultado
Los nueve comprobantes quedaron **aceptados** (CDR código 0).

| Comprobante | Desde | Cliente | Módulo | Insumos | Costo guardado |
|---|---|---|---|---|---|
| B001-1 | Tukichef, punto de venta | DNI | encendido | 0.3 kg y 2 panes | 7.60 |
| F001-1 | Tukichef, punto de venta | RUC | encendido | 0.15 kg y 1 pan (la chicha nada) | 3.80 |
| B001-2 | Tukichef, mesa con comanda | Clientes Varios | encendido | 0.3 kg y 2 panes, una sola vez y al cobrar | 7.60 |
| FC01-1 | nota de crédito total de F001-1 | RUC | encendido | devueltos los de F001-1 | — |
| BC01-1 | nota de crédito total de B001-2 | Clientes Varios | encendido | devueltos los de B001-2 | — |
| B001-3 | igual que B001-1 | DNI | apagado | ninguno | vacío |
| F001-2 | igual que F001-1 | RUC | apagado | ninguno | vacío |
| F001-3 | Tukifac, nuevo comprobante | RUC | encendido | 0.15 kg y 1 pan | 3.80 |
| B001-4 | Tukifac, punto de venta | Clientes Varios | encendido | 0.15 kg y 1 pan | 3.80 |

Revisado en el `payload_json` guardado y en el XML firmado:
- tipo de operación 0101, tipo y número de documento del cliente, líneas, importes, IGV y leyenda correctos;
- ningún insumo aparece en el comprobante;
- B001-1 contra B001-3 y F001-1 contra F001-2 (módulo encendido contra apagado): **idénticos salvo el correlativo**;
- las notas de crédito llevan motivo 01, el comprobante afectado y las mismas líneas e importes.

Existencias y reporte de rentabilidad cuadran: lo anulado no cuenta y lo vendido con el módulo apagado sale «sin costo».
Test automático del comprobante idéntico (`TestRecetas_comprobanteIdentico`, boleta con plato y combo): pasa.

## Observado en el facturador (no es del módulo, ya está en `main`)
En este Windows `wkhtmltopdf` falla cuando lo llama PHP. Eso mostró que, si el PDF falla justo después de que SUNAT responde, el
facturador no guarda el XML ni el CDR y reintenta el envío (`FiscalEmitProcessor`, generación del PDF antes de guardar). En la copia
de prueba se desactivó ese fallo para poder continuar; el primer envío de B001-1 necesitó un reintento. En producción no se revisó.

## Sin probar
- SUNAT producción. El primer comprobante real tras desplegar se revisa (payload y CDR) antes de avisar a los clientes.
- MySQL 8.0.46 (se usó 8.0.30 local).
- Con factura o boleta: combos, presentaciones, descuento, dólares, crédito, varios métodos de pago, dividir cuenta, delivery y mozo.
- Nota de crédito parcial en pantalla (probada en el backend), anulación por comunicación de baja o resumen diario.
- PDF del facturador e impresión en papel.
