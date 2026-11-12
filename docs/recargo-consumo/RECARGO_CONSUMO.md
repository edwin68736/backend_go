# Recargo al consumo (hoteles, restaurantes)

Estado: fases 1–3 en la rama `feature/recargo-consumo` (backend). Faltan reportes/caja (fase 4) y pantallas de Tukifac y Tukichef (fase 5). **No activar en producción hasta tener pantallas**: el backend ya cobra y emite, pero nadie puede marcar «cobrar recargo» ni configurarlo desde la interfaz.

## Norma (verificada en SUNAT, 12-nov-2026)
- Quinta Disposición Complementaria del DL 25988: los establecimientos de hospedaje o expendio de comidas y bebidas, **en acuerdo con sus trabajadores**, pueden fijar un recargo al consumo **no mayor al 13 %** del valor de los servicios que prestan (no hay norma de 10 %; ese es solo uso común).
- No forma parte de la base imponible del IGV, aunque figure en el comprobante (Oficio 257-2003-2B0000; art. 14 de la Ley del IGV).
- No es ingreso del negocio sino de los trabajadores (Informe 049-2014-SUNAT/4B0000); para ellos no es remunerativo pero sí renta de 5.ª categoría (régimen 728) o 4.ª (CAS) (Informe 191-2019-SUNAT/7T0000). Si el negocio no lo reparte completo, sí lo tributa.
- La norma no dice si el % se aplica antes o después del IGV. **Decisión: sobre el valor de venta sin IGV, tras descuentos** (lectura conservadora; nunca excede el tope).
- Indecopi prohíbe cobrar montos adicionales desagregados del precio, con la excepción de este recargo; conviene avisarlo en la carta/ticket. Hay proyectos de ley (2025) que lo harían obligatorio informarlo; no consta que sean ley.

## Comprobantes electrónicos (guías UBL 2.1 de SUNAT)
- Factura y boleta: `cac:AllowanceCharge` global (ChargeIndicator true, código **46** del catálogo 53, factor, monto, base) + `LegalMonetaryTotal/ChargeTotalAmount`. Valor de venta, IGV y `TaxInclusiveAmount` no lo incluyen; `PayableAmount` sí.
- Nota de crédito/débito: solo `ChargeTotalAmount` (no tienen nodo de cargo global). Anulación total → recargo completo; devolución parcial → proporcional al valor devuelto (la nota que cierra lo pendiente se lleva el resto exacto).
- Resumen diario: cada línea admite `AllowanceCharge/Amount`; `TotalAmount` ya incluye el recargo. (Greenter: `mtoOtrosCargos`; el payload Go lo envía, sin probar contra SUNAT.)
- Comunicación de baja: sin importes.

## Diseño
- `tenant_company_configs.service_charge_enabled` (default apagado) y `service_charge_rate` (default 10, validado 0 < r ≤ 13). Migración V188.
- La venta (`apply_service_charge`), el cobro de mesa y el POS de Tukichef piden cobrarlo; el % **siempre** sale de la configuración del servidor. Se rechaza si el tenant lo tiene apagado, si el % supera 13 o si se combina con anticipos o detracción.
- `tenant_sales.service_charge_rate/amount`; `total` ya lo incluye (caja, pagos y saldos lo ven sin cambios). Cobro dividido de mesa: cada comprobante calcula el suyo.
- Código: `pkg/tax/service_charge.go`, `internal/billing/service/service_charge_payload.go`.

## Pruebas en SUNAT beta (12-nov-2026), todas aceptadas
Factura F001-25 (2 líneas, recargo 20 sobre 200) y F001-26; boleta B001-55; NC parcial FC01-7 (recargo 5), NC total FC01-8, NC de boleta BC01-11.

## Pendiente
- Fase 4: reportes y caja (renglón aparte «recargo al consumo», no es ingreso), registro de ventas/PLE (columna de otros cargos), print_data/tickets, dashboards.
- Fase 5: ajustes en Tukifac y Tukichef, línea de recargo en cobro/precuenta/ticket, `GET` de la configuración con PIN en Tukichef, retirar el placeholder «Propina» del producto manual.
- Notas de débito con recargo, resumen diario contra SUNAT, detracción/percepción/anticipos con recargo (hoy se rechaza la combinación).
- Reporte de reparto a trabajadores (opcional).
