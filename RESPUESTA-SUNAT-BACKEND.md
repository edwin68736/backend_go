# Respuestas del backend al enviar a SUNAT

Documentación de la **estructura exacta** que devuelve el backend cuando envías factura/boleta (u otro comprobante) y qué ocurre cuando SUNAT **acepta**, **rechaza** o **no hay conexión**.

---

## ¿El backend devuelve los PDF generados?

**Sí.** Los PDF se generan y devuelven en **este mismo backend**, pero por **endpoints distintos** al de envío a SUNAT:

| Qué necesitas | Endpoint | Respuesta |
|---------------|----------|-----------|
| **Enviar a SUNAT** (factura/boleta) | `POST /api/v1/invoice/send` | JSON con `xml`, `hash`, `sunatResponse` (no incluye PDF). |
| **Obtener el PDF** (factura/boleta) | `POST /api/v1/invoice/pdf` | Archivo PDF (binario). `Content-Type: application/pdf`. El navegador puede descargarlo o mostrarlo. |
| **Obtener solo el XML** (sin enviar) | `POST /api/v1/invoice/xml` | Archivo XML. `Content-Type: text/xml`. |

Mismo criterio para los demás comprobantes:

- **Nota de crédito/débito:** `POST /api/v1/note/send` (envío), `POST /api/v1/note/pdf` (PDF), `POST /api/v1/note/xml` (XML).
- **Comunicación de baja:** `POST /api/v1/voided/send`, `POST /api/v1/voided/pdf`, `POST /api/v1/voided/xml`.
- **Resumen de boletas:** `POST /api/v1/summary/send`, `POST /api/v1/summary/pdf`, `POST /api/v1/summary/xml`.
- **Guía de remisión:** `POST /api/v1/despatch/send`, `POST /api/v1/despatch/pdf`, `POST /api/v1/despatch/xml`.
- **Retención, Percepción, Reversión:** mismo patrón (`/retention`, `/perception`, `/reversion` con `/send`, `/pdf`, `/xml`).

Para **obtener el PDF** envías el **mismo body** que para `/send` (mismo JSON de la factura/boleta) a **`/pdf`**. La respuesta es el archivo PDF en binario (no JSON con base64). En el frontend puedes usar la URL del PDF como `src` de un iframe, abrir en nueva pestaña o guardar con el nombre que trae el header `Content-Disposition`.

### Dónde “ver” el PDF

- **URL del backend Lycet:** `http://localhost:8000/` (o la base URL de tu instancia).
- **Endpoint que devuelve el PDF:**  
  `POST http://localhost:8000/api/v1/invoice/pdf?token=TU_TOKEN`  
  con el **body de la factura/boleta** en el cuerpo de la petición.
- La **respuesta** de esa URL es el archivo PDF en binario. **No hay un archivo en disco** (p. ej. `D:\lycet\algo.pdf`); el PDF solo existe como respuesta HTTP.

**Cómo verlo en la práctica:**

| Desde | Cómo |
|-------|------|
| **Frontend** | Llamar al endpoint con `fetch` o `axios`, recibir la respuesta como **blob** y mostrarla en un iframe, nueva pestaña o enlace de descarga (p. ej. `URL.createObjectURL(blob)` y `window.open(url)`). |
| **Navegador / Postman / Insomnia** | Hacer la petición POST con token y body correctos; en la respuesta, “Guardar como” o “Abrir” el PDF. |

En **Tukifac**, el panel del tenant hace `GET /api/billing/invoice/:saleId/document/pdf`; el backend Tukifac llama a Lycet `POST /api/v1/invoice/pdf` con el payload guardado y reenvía el binario al navegador para ver o descargar.

---

## Endpoint que envía a SUNAT

- **Factura / Boleta:** `POST /api/v1/invoice/send?token=TU_TOKEN`
- **Nota de crédito/débito:** `POST /api/v1/note/send?token=TU_TOKEN`
- **Comunicación de baja:** `POST /api/v1/voided/send?token=TU_TOKEN`
- **Resumen de boletas:** `POST /api/v1/summary/send?token=TU_TOKEN`
- **Guía de remisión:** `POST /api/v1/despatch/send?token=TU_TOKEN`
- (Otros: retention, perception, reversion tienen el mismo patrón.)

En todos los casos, si el backend llega a enviar el XML a SUNAT, la respuesta HTTP es **200** y el body es un JSON con la estructura siguiente. Solo cambia el objeto interno de la clave `sunatResponse` (BillResult, SummaryResult, etc.).

---

## Estructura exacta de la respuesta (factura/boleta)

Cuando llamas a **`POST /api/v1/invoice/send`** (y el body es válido), el backend **siempre** responde **HTTP 200** con un JSON de esta forma:

```json
{
  "xml": "<string con el XML firmado enviado a SUNAT>",
  "hash": "<resumen de la firma digital del XML>",
  "sunatResponse": {
    "success": true | false,
    "error": { "code": "...", "message": "..." } | null,
    "cdrZip": "<base64 del ZIP del CDR> | null",
    "cdrResponse": {
      "accepted": true | false,
      "id": "...",
      "code": "0" | "<código de error SUNAT>",
      "description": "...",
      "notes": [ "<mensaje 1>", "<mensaje 2>" ]
    } | null
  }
}
```

### Campos del objeto raíz

| Campo | Tipo | Descripción |
|-------|------|-------------|
| **xml** | string | XML firmado que se envió (o se intentó enviar) a SUNAT. Siempre se devuelve si la petición llegó a la etapa de envío. |
| **hash** | string | Resumen (hash) de la firma digital del XML. |
| **sunatResponse** | object | Respuesta de SUNAT (o error de conexión). Ver abajo. |

### Campos de `sunatResponse` (BillResult)

| Campo | Tipo | Cuándo viene | Descripción |
|-------|------|---------------|-------------|
| **success** | boolean | Siempre | `true` si SUNAT aceptó el comprobante; `false` si rechazó o hubo error de conexión. |
| **error** | object \| null | Solo si `success === false` por error de conexión/timeout | `{ "code": "...", "message": "..." }`. Mensaje descriptivo del fallo (ej. "Connection refused", "Timeout"). |
| **cdrZip** | string \| null | Solo si `success === true` | ZIP del CDR (Comprobante de Recepción) en **base64**. Puedes decodificarlo y descomprimir para obtener el XML del CDR. |
| **cdrResponse** | object \| null | Cuando SUNAT respondió (aceptó o rechazó con CDR) | Objeto parseado del CDR. Si SUNAT rechazó, aquí vienen el código y los mensajes. |

### Campos de `sunatResponse.cdrResponse` (cuando SUNAT respondió)

| Campo | Tipo | Descripción |
|-------|------|-------------|
| **accepted** | boolean | `true` = comprobante aceptado; `false` = rechazado. |
| **id** | string | Identificador de la respuesta (ej. número de ticket o referencia). |
| **code** | string | Código de estado SUNAT. **"0"** = aceptado; otro valor = error/rechazo (ej. "3205", "233"). |
| **description** | string | Descripción breve del estado. |
| **notes** | array de string | Lista de mensajes de detalle (ej. descripción de cada observación o error). |

---

## 1. SUNAT acepta el comprobante

- **HTTP:** 200  
- **sunatResponse.success:** `true`  
- **sunatResponse.error:** `null`  
- **sunatResponse.cdrZip:** string en base64 (ZIP del CDR).  
- **sunatResponse.cdrResponse:** presente; **code** `"0"`, **accepted** `true`; **notes** suele ser array vacío o mensajes informativos.

Ejemplo:

```json
{
  "xml": "<?xml version=\"1.0\"...",
  "hash": "ABC123...",
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

---

## 2. SUNAT rechaza el comprobante

SUNAT responde con un CDR de rechazo (código distinto de "0"). El backend **sigue devolviendo HTTP 200**; la aceptación o rechazo se ve en **sunatResponse.success** y **sunatResponse.cdrResponse**.

- **HTTP:** 200  
- **sunatResponse.success:** `false`  
- **sunatResponse.error:** puede ser `null` o tener datos si además hubo un error al procesar.  
- **sunatResponse.cdrZip:** puede venir en base64 si SUNAT envió CDR de rechazo.  
- **sunatResponse.cdrResponse:** presente; **code** distinto de `"0"` (ej. `"3205"`), **accepted** `false`; **description** y **notes** con los mensajes de SUNAT.

Ejemplo (rechazo por listID vacío):

```json
{
  "xml": "<?xml version=\"1.0\"...",
  "hash": "...",
  "sunatResponse": {
    "success": false,
    "error": null,
    "cdrZip": null,
    "cdrResponse": {
      "accepted": false,
      "id": "...",
      "code": "3205",
      "description": "Debe consignar el tipo de operación",
      "notes": [
        "Detalle: xxx value='ticket: ... error: INFO: 3205 (nodo: \"cbc:InvoiceTypeCode/listID\" valor: \"\")'"
      ]
    }
  }
}
```

En el frontend debes revisar **sunatResponse.success** y **sunatResponse.cdrResponse.code** y **notes** para mostrar por qué rechazó SUNAT.

---

## 3. No hay conexión con SUNAT (timeout, red, etc.)

No se recibe CDR; la librería devuelve un error de conexión.

- **HTTP:** 200 (el backend no cambia el código HTTP por el resultado SUNAT).  
- **sunatResponse.success:** `false`  
- **sunatResponse.error:** `{ "code": "...", "message": "..." }` con el mensaje del error (ej. "Connection refused", "Timeout", "Could not connect to host").  
- **sunatResponse.cdrZip:** `null`  
- **sunatResponse.cdrResponse:** puede ser `null` o venir vacío.

Ejemplo:

```json
{
  "xml": "<?xml version=\"1.0\"...",
  "hash": "...",
  "sunatResponse": {
    "success": false,
    "error": {
      "code": "0",
      "message": "Error al conectar con SUNAT: Connection timed out"
    },
    "cdrZip": null,
    "cdrResponse": null
  }
}
```

En el frontend debes revisar **sunatResponse.error.message** para informar que no se pudo enviar por conexión.

---

## Resumen: qué revisar en el frontend

| Situación | Comprobar | Dónde ver el detalle |
|-----------|-----------|------------------------|
| **Aceptado** | `sunatResponse.success === true` y `cdrResponse.code === "0"` | Guardar `cdrZip` (base64) para el CDR. |
| **Rechazado por SUNAT** | `sunatResponse.success === false` y `sunatResponse.cdrResponse` presente | `cdrResponse.code`, `cdrResponse.description`, `cdrResponse.notes`. |
| **Sin conexión / error de red** | `sunatResponse.success === false` y `sunatResponse.error` presente | `sunatResponse.error.message`. |

Siempre que la petición sea válida (body correcto, token válido, empresa existente), el backend responde **200** y devuelve **xml**, **hash** y **sunatResponse**. No se usa otro código HTTP para “rechazo” o “sin conexión”; todo va en el cuerpo del JSON.

---

## Respuestas antes de enviar a SUNAT (validación del backend)

Si el backend **no** llega a enviar a SUNAT (por ejemplo, faltan campos obligatorios), responde con **HTTP 4xx** y un JSON distinto, sin `xml` ni `sunatResponse`:

- **400** – Datos inválidos o faltantes (ej. falta `tipoOperacion` o `tipoDoc`):

```json
{
  "error": "Faltan datos obligatorios para el comprobante.",
  "campos_requeridos": ["tipoOperacion", "tipoDoc"],
  "mensaje": "..."
}
```

- **401/403** – Token inválido o no enviado.  
- **404** – RUC de la empresa no registrado en `empresas.json` (según implementación).

En esos casos no hay `sunatResponse`; el frontend debe tratar el 4xx y el cuerpo como error de validación o autorización, no como respuesta de SUNAT.

Este texto es e prueba