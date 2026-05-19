# Comunicación de baja y Resumen de boletas — Endpoints y respuestas

Documentación de los endpoints **implementados** en el backend para **comunicación de baja** (dar de baja comprobantes) y **resumen diario de boletas**, con los datos que espera el backend y las respuestas exactas de cada endpoint.

---

# Parte 1: Comunicación de baja (voided)

Sirve para **dar de baja** facturas, boletas, notas de crédito/débito que ya no deben considerarse vigentes (anulación, error, etc.). SUNAT recibe el XML y puede devolver un ticket para consultar el estado después, o el CDR directamente según el caso.

## Endpoints implementados

| Acción | Método y ruta | Respuesta |
|--------|----------------|-----------|
| **Enviar a SUNAT** | `POST /api/v1/voided/send?token=TU_TOKEN` | JSON: `xml`, `hash`, `sunatResponse` (BillResult: aceptado/rechazo o ticket). |
| **Solo XML** | `POST /api/v1/voided/xml?token=TU_TOKEN` | Archivo XML. `Content-Type: text/xml`. |
| **Solo PDF** | `POST /api/v1/voided/pdf?token=TU_TOKEN` | Archivo PDF. `Content-Type: application/pdf`. |
| **Estado del ticket** | `GET /api/v1/voided/status?ticket=TICKET&ruc=RUC` | JSON: estado y, si SUNAT ya respondió, CDR en base64 y datos parseados. |

- **ruc** en `GET /voided/status` es opcional; se usa cuando hay varias empresas en `data/empresas.json`.
- Para **PDF** y **XML** se envía el **mismo body** que para `/send`; no se envía a SUNAT, solo se genera el archivo.

## Payload: Comunicación de baja (Voided)

El body debe ser un JSON con la estructura que espera el modelo **Voided** (emisor, fecha de comunicación y lista de comprobantes a dar de baja).

| Campo | Tipo | Descripción |
|-------|------|-------------|
| **company** | object | Emisor: `ruc`, `razonSocial`, `nombreComercial`, `address` (igual que en factura). |
| **correlativo** | string | Correlativo del resumen de bajas (ej. `"1"`). |
| **fecGeneracion** | string | Fecha de generación del resumen (ISO 8601). |
| **fecComunicacion** | string | Fecha de comunicación (ISO 8601). |
| **details** | array | Lista de comprobantes a dar de baja. Cada elemento: ver abajo. |

Cada elemento de **details** (VoidedDetail):

| Campo | Tipo | Descripción |
|-------|------|-------------|
| **tipoDoc** | string | Tipo de comprobante: `"01"` Factura, `"03"` Boleta, `"07"` Nota de crédito, `"08"` Nota de débito. |
| **serie** | string | Serie (ej. `"F001"`, `"B001"`). |
| **correlativo** | string | Número correlativo del comprobante. |
| **desMotivoBaja** | string | Motivo de la baja (texto descriptivo). |

### Ejemplo de payload (comunicación de baja)

```json
{
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
  "correlativo": "1",
  "fecGeneracion": "2026-03-08T12:00:00-05:00",
  "fecComunicacion": "2026-03-08T12:00:00-05:00",
  "details": [
    {
      "tipoDoc": "01",
      "serie": "F001",
      "correlativo": "1",
      "desMotivoBaja": "Error en datos del comprobante"
    }
  ]
}
```

## Respuestas exactas — Comunicación de baja

### POST /api/v1/voided/send

- **Content-Type de la petición:** `application/json`.
- **Respuesta HTTP 200:** JSON con la misma estructura que para factura/boleta (ver [RESPUESTA-SUNAT-BACKEND.md](RESPUESTA-SUNAT-BACKEND.md)):

```json
{
  "xml": "<XML firmado enviado a SUNAT>",
  "hash": "<hash de la firma>",
  "sunatResponse": {
    "success": true,
    "error": null,
    "cdrZip": "<base64 del ZIP del CDR si SUNAT acepta>",
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

Si SUNAT devuelve **ticket** en lugar de CDR inmediato, en el frontend se debe llamar a **GET /api/v1/voided/status?ticket=...** para consultar el estado y, cuando esté listo, obtener el CDR.

### POST /api/v1/voided/xml

- **Body:** mismo JSON de la comunicación de baja.
- **Respuesta HTTP 200:** cuerpo = archivo **XML**; headers `Content-Type: text/xml` y `Content-Disposition: attachment; filename="RA-20260308-1.xml"` (o similar).

### POST /api/v1/voided/pdf

- **Body:** mismo JSON de la comunicación de baja.
- **Respuesta HTTP 200:** cuerpo = archivo **PDF**; headers `Content-Type: application/pdf` y `Content-Disposition: attachment; filename="..."`.

### GET /api/v1/voided/status

- **Parámetros de query:**
  - **ticket** (obligatorio): número de ticket devuelto por SUNAT al enviar la comunicación de baja.
  - **ruc** (opcional): RUC de la empresa cuando hay multi-empresa.

- **Respuesta HTTP 200:** JSON con el estado y, si SUNAT ya procesó, el CDR:

```json
{
  "success": true,
  "error": null,
  "code": "0",
  "cdrZip": "<base64 del ZIP del CDR>",
  "cdrResponse": {
    "accepted": true,
    "id": "...",
    "code": "0",
    "description": "Aceptado",
    "notes": []
  }
}
```

- Si el ticket aún no está procesado o hay error, **success** será `false` y puede venir **error** con `code` y `message`; **cdrZip** y **cdrResponse** pueden ser `null`.

- **Respuesta HTTP 400** si falta el ticket: `{ "message": "Ticket Requerido" }`.

---

# Parte 2: Resumen diario de boletas (summary)

Sirve para enviar a SUNAT el **resumen diario** de comprobantes (boletas, notas, etc.). SUNAT devuelve un **ticket**; con ese ticket se consulta el estado hasta que SUNAT procese y se pueda descargar el CDR.

## Endpoints implementados

| Acción | Método y ruta | Respuesta |
|--------|----------------|-----------|
| **Enviar a SUNAT** | `POST /api/v1/summary/send?token=TU_TOKEN` | JSON: `xml`, `hash`, `sunatResponse` con **ticket** (SummaryResult). |
| **Solo XML** | `POST /api/v1/summary/xml?token=TU_TOKEN` | Archivo XML. `Content-Type: text/xml`. |
| **Solo PDF** | `POST /api/v1/summary/pdf?token=TU_TOKEN` | Archivo PDF. `Content-Type: application/pdf`. |
| **Estado del ticket** | `GET /api/v1/summary/status?ticket=TICKET&ruc=RUC` | JSON: estado y, si SUNAT ya respondió, **cdrZip** (base64) y **cdrResponse**. |

- **ruc** en `GET /summary/status` es opcional (multi-empresa).
- Tras **POST /summary/send** hay que guardar el **ticket** y consultar **GET /summary/status** hasta que **success** sea `true` y venga **cdrZip** para descargar el CDR.

## Payload: Resumen diario (Summary)

| Campo | Tipo | Descripción |
|-------|------|-------------|
| **company** | object | Emisor (igual que en otros comprobantes). |
| **correlativo** | string | Correlativo del resumen (ej. `"1"`). |
| **fecGeneracion** | string | Fecha de generación (ISO 8601). |
| **fecResumen** | string | Fecha del resumen (día que se reporta) (ISO 8601). |
| **moneda** | string | Moneda (ej. `"PEN"`). |
| **details** | array | Lista de resúmenes por comprobante. Cada elemento: tipoDoc, serieNro, cliente, totales, etc. (SummaryDetail). |

Cada **SummaryDetail** incluye, entre otros: **tipoDoc**, **serieNro** (serie-número), **clienteTipo**, **clienteNro**, **total**, **mtoOperGravadas**, **mtoIGV**, y demás totales según el tipo de comprobante. La estructura completa está en el esquema **Summary** del `swagger.yaml` (properties de Summary y SummaryDetail).

### Ejemplo mínimo de payload (resumen)

```json
{
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
  "correlativo": "1",
  "fecGeneracion": "2026-03-08T12:00:00-05:00",
  "fecResumen": "2026-03-08T00:00:00-05:00",
  "moneda": "PEN",
  "details": [
    {
      "tipoDoc": "03",
      "serieNro": "B001-1",
      "clienteTipo": "1",
      "clienteNro": "12345678",
      "total": 118.00,
      "mtoOperGravadas": 100.00,
      "mtoIGV": 18.00
    }
  ]
}
```

(En producción hay que completar todos los campos requeridos por SUNAT para el resumen; el swagger y la validación del backend definen el detalle.)

## Respuestas exactas — Resumen de boletas

### POST /api/v1/summary/send

- **Body:** JSON del resumen (Summary).
- **Respuesta HTTP 200:** JSON con **ticket** en sunatResponse (no trae CDR en la respuesta directa):

```json
{
  "xml": "<XML firmado enviado>",
  "hash": "<hash>",
  "sunatResponse": {
    "success": true,
    "error": null,
    "ticket": "1234567890123456"
  }
}
```

- Se debe guardar **sunatResponse.ticket** y usarlo en **GET /api/v1/summary/status**.

### POST /api/v1/summary/xml

- **Body:** mismo JSON del resumen.
- **Respuesta HTTP 200:** cuerpo = archivo **XML**; `Content-Type: text/xml`, `Content-Disposition` con nombre de archivo.

### POST /api/v1/summary/pdf

- **Body:** mismo JSON del resumen.
- **Respuesta HTTP 200:** cuerpo = archivo **PDF**; `Content-Type: application/pdf`, `Content-Disposition` con nombre de archivo.

### GET /api/v1/summary/status

- **Parámetros de query:** **ticket** (obligatorio), **ruc** (opcional).
- **Respuesta HTTP 200:** mismo formato que voided/status:

```json
{
  "success": true,
  "error": null,
  "code": "0",
  "cdrZip": "<base64 del ZIP del CDR>",
  "cdrResponse": {
    "accepted": true,
    "id": "...",
    "code": "0",
    "description": "Aceptado",
    "notes": []
  }
}
```

- Cuando **success** es `true` y existe **cdrZip**, el frontend puede decodificar el base64, descomprimir el ZIP y obtener el XML del CDR para guardarlo o mostrarlo.

- **HTTP 400** si falta el ticket: `{ "message": "Ticket Requerido" }`.

---

## Resumen rápido (frontend)

| Recurso | Enviar | XML | PDF | Consultar estado / CDR |
|---------|--------|-----|-----|------------------------|
| **Comunicación de baja** | `POST /api/v1/voided/send` | `POST /api/v1/voided/xml` | `POST /api/v1/voided/pdf` | `GET /api/v1/voided/status?ticket=...` |
| **Resumen diario** | `POST /api/v1/summary/send` | `POST /api/v1/summary/xml` | `POST /api/v1/summary/pdf` | `GET /api/v1/summary/status?ticket=...` |

- En **voided** y **summary**, el **CDR** se obtiene por el endpoint **status** cuando SUNAT devuelve ticket al enviar; en la respuesta de **status** viene **cdrZip** (base64) para descargar el CDR.
