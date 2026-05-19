# Guía de remisión, Retención, Percepción, Reversión y Detracción

Documentación de los **documentos electrónicos** que faltaban respecto a factura, boleta, nota de crédito/débito, comunicación de baja y resumen de boletas. Incluye solo lo **implementado** en el backend.

---

# 1. Guía de remisión (Despatch)

Incluye **guía de remisión remitente** (emisor de la mercadería) y **guía de remisión transportista** (cuando el traslado lo hace un transportista): el mismo endpoint y modelo **Despatch** sirve para ambos; la diferencia está en los datos que se envían (origen, destino, **transportista**, vehículo, etc.).

## Endpoints implementados

| Acción | Método y ruta | Respuesta |
|--------|----------------|-----------|
| **Enviar a SUNAT** | `POST /api/v1/despatch/send?token=TU_TOKEN` | JSON: `xml`, `sunatResponse` (ticket o CDR; **no** incluye `hash`). |
| **Solo XML** | `POST /api/v1/despatch/xml?token=TU_TOKEN` | Archivo XML. `Content-Type: text/xml`. |
| **Solo PDF** | `POST /api/v1/despatch/pdf?token=TU_TOKEN` | Archivo PDF. `Content-Type: application/pdf`. |
| **Estado del ticket** | `GET /api/v1/despatch/status?ticket=TICKET&ruc=RUC` | JSON: estado y, si SUNAT respondió, `cdrZip` (base64) y `cdrResponse`. |

- **ruc** en `status` es opcional (multi-empresa).
- Para **PDF** y **XML** se usa el **mismo body** que para `/send`.

## Payload: Guía de remisión (Despatch)

| Campo | Tipo | Descripción |
|-------|------|-------------|
| **version** | string | Versión UBL (ej. `"2020"`). |
| **tipoDoc** | string | Tipo de guía (según catálogo SUNAT; ej. `"09"` para guía remisión remitente). |
| **serie** | string | Serie (ej. `"T001"`). |
| **correlativo** | string | Número correlativo. |
| **fechaEmision** | string | Fecha y hora ISO 8601. |
| **observacion** | string | Opcional. Observaciones. |
| **company** | object | Emisor (ruc, razonSocial, nombreComercial, address). |
| **destinatario** | object | Cliente/destinatario (tipoDoc, numDoc, rznSocial, address). |
| **tercero** | object | Opcional. Tercero cuando aplica. |
| **comprador** | object | Opcional. Comprador cuando aplica. |
| **envio** | object | **Shipment**: datos del traslado. Ver abajo. |
| **details** | array | Ítems que se trasladan (DespatchDetail). |
| **addDocs** | array | Opcional. Documentos adicionales (AdditionalDoc). |

### Objeto envio (Shipment) — traslado y transportista

Para **guía remisión transportista** se completan **transportista**, **vehiculo**, **choferes**; para **guía remitente** pueden ir solo origen/destino y modo de traslado.

| Campo | Tipo | Descripción |
|-------|------|-------------|
| **codTraslado** | string | Código motivo de traslado (catálogo SUNAT). |
| **desTraslado** | string | Descripción del motivo de traslado. |
| **modTraslado** | string | Modalidad de traslado (catálogo). |
| **fecTraslado** | string | Fecha/hora de traslado (ISO 8601). |
| **partida** | object | Lugar de partida (Direction: dirección, ubigueo, etc.). |
| **llegada** | object | Lugar de llegada (Direction). |
| **pesoTotal** | number | Peso total. |
| **undPesoTotal** | string | Unidad de peso (ej. `"KGM"`). |
| **numBultos** | integer | Número de bultos. |
| **transportista** | object | **Transportist**: tipoDoc, numDoc, rznSocial, nroMtc, placa, choferTipoDoc, choferDoc. |
| **vehiculo** | object | **Vehicle**: placa, nroCirculacion, etc. |
| **choferes** | array | **Driver**: tipo, tipoDoc, nroDoc, nombres, apellidos, licencia. |

### DespatchDetail (cada ítem)

| Campo | Tipo | Descripción |
|-------|------|-------------|
| **codigo** | string | Código del ítem. |
| **descripcion** | string | Descripción. |
| **unidad** | string | Unidad de medida (ej. `"NIU"`). |
| **cantidad** | number | Cantidad. |
| **codProdSunat** | string | Opcional. Código producto SUNAT. |

### Ejemplo mínimo (guía con transportista)

```json
{
  "version": "2020",
  "tipoDoc": "09",
  "serie": "T001",
  "correlativo": "1",
  "fechaEmision": "2026-03-08T10:00:00-05:00",
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
  "destinatario": {
    "tipoDoc": "6",
    "numDoc": "20100000001",
    "rznSocial": "CLIENTE S.A.C.",
    "address": {
      "ubigueo": "150101",
      "codigoPais": "PE",
      "departamento": "LIMA",
      "provincia": "LIMA",
      "distrito": "LIMA",
      "direccion": "AV. DESTINO 456"
    }
  },
  "envio": {
    "codTraslado": "01",
    "desTraslado": "Venta",
    "modTraslado": "01",
    "fecTraslado": "2026-03-08T10:00:00-05:00",
    "partida": {
      "ubigueo": "150131",
      "codigoPais": "PE",
      "departamento": "LIMA",
      "provincia": "LIMA",
      "distrito": "SAN ISIDRO",
      "direccion": "AV. EJEMPLO 123"
    },
    "llegada": {
      "ubigueo": "150101",
      "codigoPais": "PE",
      "departamento": "LIMA",
      "provincia": "LIMA",
      "distrito": "LIMA",
      "direccion": "AV. DESTINO 456"
    },
    "pesoTotal": 100,
    "undPesoTotal": "KGM",
    "numBultos": 2,
    "transportista": {
      "tipoDoc": "6",
      "numDoc": "20100000002",
      "rznSocial": "TRANSPORTES XYZ S.A.C.",
      "placa": "ABC-123",
      "choferTipoDoc": "1",
      "choferDoc": "87654321"
    }
  },
  "details": [
    {
      "codigo": "P001",
      "descripcion": "Producto ejemplo",
      "unidad": "NIU",
      "cantidad": 10
    }
  ]
}
```

## Respuesta POST /api/v1/despatch/send

- **HTTP 200.** Cuerpo JSON (sin `hash`):

```json
{
  "xml": "<XML firmado enviado a SUNAT>",
  "sunatResponse": {
    "success": true,
    "error": null,
    "ticket": "1234567890123456"
  }
}
```

o, si SUNAT devuelve CDR directo:

```json
{
  "xml": "...",
  "sunatResponse": {
    "success": true,
    "error": null,
    "cdrZip": "<base64>",
    "cdrResponse": { "accepted": true, "code": "0", "description": "...", "notes": [] }
  }
}
```

- Si hay **ticket**, usar **GET /api/v1/despatch/status?ticket=...** para obtener el CDR cuando esté listo (misma forma que en [PAYLOAD-VOIDED-RESUMEN.md](PAYLOAD-VOIDED-RESUMEN.md) para voided/summary status).

## Respuesta GET /api/v1/despatch/status

- **Parámetros:** `ticket` (obligatorio), `ruc` (opcional).
- **HTTP 200:** JSON tipo StatusResult: `success`, `error`, `code`, `cdrZip`, `cdrResponse`.
- **HTTP 400** si falta ticket: `{ "message": "Ticket Requerido" }`.

---

# 2. Comprobante de retención (Retention)

Para documentar retenciones del IR (cuando el comprador retiene un porcentaje al proveedor).

## Endpoints implementados

| Acción | Método y ruta | Respuesta |
|--------|----------------|-----------|
| **Enviar a SUNAT** | `POST /api/v1/retention/send?token=TU_TOKEN` | JSON: `xml`, `hash`, `sunatResponse` (BillResult). |
| **Solo XML** | `POST /api/v1/retention/xml?token=TU_TOKEN` | Archivo XML. |
| **Solo PDF** | `POST /api/v1/retention/pdf?token=TU_TOKEN` | Archivo PDF. |

No hay endpoint **status** para retención en este backend.

## Payload: Retención (Retention)

| Campo | Tipo | Descripción |
|-------|------|-------------|
| **serie** | string | Serie (ej. `"R001"`). |
| **correlativo** | string | Correlativo. |
| **fechaEmision** | string | Fecha ISO 8601. |
| **company** | object | Emisor (quien retiene). |
| **proveedor** | object | Proveedor (client): tipoDoc, numDoc, rznSocial, address. |
| **regimen** | string | Régimen de retención. |
| **tasa** | number | Tasa de retención. |
| **impRetenido** | number | Importe retenido. |
| **impPagado** | number | Importe pagado. |
| **observacion** | string | Opcional. |
| **details** | array | RetentionDetail: tipoDoc, numDoc, fechaEmision, impTotal, moneda, pagos, fechaRetencion, impRetenido, impPagar, tipoCambio. |

### Ejemplo mínimo

```json
{
  "serie": "R001",
  "correlativo": "1",
  "fechaEmision": "2026-03-08T12:00:00-05:00",
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
  "proveedor": {
    "tipoDoc": "6",
    "numDoc": "20100000001",
    "rznSocial": "PROVEEDOR S.A.C.",
    "address": {
      "ubigueo": "150101",
      "codigoPais": "PE",
      "departamento": "LIMA",
      "provincia": "LIMA",
      "distrito": "LIMA",
      "direccion": "AV. PROVEEDOR 100"
    }
  },
  "regimen": "01",
  "tasa": 6,
  "impRetenido": 60.00,
  "impPagado": 940.00,
  "details": [
    {
      "tipoDoc": "01",
      "numDoc": "F001-1",
      "fechaEmision": "2026-03-08T10:00:00-05:00",
      "impTotal": 1000.00,
      "moneda": "PEN",
      "fechaRetencion": "2026-03-08T12:00:00-05:00",
      "impRetenido": 60.00,
      "impPagar": 940.00
    }
  ]
}
```

## Respuesta POST /api/v1/retention/send

- **HTTP 200.** Misma estructura que factura/boleta: `xml`, `hash`, `sunatResponse` (success, error, cdrZip, cdrResponse). Ver [RESPUESTA-SUNAT-BACKEND.md](RESPUESTA-SUNAT-BACKEND.md).

---

# 3. Comprobante de percepción (Perception)

Para documentar percepciones (cuando el comprador percibe un porcentaje al proveedor).

## Endpoints implementados

| Acción | Método y ruta | Respuesta |
|--------|----------------|-----------|
| **Enviar a SUNAT** | `POST /api/v1/perception/send?token=TU_TOKEN` | JSON: `xml`, `hash`, `sunatResponse` (BillResult). |
| **Solo XML** | `POST /api/v1/perception/xml?token=TU_TOKEN` | Archivo XML. |
| **Solo PDF** | `POST /api/v1/perception/pdf?token=TU_TOKEN` | Archivo PDF. |

No hay endpoint **status** para percepción.

## Payload: Percepción (Perception)

| Campo | Tipo | Descripción |
|-------|------|-------------|
| **serie** | string | Serie (ej. `"P001"`). |
| **correlativo** | string | Correlativo. |
| **fechaEmision** | string | Fecha ISO 8601. |
| **company** | object | Emisor (quien percibe). |
| **proveedor** | object | Proveedor (client). |
| **regimen** | string | Régimen de percepción. |
| **tasa** | number | Tasa. |
| **impPercibido** | number | Importe percibido. |
| **impCobrado** | number | Importe cobrado. |
| **observacion** | string | Opcional. |
| **details** | array | PerceptionDetail: tipoDoc, numDoc, fechaEmision, impTotal, moneda, cobros (Payment), fechaPercepcion, impPercibido, impCobrar, etc. |

### Ejemplo mínimo

```json
{
  "serie": "P001",
  "correlativo": "1",
  "fechaEmision": "2026-03-08T12:00:00-05:00",
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
  "proveedor": {
    "tipoDoc": "6",
    "numDoc": "20100000001",
    "rznSocial": "PROVEEDOR S.A.C.",
    "address": {
      "ubigueo": "150101",
      "codigoPais": "PE",
      "departamento": "LIMA",
      "provincia": "LIMA",
      "distrito": "LIMA",
      "direccion": "AV. PROVEEDOR 100"
    }
  },
  "regimen": "01",
  "tasa": 2,
  "impPercibido": 20.00,
  "impCobrado": 980.00,
  "details": [
    {
      "tipoDoc": "01",
      "numDoc": "F001-1",
      "fechaEmision": "2026-03-08T10:00:00-05:00",
      "impTotal": 1000.00,
      "moneda": "PEN",
      "fechaPercepcion": "2026-03-08T12:00:00-05:00",
      "impPercibido": 20.00,
      "impCobrar": 980.00
    }
  ]
}
```

## Respuesta POST /api/v1/perception/send

- **HTTP 200.** Igual que factura/boleta: `xml`, `hash`, `sunatResponse`. Ver [RESPUESTA-SUNAT-BACKEND.md](RESPUESTA-SUNAT-BACKEND.md).

---

# 4. Comunicación de reversión (Reversion)

Para revertir percepciones u otros efectos. Misma idea que comunicación de baja pero para reversiones.

## Endpoints implementados

| Acción | Método y ruta | Respuesta |
|--------|----------------|-----------|
| **Enviar a SUNAT** | `POST /api/v1/reversion/send?token=TU_TOKEN` | JSON: `xml`, `hash`, `sunatResponse` (BillResult o ticket). |
| **Solo XML** | `POST /api/v1/reversion/xml?token=TU_TOKEN` | Archivo XML. |
| **Solo PDF** | `POST /api/v1/reversion/pdf?token=TU_TOKEN` | Archivo PDF. |
| **Estado del ticket** | `GET /api/v1/reversion/status?ticket=TICKET&ruc=RUC` | JSON: success, cdrZip, cdrResponse. |

## Payload: Reversión (Reversion)

Estructura **igual** que Comunicación de baja (Voided): company, correlativo, fecGeneracion, fecComunicacion, details (cada uno: tipoDoc, serie, correlativo, desMotivoBaja). Ver [PAYLOAD-VOIDED-RESUMEN.md](PAYLOAD-VOIDED-RESUMEN.md) para el detalle de **Voided**; **Reversion** usa el mismo esquema.

### Ejemplo mínimo

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
      "tipoDoc": "40",
      "serie": "P001",
      "correlativo": "1",
      "desMotivoBaja": "Reversión de percepción"
    }
  ]
}
```

## Respuestas

- **POST /reversion/send:** igual que otros comprobantes con BillResult (o ticket si SUNAT lo devuelve).
- **GET /reversion/status:** igual que voided/status y summary/status: `success`, `cdrZip`, `cdrResponse`, `error`. **HTTP 400** si falta ticket: `{ "message": "Ticket Requerido" }`.

---

# 5. Detracción (en factura y boleta)

La **detracción** no tiene endpoint propio. Se envía como **campo opcional** dentro del payload de **factura** o **boleta** (Invoice) en los endpoints que ya tienes documentados:

- `POST /api/v1/invoice/send`
- `POST /api/v1/invoice/xml`
- `POST /api/v1/invoice/pdf`

Ver [PAYLOAD-FACTURA-BOLETA.md](PAYLOAD-FACTURA-BOLETA.md) para el payload base. Añade en el mismo JSON de la factura/boleta un objeto **detraccion** con la siguiente estructura.

## Campo opcional: detraccion (dentro del JSON de Invoice)

| Campo | Tipo | Descripción |
|-------|------|-------------|
| **percent** | number | Porcentaje de detracción. |
| **mount** | number | Monto detraído. |
| **ctaBanco** | string | Código cuenta bancaria (catálogo SUNAT). |
| **codMedioPago** | string | Código medio de pago (catálogo 59). |
| **codBienDetraccion** | string | Código bien o servicio (catálogo 54). |
| **valueRef** | number | Opcional. Valor de referencia. |

### Ejemplo (fragmento dentro de una factura)

```json
{
  "ublVersion": "2.1",
  "tipoOperacion": "0101",
  "tipoDoc": "01",
  "serie": "F001",
  "correlativo": "2",
  "fechaEmision": "2026-03-08T12:00:00-05:00",
  "company": { ... },
  "client": { ... },
  "tipoMoneda": "PEN",
  "detraccion": {
    "percent": 4,
    "mount": 40.00,
    "ctaBanco": "01-0123-123456789",
    "codMedioPago": "001",
    "codBienDetraccion": "034"
  },
  "mtoOperGravadas": 1000.00,
  "mtoIGV": 180.00,
  "totalImpuestos": 180.00,
  "valorVenta": 1000.00,
  "subTotal": 1180.00,
  "mtoImpVenta": 1180.00,
  "details": [ ... ],
  "legends": [ ... ]
}
```

Los catálogos (cuenta bancaria, medio de pago, bien/servicio) son los publicados por SUNAT para detracción.

---

# Resumen: documentos y endpoints

| Documento | Send | XML | PDF | Status |
|-----------|------|-----|-----|--------|
| **Guía de remisión** (remitente o transportista) | `POST /api/v1/despatch/send` | `POST /api/v1/despatch/xml` | `POST /api/v1/despatch/pdf` | `GET /api/v1/despatch/status?ticket=...` |
| **Retención** | `POST /api/v1/retention/send` | `POST /api/v1/retention/xml` | `POST /api/v1/retention/pdf` | — |
| **Percepción** | `POST /api/v1/perception/send` | `POST /api/v1/perception/xml` | `POST /api/v1/perception/pdf` | — |
| **Reversión** | `POST /api/v1/reversion/send` | `POST /api/v1/reversion/xml` | `POST /api/v1/reversion/pdf` | `GET /api/v1/reversion/status?ticket=...` |
| **Detracción** | No hay endpoint propio; se envía dentro del body de **invoice** (factura/boleta) en `/invoice/send`, `/invoice/xml`, `/invoice/pdf`. | | | |

Todos los `POST` de envío usan **token** en query: `?token=TU_TOKEN`. El **ruc** en `status` es opcional cuando hay multi-empresa.
