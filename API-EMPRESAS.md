# API Empresas (multiempresa)

Base: `GET/POST /api/v1/empresas` y `PATCH /api/v1/empresas/{ruc}/ambiente`.

## Reglas

- **RUC** es siempre obligatorio e identifica a la empresa.
- **Al registrar una nueva empresa**: son obligatorios **SOL_USER** y **SOL_PASS** (Clave SOL). Certificado y logo son opcionales.
- **Al actualizar**: solo se modifican los campos que envíes. Si no envías certificado ni logo, se mantienen los que ya tenía la empresa. Puedes enviar solo `ambiente`, solo `SOL_USER`, solo `SOL_PASS`, o cualquier combinación.

---

## Listar empresas

```http
GET /api/v1/empresas
```

---

## Obtener una empresa

```http
GET /api/v1/empresas/{ruc}
```

Ejemplo: `GET /api/v1/empresas/20123456789`  
404 si el RUC no está registrado.

---

## Crear o actualizar empresa(s)

### Una sola empresa (recomendado desde frontend)

```http
POST /api/v1/empresas
Content-Type: application/json
```

**Registrar nueva:**

```json
{
  "ruc": "20123456789",
  "SOL_USER": "20123456789MODDATOS",
  "SOL_PASS": "miClaveSOL",
  "ambiente": "pruebas",
  "certificate_base64": "(opcional)",
  "logo_base64": "(opcional)"
}
```

- **ruc**: obligatorio (11 dígitos).
- **SOL_USER**, **SOL_PASS**: obligatorios al crear.
- **ambiente**: opcional; si no se envía, se usa `"pruebas"`. Valores: `"pruebas"` o `"produccion"`.
- **certificate_base64** / **logo_base64**: opcionales.

**Actualizar (solo lo que quieras cambiar):**

Solo ambiente:

```json
{
  "ruc": "20123456789",
  "ambiente": "produccion"
}
```

Solo Clave SOL:

```json
{
  "ruc": "20123456789",
  "SOL_USER": "20123456789NUEVOUSER",
  "SOL_PASS": "nuevaClave"
}
```

Si no envías `certificate_base64` ni `logo_base64`, no se tocan el certificado ni el logo ya guardados.

### Varias empresas a la vez

```json
{
  "empresas": {
    "20123456789": {
      "SOL_USER": "...",
      "SOL_PASS": "...",
      "ambiente": "pruebas",
      "certificate_base64": "...",
      "logo_base64": "..."
    }
  }
}
```

---

## Cambiar solo el ambiente (producción ↔ pruebas)

```http
PATCH /api/v1/empresas/{ruc}/ambiente
Content-Type: application/json
```

**Body:**

```json
{
  "ambiente": "produccion"
}
```

o `"ambiente": "pruebas"`. No modifica Clave SOL, certificado ni logo.

**Validación al pasar a producción:** Si envías `"ambiente": "produccion"`, el backend comprueba que la empresa tenga:
- **Usuario SOL** (SOL_USER) configurado
- **Contraseña SOL** (SOL_PASS) configurada  
- **Certificado digital** configurado y que el archivo del certificado exista en `data/`

Si falta alguno, responde **400** con un mensaje indicando qué falta (por ejemplo: *"Para pasar a producción la empresa debe tener usuario SOL, contraseña SOL y certificado digital configurados. Faltan: certificado digital."*). De pruebas a pruebas no se valida.

- **200**: ambiente actualizado.
- **404**: RUC no registrado.
- **400**: body sin `"ambiente"`, valor no válido, o al pasar a **produccion** faltan usuario SOL, contraseña SOL o certificado.

---

## Errores

| Código | Causa |
|--------|--------|
| 400 | JSON inválido; falta `ruc`; o al **crear** falta `SOL_USER` o `SOL_PASS`; o al pasar a **produccion** faltan usuario SOL, contraseña SOL o certificado. |
| 404 | RUC no existe (GET una empresa o PATCH ambiente). |
