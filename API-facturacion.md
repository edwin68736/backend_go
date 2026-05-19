# API REST — Lycet Facturador SUNAT

Documentación completa del API para integrar el facturador desde un backend externo (por ejemplo Go). El facturador recibe la información del comprobante, genera el XML UBL 2.1, lo firma con el certificado de la empresa y lo envía a SUNAT. **Multiempresa** se maneja mediante el RUC en cada comprobante y la configuración almacenada en el servidor.

---

## Índice

1. [Base URL y autenticación](#1-base-url-y-autenticación)
2. [Multiempresa (SaaS)](#2-multiempresa-saas)
3. [Configuración (certificados y empresas)](#3-configuración-certificados-y-empresas)
4. [Comprobantes con respuesta inmediata (Factura, Boleta, Notas, Retención, Percepción)](#4-comprobantes-con-respuesta-inmediata)
5. [Comprobantes con ticket (Resumen, Comunicación de baja, Reversión)](#5-comprobantes-con-ticket)
6. [Guía de remisión (Despacho)](#6-guía-de-remisión-despacho)
7. [Consulta de estado y CDR](#7-consulta-de-estado-y-cdr)
8. [Utilidades (QR, PDF)](#8-utilidades-qr-pdf)
9. [Códigos y formatos comunes](#9-códigos-y-formatos-comunes)
10. [Errores y códigos HTTP](#10-errores-y-códigos-http)
11. [Resumen de endpoints](#11-resumen-de-endpoints)
12. [Integración desde Go](#12-integración-desde-go)

---

## 1. Base URL y autenticación

- **Base URL:** `https://tu-dominio.com/api/v1` (o `http://localhost:8000/api/v1` en desarrollo).
- **Autenticación:** Todas las rutas bajo `/api` requieren el token en **query**:
  ```
  ?token=TU_CLIENT_TOKEN
  ```
  El valor se configura en el servidor Lycet con la variable de entorno `CLIENT_TOKEN` (por ejemplo en `.env`).

- **Cabeceras recomendadas:**
  ```
  Content-Type: application/json
  Accept: application/json
  ```

**Ejemplo desde Go:**
```go
baseURL := "https://lycet.ejemplo.com/api/v1"
token := "tu_token_secreto"
// Añadir a todas las peticiones: baseURL + "/invoice/send?token=" + token
```

---

## 2. Multiempresa (SaaS)

El facturador identifica la empresa por el **RUC** que viene dentro de cada comprobante en `company.ruc`. No envías el RUC en la URL ni en un header especial: va en el JSON del body.

- Si existe el archivo **`empresas.json`** en el servidor (en la carpeta `data/`) y hay una entrada para ese RUC, se usan **certificado, usuario SOL y contraseña (y URLs si se definen)** de esa empresa.
- Si no existe entrada para ese RUC, se usan las **variables de entorno** del servidor (una sola empresa por defecto).

Por tanto, en un esquema SaaS:

1. **Desde tu backend Go:** guardas tú los datos de cada empresa (RUC, certificado, usuario SOL, etc.).
2. **En Lycet:** registras cada empresa vía [Configuración](#3-configuración-certificados-y-empresas) (certificado, logo y el contenido de `empresas.json`).
3. **Al emitir:** en cada request envías el comprobante con `company.ruc` correspondiente. Lycet elige automáticamente la configuración de ese RUC.

Estructura de **`empresas.json`** (la que debe estar guardada en el servidor o enviarse en base64 en configuración):

```json
{
  "20123456789": {
    "SOL_USER": "20123456789MODDATOS",
    "SOL_PASS": "moddatos",
    "certificate": "20123456789-cert.pem",
    "logo": "20123456789-logo.png"
  },
  "20987654321": {
    "SOL_USER": "20987654321MODDATOS",
    "SOL_PASS": "moddatos",
    "certificate": "20987654321-cert.pem",
    "logo": "20987654321-logo.png",
    "FE_URL": "https://e-beta.sunat.gob.pe/ol-ti-itcpfegem-beta/billService",
    "RE_URL": "https://e-beta.sunat.gob.pe/ol-ti-itemision-otroscpe-gem-beta/billService",
    "GUIA_URL": "https://e-beta.sunat.gob.pe/ol-ti-itemision-guia-gem-beta/billService"
  }
}
```

- **SOL_USER:** RUC (11 dígitos) + usuario SOL (ej. `MODDATOS`), sin espacio.
- **certificate** / **logo:** nombres de archivos dentro de la carpeta `data/` del servidor. Esos archivos deben haberse subido antes (certificado en PEM, logo en PNG).

Para **guías de remisión** el facturador usa el cliente **API** (Nubefact u otro OSE). En ese caso en `empresas.json` puedes incluir también:

```json
"AUTH_URL": "https://api-test-seguridad.sunat.gob.pe/v1",
"API_URL": "https://api-test.sunat.gob.pe/v1",
"CLIENT_ID": "tu-client-id",
"CLIENT_SECRET": "tu-client-secret"
```

---

## 3. Configuración (certificados y empresas)

### POST `/api/v1/configuration/`

Guarda en el servidor el certificado, el logo y/o el contenido del archivo de empresas. Cada valor se envía en **Base64**. Las claves permitidas son: `certificate`, `logo`, `companies`.

- **Autenticación:** `?token=...`
- **Content-Type:** `application/json`
- **Body:**

```json
{
  "certificate": "LS0tLS1CRUdJTi...",
  "logo": "iVBORw0KGgo...",
  "companies": "eyIyMDAwMDAwMDAwMSI6..."
}
```

- **certificate:** Contenido del archivo `.pem` en Base64 (certificado digital para firmar).
- **logo:** Contenido del archivo `logo.png` en Base64 (opcional; se usa en los PDF).
- **companies:** Contenido del archivo **empresas.json** (JSON completo como string) codificado en Base64. Es decir: `base64(json_string)` donde `json_string` es el texto del objeto `{"RUC": {...}, ...}`.

Si envías solo una clave (por ejemplo solo `companies`), las demás no se modifican. Las claves con valor vacío se ignoran.

- **Respuesta:** `204 No Content` en éxito (body vacío).
- **Errores:** 403 si el token es inválido.

**Ejemplo Go (enviar empresas.json):**
```go
empresas := map[string]interface{}{
    "20123456789": map[string]string{
        "SOL_USER":     "20123456789MODDATOS",
        "SOL_PASS":     "moddatos",
        "certificate":  "20123456789-cert.pem",
        "logo":         "20123456789-logo.png",
    },
}
jsonBytes, _ := json.Marshal(empresas)
companiesB64 := base64.StdEncoding.EncodeToString(jsonBytes)
body := map[string]string{"companies": companiesB64}
// POST /api/v1/configuration/?token=...
```

**Nota sobre certificados y logos por empresa:** El endpoint solo acepta las claves `certificate`, `logo` y `companies`. Para multiempresa, en `empresas.json` cada RUC referencia archivos por nombre (ej. `20123456789-cert.pem`). Esos archivos deben existir en la carpeta `data/` del servidor. Para crearlos puedes: (1) montar un volumen con los archivos ya generados, (2) usar una herramienta como [lycet-ui-config](https://giansalex.github.io/lycet-ui-config/) o (3) extender el backend con un endpoint que acepte `ruc` + certificado/logo en base64 y escriba en `data/{ruc}-cert.pem` y `data/{ruc}-logo.png`.

---

## 12. Integración desde Go

- **Base:** Todas las peticiones a `/api/v1/...` deben llevar `?token=TU_TOKEN`.
- **Empresa:** En cada comprobante incluye `company.ruc`; el facturador usará la configuración de ese RUC (empresas.json) o la de entorno.
- **Flujo típico:**
  1. Configurar una vez (o por empresa): `POST /api/v1/configuration/` con `companies` en base64 (y opcionalmente `certificate`/`logo` global).
  2. Emitir: `POST /api/v1/invoice/send` (o note, summary, voided, etc.) con el JSON del comprobante.
  3. Para resumen/baja/reversión/guía: guardar el `ticket` de la respuesta y luego consultar `GET .../status?ticket=...&ruc=...` hasta obtener el CDR.
- **Generación de cliente Go:** Puedes usar el OpenAPI del proyecto para generar un cliente:
  - Archivo: `public/swagger.yaml` (ruta relativa al repo: `public/swagger.yaml`).
  - Herramientas: [oapi-codegen](https://github.com/deepmap/oapi-codegen), [go-swagger](https://github.com/go-swagger/go-swagger) o importar en Postman y exportar código Go.
  - **Importante:** En el Swagger actual las rutas pueden aparecer sin el prefijo `/api/v1`; en producción la base URL debe ser `https://tu-host/api/v1` y el token añadido en todas las peticiones (query `token`).

Ejemplo mínimo de petición desde Go:
```go
url := baseURL + "/invoice/send?token=" + token
buf, _ := json.Marshal(invoicePayload)
req, _ := http.NewRequest("POST", url, bytes.NewReader(buf))
req.Header.Set("Content-Type", "application/json")
resp, err := http.DefaultClient.Do(req)
// Leer resp.Body para obtener xml, hash, sunatResponse
```

---

---

## 4. Comprobantes con respuesta inmediata

Estos comprobantes reciben de SUNAT una respuesta directa (aceptado/rechazado) y, en caso de éxito, el CDR viene en la misma respuesta.

Comportamiento común:

- **POST .../send:** Genera XML, firma, envía a SUNAT y devuelve XML firmado, hash y respuesta SUNAT (incluyendo CDR en base64 si aplica).
- **POST .../xml:** Solo genera y firma el XML; no envía a SUNAT. Útil para pruebas o flujos donde tú envías el XML después.
- **POST .../pdf:** Genera el PDF del comprobante (requiere logo y datos en el documento). No envía a SUNAT.

En todos los casos el **RUC de la empresa** se toma de `company.ruc` del body. El body puede ser el comprobante en la raíz del JSON o dentro de la clave **`document`**.

### 4.1 Factura / Boleta

| Método | Ruta | Descripción |
|--------|------|-------------|
| POST | `/api/v1/invoice/send` | Enviar a SUNAT |
| POST | `/api/v1/invoice/xml` | Obtener solo XML firmado |
| POST | `/api/v1/invoice/pdf` | Obtener PDF |
| GET | `/api/v1/invoice/status` | Consultar estado/CDR por tipo, serie y número |

**Tipos de documento (tipoDoc):** `01` = Factura, `03` = Boleta.

**Ejemplo mínimo de body (send/xml/pdf):**
```json
{
  "ublVersion": "2.1",
  "tipoDoc": "01",
  "serie": "F001",
  "correlativo": "1",
  "fechaEmision": "2024-01-15T10:00:00-05:00",
  "company": {
    "ruc": "20123456789",
    "razonSocial": "MI EMPRESA S.A.C.",
    "nombreComercial": "MI EMPRESA",
    "address": {
      "ubigueo": "150101",
      "codigoPais": "PE",
      "departamento": "LIMA",
      "provincia": "LIMA",
      "distrito": "LIMA",
      "direccion": "AV. EJEMPLO 123"
    }
  },
  "client": {
    "tipoDoc": "6",
    "numDoc": "20100000001",
    "rznSocial": "CLIENTE SAC",
    "address": {
      "ubigueo": "150101",
      "codigoPais": "PE",
      "direccion": "CALLE 456"
    }
  },
  "tipoMoneda": "PEN",
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
      "codProducto": "PROD01",
      "descripcion": "Producto ejemplo",
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
    { "code": "1000", "value": "SON CIEN CON 00/100 SOLES" }
  ]
}
```

**Respuesta típica de `/invoice/send` (200):**
```json
{
  "xml": "<?xml version=\"1.0\" ...",
  "hash": "ABC123...",
  "sunatResponse": {
    "success": true,
    "cdrZip": "UEsDBBQACAgI...",
    "cdrResponse": {
      "id": "R-2024-00123456",
      "code": "0",
      "description": "La factura fue aceptada",
      "notes": []
    }
  }
}
```

**Consulta de estado (GET `/api/v1/invoice/status`):**
- Query: `tipo`, `serie`, `numero` (obligatorios); `ruc` (opcional, para multiempresa).
- Ejemplo: `GET /api/v1/invoice/status?token=xxx&tipo=01&serie=F001&numero=1&ruc=20123456789`
- Respuesta: mismo esquema que `sunatResponse` (incluye `cdrZip` en base64 si hay éxito).

---

### 4.2 Nota de crédito / débito

| Método | Ruta | Descripción |
|--------|------|-------------|
| POST | `/api/v1/note/send` | Enviar a SUNAT |
| POST | `/api/v1/note/xml` | XML firmado |
| POST | `/api/v1/note/pdf` | PDF |

**tipoDoc:** `07` = Nota de crédito, `08` = Nota de débito.  
Además del cuerpo similar a factura/boleta, se incluyen:
- `tipDocAfectado`: tipo del comprobante que se anula/modifica (ej. `01`, `03`).
- `numDocfectado`: serie-número del comprobante afectado (ej. `F001-1`).
- `codMotivo` / `desMotivo`: código y descripción del motivo.

---

### 4.3 Retención

| Método | Ruta | Descripción |
|--------|------|-------------|
| POST | `/api/v1/retention/send` | Enviar a SUNAT |
| POST | `/api/v1/retention/xml` | XML firmado |
| POST | `/api/v1/retention/pdf` | PDF |

El body sigue el esquema de comprobante de retención (serie, correlativo, company, proveedor, regimen, tasa, details con pagos, etc.). Ver `public/swagger.yaml` para el schema completo.

---

### 4.4 Percepción

| Método | Ruta | Descripción |
|--------|------|-------------|
| POST | `/api/v1/perception/send` | Enviar a SUNAT |
| POST | `/api/v1/perception/xml` | XML firmado |
| POST | `/api/v1/perception/pdf` | PDF |

El body sigue el esquema de comprobante de percepción (serie, correlativo, company, proveedor, regimen, tasa, details, etc.).

---

## 5. Comprobantes con ticket

Resumen diario, comunicación de baja y resumen de reversiones se envían a SUNAT y esta devuelve un **ticket**. Luego se consulta el estado con ese ticket hasta obtener el CDR.

### 5.1 Resumen diario (Summary)

| Método | Ruta | Descripción |
|--------|------|-------------|
| POST | `/api/v1/summary/send` | Enviar; respuesta incluye `ticket` |
| POST | `/api/v1/summary/xml` | Solo XML |
| POST | `/api/v1/summary/pdf` | PDF del resumen |
| GET | `/api/v1/summary/status` | Estado del ticket |

**GET `/api/v1/summary/status`:**
- Query: `ticket` (obligatorio), `ruc` (opcional).
- Ejemplo: `GET /api/v1/summary/status?token=xxx&ticket=123456789&ruc=20123456789`

**Respuesta de send (200):**
```json
{
  "xml": "...",
  "hash": "...",
  "sunatResponse": {
    "success": true,
    "ticket": "123456789"
  }
}
```

---

### 5.2 Comunicación de baja (Voided)

| Método | Ruta | Descripción |
|--------|------|-------------|
| POST | `/api/v1/voided/send` | Enviar; respuesta con `ticket` |
| POST | `/api/v1/voided/xml` | Solo XML |
| POST | `/api/v1/voided/pdf` | PDF |
| GET | `/api/v1/voided/status` | Estado del ticket |

**GET `/api/v1/voided/status`:** `ticket` (obligatorio), `ruc` (opcional).

---

### 5.3 Resumen de reversiones (Reversion)

| Método | Ruta | Descripción |
|--------|------|-------------|
| POST | `/api/v1/reversion/send` | Enviar; respuesta con `ticket` |
| POST | `/api/v1/reversion/xml` | Solo XML |
| POST | `/api/v1/reversion/pdf` | PDF |
| GET | `/api/v1/reversion/status` | Estado del ticket |

**GET `/api/v1/reversion/status`:** `ticket` (obligatorio), `ruc` (opcional).

---

## 6. Guía de remisión (Despacho)

Las guías usan el cliente **API** (no SOAP). La respuesta de **send** no incluye `hash`; solo `xml` y `sunatResponse`.

| Método | Ruta | Descripción |
|--------|------|-------------|
| POST | `/api/v1/despatch/send` | Enviar a SUNAT (API) |
| POST | `/api/v1/despatch/xml` | XML firmado |
| POST | `/api/v1/despatch/pdf` | PDF |
| GET | `/api/v1/despatch/status` | Estado del ticket |

**GET `/api/v1/despatch/status`:** `ticket` (obligatorio), `ruc` (opcional).

**Respuesta típica de `/despatch/send`:**
```json
{
  "xml": "<?xml ...",
  "sunatResponse": {
    "success": true,
    "ticket": "123456789"
  }
}
```

Para multiempresa con guías, en `empresas.json` debe estar configurado el RUC con credenciales API (CLIENT_ID, CLIENT_SECRET y URLs AUTH/API si aplica).

---

## 7. Consulta de estado y CDR

- **Factura/Boleta/Nota:** uso de **GET `/api/v1/invoice/status`** con `tipo`, `serie`, `numero` y opcionalmente `ruc`.
- **Resumen, Comunicación de baja, Reversión, Guía:** uso de **GET `.../status`** con `ticket` y opcionalmente `ruc`.

En todos los casos, si la respuesta es exitosa y hay CDR, viene `cdrZip` en base64. Puedes decodificarlo y descomprimir el ZIP para obtener el XML del CDR.

---

## 8. Utilidades (QR, PDF)

### GET/POST `/api/v1/sale/qr`

Genera la imagen **QR** (en formato SVG) para un comprobante de venta. No envía nada a SUNAT.

- **Método:** POST (body JSON).
- **Body ejemplo:**
```json
{
  "ruc": "20123456789",
  "tipo": "01",
  "serie": "F001",
  "numero": "1",
  "emision": "2024-01-15",
  "igv": 15.25,
  "total": 100.00,
  "clienteTipo": "6",
  "clienteNumero": "20100000001"
}
```
- **Respuesta:** `200 OK`, `Content-Type: image/svg+xml`, cuerpo = SVG del QR.

Útil para que tu backend en Go genere el mismo QR que SUNAT espera en el comprobante impreso o en la representación impresa.

---

## 9. Códigos y formatos comunes

- **tipoDoc (comprobantes de venta):** `01` Factura, `03` Boleta, `07` Nota de crédito, `08` Nota de débito.
- **Tipo documento cliente:** `6` RUC, `1` DNI, `4` Carné de extranjería, etc.
- **Moneda:** `PEN` (soles), `USD` (dólares).
- **Fechas:** ISO 8601 con zona, ej. `2024-01-15T10:00:00-05:00`.
- **tipAfeIgv (afectación IGV):** `10` Gravado, `20` Exonerado, `30` Inafecto, `40` Exportación.
- **Formato del body:** Puedes enviar el comprobante en la raíz del JSON o dentro de la clave **`document`**; ambos son válidos.

---

## 10. Errores y códigos HTTP

| Código | Significado |
|--------|-------------|
| 200 | OK; cuerpo según el endpoint (JSON con xml/hash/sunatResponse o CDR). |
| 204 | OK sin cuerpo (configuración guardada). |
| 400 | Parámetros faltantes o inválidos (ej. "Tipo Requerido", "Serie Requerido", "Ticket Requerido"). Body típico: `{"message": "..."}`. En validación de documento puede ser un array de `{ "field", "message" }`. |
| 403 | Token inválido o ausente. `"This action needs a valid token!"`. |
| 500 | Error interno del servidor o de SUNAT. |

En `sunatResponse`:
- `success: true` → comprobante aceptado (o ticket recibido).
- `success: false` → rechazado; revisar `error.code`, `error.message` y `cdrResponse` si viene.

---

## 11. Resumen de endpoints

Todos requieren `?token=TU_TOKEN` en las peticiones a `/api/...`.

| Recurso | POST send | POST xml | POST pdf | GET status |
|---------|-----------|----------|----------|------------|
| **Configuración** | `/api/v1/configuration/` | — | — | — |
| **Factura/Boleta** | `/api/v1/invoice/send` | `/api/v1/invoice/xml` | `/api/v1/invoice/pdf` | `/api/v1/invoice/status?tipo=&serie=&numero=&ruc=` |
| **Nota** | `/api/v1/note/send` | `/api/v1/note/xml` | `/api/v1/note/pdf` | — |
| **Resumen** | `/api/v1/summary/send` | `/api/v1/summary/xml` | `/api/v1/summary/pdf` | `/api/v1/summary/status?ticket=&ruc=` |
| **Com. baja** | `/api/v1/voided/send` | `/api/v1/voided/xml` | `/api/v1/voided/pdf` | `/api/v1/voided/status?ticket=&ruc=` |
| **Guía** | `/api/v1/despatch/send` | `/api/v1/despatch/xml` | `/api/v1/despatch/pdf` | `/api/v1/despatch/status?ticket=&ruc=` |
| **Retención** | `/api/v1/retention/send` | `/api/v1/retention/xml` | `/api/v1/retention/pdf` | — |
| **Percepción** | `/api/v1/perception/send` | `/api/v1/perception/xml` | `/api/v1/perception/pdf` | — |
| **Reversión** | `/api/v1/reversion/send` | `/api/v1/reversion/xml` | `/api/v1/reversion/pdf` | `/api/v1/reversion/status?ticket=&ruc=` |
| **Venta (QR)** | `/api/v1/sale/qr` | — | — | — |

Para esquemas detallados de cada comprobante (Invoice, Note, Summary, Voided, Despatch, Retention, Perception, Reversion) usa el archivo **`public/swagger.yaml`** del proyecto; puedes importarlo en Swagger Editor o Postman para generar clientes (incluido Go).

---

**Uso desde tu backend en Go:**  
Implementa un cliente HTTP que envíe POST/GET a estas rutas con `?token=...` y body JSON según esta guía y el `swagger.yaml`. Guarda en tu base de datos la información de empresas, comprobantes y los resultados (xml, hash, ticket, CDR) que devuelva el facturador; Lycet no persiste comprobantes ni historial.
