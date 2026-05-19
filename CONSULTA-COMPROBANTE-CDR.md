# Consulta de comprobante y descarga del CDR

Documentación del endpoint **implementado** en el backend para **consultar la validez** de un comprobante (factura o boleta) en SUNAT y **obtener el CDR** (Comprobante de Recepción) cuando SUNAT lo tiene disponible.

---

## Endpoint implementado

| Acción | Método y ruta | Descripción |
|--------|----------------|-------------|
| **Consulta CDR / validez** | `GET /api/v1/invoice/status?tipo=TIPO&serie=SERIE&numero=NUMERO&ruc=RUC` | Consulta a SUNAT si existe CDR para el comprobante indicado. Si existe, devuelve el CDR en base64 y los datos parseados (validez, código, descripción). |

No existe en este backend un endpoint distinto solo para “validar” sin descargar: la consulta se hace con **invoice/status** y la respuesta incluye el CDR cuando SUNAT lo devuelve.

---

## Parámetros de query

| Parámetro | Obligatorio | Descripción |
|-----------|-------------|-------------|
| **tipo** | Sí | Tipo de comprobante según catálogo SUNAT: `01` Factura, `03` Boleta, `07` Nota de crédito, `08` Nota de débito. |
| **serie** | Sí | Serie del comprobante (ej. `F001`, `B001`). |
| **numero** | Sí | Número correlativo del comprobante (ej. `1`, `25`). |
| **ruc** | No | RUC del emisor. Obligatorio cuando hay varias empresas en `data/empresas.json`; si no se envía, se usa la configuración por defecto. |

Ejemplo de URL:

```
GET /api/v1/invoice/status?tipo=01&serie=F001&numero=1
GET /api/v1/invoice/status?tipo=03&serie=B001&numero=25&ruc=20161515648
```

No se usa token en este endpoint (la autenticación del servicio SUNAT se hace con las credenciales SOL del backend según el RUC).

---

## Respuesta exacta

- **HTTP 200** siempre que la petición sea válida (tipo, serie, numero presentes). El cuerpo es **JSON** con la estructura de resultado de consulta CDR (equivalente a StatusResult):

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

### Campos del JSON

| Campo | Tipo | Descripción |
|-------|------|-------------|
| **success** | boolean | `true` si la consulta a SUNAT fue exitosa y se obtuvo respuesta (con o sin CDR). `false` si hubo error de conexión o SUNAT no tiene CDR para ese comprobante. |
| **error** | object \| null | Si `success === false` por fallo de conexión o servicio: `{ "code": "...", "message": "..." }`. |
| **code** | string | Código de estado (ej. `"0"` cuando hay CDR aceptado). |
| **cdrZip** | string \| null | Cuando SUNAT devuelve el CDR: contenido del **ZIP del CDR en base64**. El frontend puede decodificar el base64, descomprimir el ZIP y obtener el XML del CDR para guardarlo o validar el comprobante. |
| **cdrResponse** | object \| null | Cuando hay CDR: datos parseados del CDR (aceptado, código, descripción, notas). |

### cdrResponse (cuando viene)

| Campo | Tipo | Descripción |
|-------|------|-------------|
| **accepted** | boolean | `true` = comprobante aceptado por SUNAT; `false` = rechazado. |
| **id** | string | Identificador de la respuesta. |
| **code** | string | Código SUNAT. `"0"` = aceptado. |
| **description** | string | Descripción del estado. |
| **notes** | array de string | Mensajes de detalle. |

---

## Casos de uso en el frontend

1. **Validar comprobante:** llamar a `GET /api/v1/invoice/status?tipo=01&serie=F001&numero=1`. Si **success** es `true` y **cdrResponse** existe con **code === "0"** y **accepted === true**, el comprobante está aceptado por SUNAT.
2. **Descargar el CDR:** si **success** es `true` y **cdrZip** no es `null`, decodificar el base64, descomprimir el ZIP y guardar o mostrar el XML del CDR.
3. **Comprobante no encontrado o sin CDR:** **success** puede ser `false` o **cdrZip** y **cdrResponse** pueden ser `null`; revisar **error.message** para mensajes de conexión o que el comprobante no tenga CDR en SUNAT.

---

## Errores HTTP

- **400** — Faltan parámetros obligatorios. El cuerpo es JSON con un mensaje por cada parámetro faltante, por ejemplo:
  - `{ "message": "Tipo Requerido" }`
  - `{ "message": "Serie Requerido" }`
  - `{ "message": "Numero Requerido" }`

---

## Resumen

| Qué necesitas | Cómo hacerlo |
|---------------|--------------|
| Saber si una factura/boleta está aceptada en SUNAT | `GET /api/v1/invoice/status?tipo=01&serie=F001&numero=1` y revisar `success`, `cdrResponse.code`, `cdrResponse.accepted`. |
| Descargar el CDR de ese comprobante | Misma llamada; si la respuesta trae `cdrZip`, decodificar base64 y descomprimir el ZIP para obtener el XML del CDR. |

Este endpoint es el único implementado en el backend para **consulta de comprobante** y **descarga del CDR** de facturas/boletas (y otros tipos si SUNAT los soporta en el mismo servicio). No hay otro endpoint específico de “solo validación” ni de “solo descarga CDR”; ambos se cubren con **GET /api/v1/invoice/status**.
