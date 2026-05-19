# Forma de pago vs Métodos de pago internos

Según la normativa de SUNAT, el sistema distingue entre:

## Forma de pago (SUNAT)

Para el envío de comprobantes electrónicos a SUNAT, el sistema **siempre utiliza la forma de pago Contado**, ya que las ventas registradas corresponden a pagos realizados en el momento.

- **Campo en payload:** `formaPago: { tipo: "Contado" }`
- **Implementación:** El backend (`internal/billing/service`) envía siempre `FormaPago: &InvoiceFormaPago{Tipo: "Contado"}` en el payload a Lycet/SUNAT.

## Métodos de pago internos

El sistema maneja internamente los **métodos de pago** propios del negocio, configurados en el panel del tenant:

- Efectivo
- Yape
- Plin
- Tarjeta
- Transferencia
- Otros (según cuentas bancarias configuradas)

Estos métodos se utilizan para:

- **Control de caja:** Registrar ingresos por método
- **Reportes:** Totales por método de pago
- **Conciliación:** Vincular pagos con cuentas bancarias
- **Ventas con múltiples pagos:** El cliente puede pagar parte en efectivo y parte con Yape, etc.

## Flujo de venta

1. El usuario selecciona uno o más **métodos de pago internos** (efectivo, Yape, etc.) y los montos.
2. El sistema registra la venta con `TenantSalePayment` (múltiples pagos posibles).
3. Al enviar a SUNAT, el comprobante electrónico lleva **siempre** `formaPago: Contado`.

## API

- **GET /api/cashbank/payment-methods:** Lista los métodos de pago internos del tenant (estándares + configurados en cuentas bancarias).
