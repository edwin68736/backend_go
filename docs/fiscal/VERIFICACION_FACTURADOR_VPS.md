# Verificación del JMS del facturador (VPS) antes de desplegar la corrección de fechas

Estado: **pendiente de acceso**. El facturador vive en otro VPS (`2.24.110.236`, PHP/Symfony, deploy tipo release sin git) y el acceso que existía era por contraseña de root entregada de forma puntual; no se guarda. Para auditarlo hace falta una **llave SSH dedicada de solo lectura** (ver el proceso en la memoria del proyecto: una llave por VPS, instalada por el dueño, sin probar llaves ajenas contra el host). **No se ejecutó nada contra ese servidor.**

## Referencia local (el JMS con el que se validó la corrección)
- `jms/serializer` **3.32.7** (`facturador_lycet/composer.lock`); Greenter **v5.3.0** (`greenter/greenter`).
- `vendor/jms/serializer/src/Handler/DateHandler.php` → sha256 `3c871c4ba5309458f9e2f85b4554fdc6de77f389f78b61481ea920cca6726a15`.
- El formato exigido (`Y-m-d\TH:i:sP`) sale de `DateHandler.php` (formato por defecto y `createFromFormat` con ese único formato); declarado en `config/serializer/Retention.*.yml` y `Perception.*.yml`.

## Verificación propuesta (solo lectura, sin emitir, sin SUNAT, sin tocar BD)
1. Localizar el release activo: `readlink -f /home/tukifac-facturador/htdocs/facturador.tukifac.com/current`.
2. Comparar versión de JMS y huella del handler con la referencia local:
   `grep -A3 '"name": "jms/serializer"' <release>/composer.lock | grep version` y
   `sha256sum <release>/vendor/jms/serializer/src/Handler/DateHandler.php`.
   Igualdad ⇒ el contrato validado localmente es el del servidor.
3. Comparar los YAML del serializer: `sha256sum <release>/config/serializer/Retention.*.yml <release>/config/serializer/Perception.*.yml` contra los de `facturador_lycet/config/serializer`.
4. (Opcional, equivalente a `/retention/xml` pero sin HTTP ni firma): ejecutar en el VPS, con el PHP del servidor, el verificador del repo
   `backend_principal/internal/billing/service/testdata/verify_facturador_dates.php` contra el `vendor` del release. Hace deserialización (JMS) y render de la plantilla Greenter con un payload de prueba y un control negativo que reproduce el error de producción. No usa red, certificados ni BD.
5. **No** llamar a `/retention/xml` ni `/retention/send` con datos reales: el endpoint firma con el certificado del RUC y puede dejar registros en `fiscal_documents`. Si se quisiera la prueba HTTP, hacerla con una empresa de pruebas del facturador (beta SUNAT) y con autorización expresa.

## Payloads antiguos encolados (verificado hoy, solo lectura)
- **Backend (BD de los 788 tenants):** las 5 retenciones están `job_status=failed`, `pipeline_status=FAILED`, `next_retry_at=NULL` (no hay reintentos programados). No hay percepciones. Ningún documento de retención/percepción está en estado no terminal.
  Observación: `retry_count` de doriconta R001-1 = **181** e industrialrafaz R001-1 = **183** (reintentos históricos antes de caer en FAILED; conviene revisar el límite de reintentos del *worker* de reenvío).
- **Redis del backend (VPS 72.62.83.69):** `tukifac:billing:queue`, `:processing`, `:dead`, `tukifac:fiscal:queue` y `:processing` → todas con **0** elementos.
- **Cola propia del facturador (`fiscal_documents` / `fiscal_emit_attempts` en la BD del facturador):** **no verificado** (requiere el acceso de arriba). Consulta de solo lectura sugerida: documentos de tipo retención/percepción con estado no terminal y la fecha del último intento.
- Las 5 retenciones **no se reencolan** hasta autorización por cliente (ver `REGULARIZACION_RETENCIONES.md`).
