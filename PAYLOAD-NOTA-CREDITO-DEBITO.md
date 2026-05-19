# Payload y respuestas: Nota de Crédito y Nota de Débito (frontend)

Documentación para implementar en el frontend el **envío**, la **obtención de XML** y la **obtención de PDF** de notas de crédito y notas de débito. Incluye los datos exactos que espera el backend y las respuestas exactas de cada endpoint.

---

## Endpoints

| Acción | Método y ruta | Respuesta |
|--------|----------------|-----------|
| **Enviar a SUNAT** | `POST /api/v1/note/send?token=TU_TOKEN` | JSON con `xml`, `hash`, `sunatResponse` (misma estructura que factura/boleta). |
| **Obtener solo XML** | `POST /api/v1/note/xml?token=TU_TOKEN` | Archivo XML (binario). `Content-Type: text/xml`. |
| **Obtener solo PDF** | `POST /api/v1/note/pdf?token=TU_TOKEN` | Archivo PDF (binario). `Content-Type: application/pdf`. |

- El **mismo body** (mismo JSON de la nota) se usa en los tres endpoints. Para PDF y XML no se envía a SUNAT; solo se genera el archivo.
- El **RUC** de la empresa se toma de **`company.ruc`** del body (debe existir en `data/empresas.json` o en la configuración por defecto).
- Si usas **multi-empresa**, puedes enviar `?ruc=RUC` en la query para indicar con qué empresa operar (según implementación del backend).

---

## Tipo de comprobante: Nota de Crédito vs Nota de Débito

| Comprobante | **tipoDoc** | Uso |
|-------------|-------------|-----|
| **Nota de crédito** | `"07"` | Anular o reducir importe de una factura/boleta (devolución, descuento, etc.). |
| **Nota de débito** | `"08"` | Aumentar el importe de una factura/boleta (intereses, gastos, etc.). |

---

## Documento afectado (comprobante que se corrige)

La nota **siempre** referencia al menos un comprobante afectado (la factura o boleta que se está corrigiendo). Se envía en **`relDocs`** (documentos relacionados):

| Campo | Tipo | Descripción |
|-------|------|-------------|
| **relDocs** | array | Lista de documentos afectados. Cada elemento: `{ "tipoDoc": "01" \| "03", "nroDoc": "SERIE-NUMERO" }`. |
| **tipoDoc** (dentro de relDocs) | string | Tipo del comprobante afectado: `"01"` Factura, `"03"` Boleta. |
| **nroDoc** (dentro de relDocs) | string | Número del comprobante afectado en formato **serie-número** (ej. `"F001-1"`, `"B001-25"`). |

Opcionalmente el backend puede aceptar también (según esquema del API):

- **tipDocAfectado**: tipo del documento afectado (`"01"` o `"03"`).
- **numDocfectado**: número del documento afectado (serie-correlativo). *Nota: en el esquema aparece como `numDocfectado` (typo); si existe en el backend, usar ese nombre.*

Para el frontend es suficiente y recomendable usar **`relDocs`** con al menos un elemento.

---

## Motivo de la nota

SUNAT exige código y descripción del motivo (catálogo de motivos de nota de crédito/débito):

| Campo | Tipo | Descripción |
|-------|------|-------------|
| **codMotivo** | string | Código del motivo (catálogo SUNAT). Ej. `"01"` Anulación de la operación, `"02"` Anulación por error en el RUC`, etc. |
| **desMotivo** | string | Descripción del motivo (texto libre o según catálogo). |

Debes enviar ambos. Consulta el **catálogo de motivos** en la guía UBL 2.1 de SUNAT (Nota de crédito / Nota de débito).

---

## Campos obligatorios comunes (resumen)

Además de los de factura/boleta (company, client, details, totales, leyendas, etc.), para la nota son **obligatorios**:

| Campo | Tipo | Descripción |
|-------|------|-------------|
| **tipoDoc** | string | `"07"` Nota de crédito, `"08"` Nota de débito. |
| **serie** | string | Serie del comprobante (ej. `"FC01"`, `"FD01"`). |
| **correlativo** | string | Número correlativo. |
| **fechaEmision** | string | Fecha y hora ISO 8601 (ej. `"2026-03-08T12:00:00-05:00"`). |
| **company** | object | Emisor (`ruc`, `razonSocial`, `nombreComercial`, `address`). |
| **client** | object | Cliente (`tipoDoc`, `numDoc`, `rznSocial`, `address`). |
| **tipoMoneda** | string | Moneda (ej. `"PEN"`). |
| **relDocs** | array | Al menos un documento afectado: `{ "tipoDoc": "01" \| "03", "nroDoc": "SERIE-NUMERO" }`. |
| **codMotivo** | string | Código del motivo. |
| **desMotivo** | string | Descripción del motivo. |
| **details** | array | Ítems (al menos uno). Misma estructura que en factura/boleta. Cada ítem debe llevar **tipAfeIgv** según afectación (10 gravado, 20 exonerado, 30 inafecto). Si hay exonerados/inafectos, ver sección *Tributos e IGV* más abajo. |
| **legends** | array | Leyendas (código 1000 = monto en letras). |
| **mtoOperGravadas** | number | Total operaciones gravadas. |
| **mtoIGV** | number | IGV. |
| **totalImpuestos** | number | Total tributos. |
| **valorVenta** | number | Valor de venta. |
| **subTotal** | number | Subtotal. |
| **mtoImpVenta** | number | Monto total de la nota. |

---

## Payload completo: Nota de Crédito (tipoDoc 07)

Ejemplo que referencia una **factura** `F001-1` como documento afectado:

```json
{
  "ublVersion": "2.1",
  "tipoDoc": "07",
  "serie": "FC01",
  "correlativo": "1",
  "fechaEmision": "2026-03-08T12:00:00-05:00",
  "formaPago": {
    "tipo": "Contado"
  },
  "company": {
    "ruc": "20161515648",
    "razonSocial": "MI EMPRESA S.A.C.",
    "nombreComercial": "MI EMPRESA S.A.C.",
    "address": {
      "ubigueo": "150131",
      "codigoPais": "PE",
      "departamento": "LIMA",
      "provincia": "LIMA",
      "distrito": "SAN ISIDRO",
      "urbanizacion": "-",
      "direccion": "AV. EJEMPLO 123"
    }
  },
  "client": {
    "tipoDoc": "6",
    "numDoc": "20100000001",
    "rznSocial": "CLIENTE EJEMPLO S.A.C.",
    "address": {
      "ubigueo": "150101",
      "codigoPais": "PE",
      "departamento": "LIMA",
      "provincia": "LIMA",
      "distrito": "LIMA",
      "direccion": "AV. CLIENTE 456"
    }
  },
  "tipoMoneda": "PEN",
  "codMotivo": "01",
  "desMotivo": "Anulación de la operación",
  "relDocs": [
    {
      "tipoDoc": "01",
      "nroDoc": "F001-1"
    }
  ],
  "mtoOperGravadas": 84.75,
  "mtoIGV": 15.25,
  "totalImpuestos": 15.25,
  "valorVenta": 84.75,
  "subTotal": 100.00,
  "mtoImpVenta": 100.00,
  "details": [
    {
      "unidad": "NIU",
      "cantidad": 1,
      "codProducto": "P001",
      "descripcion": "Producto ejemplo (nota de crédito)",
      "mtoValorUnitario": 84.75,
      "mtoValorVenta": 84.75,
      "tipAfeIgv": "10",
      "mtoBaseIgv": 84.75,
      "porcentajeIgv": 18,
      "igv": 15.25,
      "totalImpuestos": 15.25,
      "mtoPrecioUnitario": 100.00
    }
  ],
  "legends": [
    {
      "code": "1000",
      "value": "SON CIEN CON 00/100 SOLES"
    }
  ]
}
```

---

## Payload completo: Nota de Débito (tipoDoc 08)

Ejemplo que referencia una **boleta** `B001-30` como documento afectado:

```json
{
  "ublVersion": "2.1",
  "tipoDoc": "08",
  "serie": "FD01",
  "correlativo": "1",
  "fechaEmision": "2026-03-08T14:00:00-05:00",
  "formaPago": {
    "tipo": "Contado"
  },
  "company": {
    "ruc": "20161515648",
    "razonSocial": "MI EMPRESA S.A.C.",
    "nombreComercial": "MI EMPRESA S.A.C.",
    "address": {
      "ubigueo": "150131",
      "codigoPais": "PE",
      "departamento": "LIMA",
      "provincia": "LIMA",
      "distrito": "SAN ISIDRO",
      "urbanizacion": "-",
      "direccion": "AV. EJEMPLO 123"
    }
  },
  "client": {
    "tipoDoc": "1",
    "numDoc": "12345678",
    "rznSocial": "JUAN PEREZ",
    "address": {
      "ubigueo": "150101",
      "codigoPais": "PE",
      "departamento": "LIMA",
      "provincia": "LIMA",
      "distrito": "LIMA",
      "direccion": "JR. CLIENTE 789"
    }
  },
  "tipoMoneda": "PEN",
  "codMotivo": "02",
  "desMotivo": "Intereses por mora",
  "relDocs": [
    {
      "tipoDoc": "03",
      "nroDoc": "B001-30"
    }
  ],
  "mtoOperGravadas": 8.47,
  "mtoIGV": 1.53,
  "totalImpuestos": 1.53,
  "valorVenta": 8.47,
  "subTotal": 10.00,
  "mtoImpVenta": 10.00,
  "details": [
    {
      "unidad": "NIU",
      "cantidad": 1,
      "codProducto": "INTERES",
      "descripcion": "Intereses por mora",
      "mtoValorUnitario": 8.47,
      "mtoValorVenta": 8.47,
      "tipAfeIgv": "10",
      "mtoBaseIgv": 8.47,
      "porcentajeIgv": 18,
      "igv": 1.53,
      "totalImpuestos": 1.53,
      "mtoPrecioUnitario": 10.00
    }
  ],
  "legends": [
    {
      "code": "1000",
      "value": "SON DIEZ CON 00/100 SOLES"
    }
  ]
}
```

---

## Tributos e IGV: productos gravados, exonerados e inafectos (rechazo SUNAT 2638 / 3105)

SUNAT exige que **por cada tipo de tributo/afectación usado en las líneas del comprobante exista un total de ese tributo en el resumen del XML** (bloques `cac:TaxTotal` / `cac:TaxSubtotal`). Si en alguna línea usas un tipo de afectación distinto del gravado (IGV 17%), debes declarar el monto total de ese tributo a nivel de resumen. Esto aplica igual para **notas de crédito y notas de débito**.

### Qué significa el error de SUNAT

- **"El XML debe contener al menos un tributo por línea de afectación por IGV"** o **código 2638 / 3105**: indica que en el XML hay líneas con `tipAfeIgv` exonerado (`"20"`), inafecto (`"30"`), etc., pero **falta el nodo (tag) con el monto total de ese tributo** en la sección de resumen de impuestos, **o bien** que **cada línea (InvoiceLine) no tiene su bloque de tributo** (cac:TaxTotal por línea). SUNAT espera tantos bloques en resumen como tipos de tributo y **cada línea debe contener al menos un tributo** acorde a su afectación.

### Por línea: mtoBaseIgv para que exista tributo en cada línea (evitar 3105)

Para que el XML tenga **al menos un tributo por línea** y no rechace con 3105, cada ítem en `details[]` debe enviar **`mtoBaseIgv`** con el valor de venta de la línea (`mtoValorVenta`), **también para exonerado (20) e inafecto (30)** — no enviar 0. Así el generador incluye un TaxSubtotal por línea (con base = valor de la línea y monto de impuesto = 0 para 20/30). Ver el mismo criterio en [PAYLOAD-FACTURA-BOLETA.md](PAYLOAD-FACTURA-BOLETA.md).

### Tipos de afectación del IGV (Catálogo N° 07)

| Código | Descripción |
|--------|-------------|
| **10** | Gravado - Operación Onerosa |
| **20** | Exonerado - Operación Onerosa |
| **30** | Inafecto - Operación Onerosa |
| **40** | Exportación |
| Otros | Según catálogo SUNAT vigente |

### Qué debe enviar el frontend cuando hay exonerados o inafectos

1. **Por línea (`details[]`):** cada ítem debe llevar el `tipAfeIgv` correcto (`"10"`, `"20"`, `"30"`, etc.) y los montos coherentes: **gravado (10)** con `mtoBaseIgv`, `porcentajeIgv`, `igv`, `totalImpuestos`; **exonerado (20) / inafecto (30)** con `mtoBaseIgv` = valor de venta de la línea (no 0), `igv` y `totalImpuestos` = 0, `porcentajeIgv` = 0.

2. **Totales a nivel de comprobante:** además de `mtoOperGravadas` y `mtoIGV`, cuando existan operaciones exoneradas o inafectas deben declararse **`mtoOperExoneradas`** y **`mtoOperInafectas`** (el backend/Lycet los envía cuando hay líneas 20/30). Los totales globales (`valorVenta`, `subTotal`, `mtoImpVenta`, `totalImpuestos`) deben cuadrar.

3. **Verificación del XML:** si SUNAT rechaza con 2638/3105, revisar el XML generado y comprobar que existan **tantos bloques `cac:TaxTotal` como tipos de tributo** usados en las líneas (p. ej. uno para IGV gravado y otro para exonerado/inafecto).

En resumen: **si alguna línea tiene `tipAfeIgv` exonerado o inafecto, el XML debe incluir el tag del total de ese tributo en el resumen; de lo contrario SUNAT rechaza la nota.** Ver también [PAYLOAD-FACTURA-BOLETA.md](PAYLOAD-FACTURA-BOLETA.md) para el detalle en factura/boleta.

---

## Respuesta exacta de cada endpoint

### 1. `POST /api/v1/note/send` — Enviar a SUNAT

- **Content-Type de la petición:** `application/json`.
- **Body:** el JSON de la nota (como en los ejemplos anteriores).

**Respuesta:**

- **HTTP 200** siempre que el backend procese la petición y envíe (o intente enviar) el XML a SUNAT. El cuerpo es **JSON** con la misma estructura que para factura/boleta (ver [RESPUESTA-SUNAT-BACKEND.md](RESPUESTA-SUNAT-BACKEND.md)):

```json
{
  "xml": "<string con el XML firmado enviado a SUNAT>",
  "hash": "<resumen de la firma digital del XML>",
  "sunatResponse": {
    "success": true,
    "error": null,
    "cdrZip": "UEsDBBQAAAAI...",
    "cdrResponse": {
      "accepted": true,
      "id": "...",
      "code": "0",
      "description": "Aceptado",
      "notes": []
    }
  }
}
```

- Si SUNAT **rechaza** o hay **error de conexión**, sigue siendo **HTTP 200**; debes revisar **`sunatResponse.success`**, **`sunatResponse.cdrResponse`** y **`sunatResponse.error`** como se describe en [RESPUESTA-SUNAT-BACKEND.md](RESPUESTA-SUNAT-BACKEND.md). No se devuelve PDF ni XML como archivo en este endpoint.

---

### 2. `POST /api/v1/note/xml` — Obtener solo el XML

- **Content-Type de la petición:** `application/json`.
- **Body:** el **mismo** JSON de la nota.

**Respuesta:**

- **HTTP 200**: cuerpo = archivo **XML** (binario / texto XML).
- **Headers:**
  - `Content-Type: text/xml`
  - `Content-Disposition: attachment; filename="FC01-1.xml"` (o el nombre que corresponda al comprobante, ej. `FD01-1.xml`).

En el frontend: leer la respuesta como **texto** o **blob**, y mostrarla, guardarla o enviarla a otro servicio. No es JSON.

---

### 3. `POST /api/v1/note/pdf` — Obtener solo el PDF

- **Content-Type de la petición:** `application/json`.
- **Body:** el **mismo** JSON de la nota.

**Respuesta:**

- **HTTP 200**: cuerpo = archivo **PDF** (binario).
- **Headers:**
  - `Content-Type: application/pdf`
  - `Content-Disposition: attachment; filename="FC01-1.pdf"` (o el nombre que corresponda, ej. `FD01-1.pdf`).

En el frontend: tratar la respuesta como **blob** y usar la URL generada (ej. `URL.createObjectURL(blob)`) como `src` de un iframe o enlace para visualizar o descargar el PDF. No es JSON.

---

## Errores HTTP (validación o autorización)

Si el backend **no** llega a generar ni enviar el comprobante (validación, token, empresa no encontrada), responde con **4xx** y un **JSON** (sin `xml` ni `sunatResponse`):

**400 – Datos inválidos o faltantes**

```json
{
  "error": "Faltan datos obligatorios para el comprobante.",
  "campos_requeridos": ["campo1", "campo2"],
  "mensaje": "Descripción adicional si existe."
}
```

**401 / 403** – Token inválido o no enviado (según implementación).

**404** – RUC de la empresa no registrado (según implementación).

En todos los 4xx el cuerpo es JSON; el frontend debe interpretarlo como error de validación o autorización, no como respuesta de SUNAT.

---

## Resumen para el frontend

| Qué hacer | Endpoint | Body | Respuesta |
|-----------|----------|------|-----------|
| Enviar nota a SUNAT | `POST /api/v1/note/send?token=...` | JSON nota (tipoDoc 07 o 08, relDocs, codMotivo, desMotivo, company, client, details, totales, legends) | JSON: `xml`, `hash`, `sunatResponse` (éxito/rechazo/error conexión). |
| Obtener XML | `POST /api/v1/note/xml?token=...` | Mismo JSON | Archivo XML (`Content-Type: text/xml`, `Content-Disposition` con nombre). |
| Obtener PDF | `POST /api/v1/note/pdf?token=...` | Mismo JSON | Archivo PDF (`Content-Type: application/pdf`, `Content-Disposition` con nombre). |

- **Nota de crédito:** `tipoDoc: "07"`. **Nota de débito:** `tipoDoc: "08"`.
- **relDocs:** al menos un elemento con el comprobante afectado: `{ "tipoDoc": "01" | "03", "nroDoc": "SERIE-NUMERO" }`.
- **codMotivo** y **desMotivo** son obligatorios; usar códigos y descripciones según catálogo SUNAT.
- La estructura detallada de **sunatResponse** (aceptado, rechazado, sin conexión) es la misma que para factura/boleta; ver [RESPUESTA-SUNAT-BACKEND.md](RESPUESTA-SUNAT-BACKEND.md).
