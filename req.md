DOCUMENTO DE REQUERIMIENTOS – MÓDULO FINANCIERO ERP
Este documento detalla los requerimientos funcionales y técnicos del módulo financiero inicial del nuevo sistema ERP del estudio contable. Este sistema será independiente de Tukifac, pero se integrará con él únicamente para sincronizar documentos electrónicos.
1. OBJETIVO DEL MÓDULO
El módulo permitirá gestionar cuentas por cobrar, pagos, saldos de clientes, ingresos del estudio y estados de cuenta. Servirá como base del core financiero del ERP.
2. INTEGRACIÓN CON TUKIFAC
El sistema se conectará mediante API a Tukifac para consultar documentos electrónicos emitidos. No almacenará la factura completa, solo su referencia financiera.
Datos sincronizados desde Tukifac:
• ID del documento (requerido)
• Tipo de comprobante (requerido)
• Número de documento (requerido)
• Fecha de emisión (requerido)
• Monto total (requerido)
• Estado del documento (requerido)
3. GESTIÓN DE CLIENTES / EMPRESAS
Campos requeridos:
• RUC
• Razón social
• Código interno del estudio
• Estado del cliente
Campos opcionales:
• Nombre comercial
• Dirección
• Teléfono
• Correo electrónico
• Fecha de inicio de servicio
4. CONTACTOS RESPONSABLES POR EMPRESA
Cada empresa podrá registrar uno o varios contactos responsables para la comunicación con el estudio.
Campos requeridos:
• Nombre completo
• Cargo en la empresa
• Teléfono o celular
• Correo electrónico
Campos opcionales:
• Observaciones
• Prioridad de contacto
5. FUNCIONALIDADES FINANCIERAS
• Sincronizar facturas desde Tukifac
• Registrar cargos manuales
• Registrar pagos manuales
• Adjuntar comprobantes
• Calcular saldos automáticamente
• Generar estados de cuenta
• Historial financiero por cliente
6. CONFIGURACIÓN DEL ESTUDIO
El sistema contará con un panel de configuración del estudio.
Campos configurables:
• Nombre del estudio (requerido)
• RUC del estudio (requerido)
• Dirección (requerido)
• Teléfono (opcional)
• Correo institucional (opcional)
• Logo del estudio (opcional)
7. ROLES Y PERMISOS
El sistema incluirá gestión de usuarios con control de acceso por roles.
Roles sugeridos:
• Administrador
• Supervisor
• Contador
• Asistente
Permisos configurables:
• Ver clientes
• Registrar pagos
• Sincronizar facturas
• Generar reportes
• Configurar el sistema
8. VISTAS DEL SISTEMA (ESTIMADAS)
• Dashboard financiero
• Gestión de clientes
• Contactos por empresa
• Estado de cuenta del cliente
• Registro de pagos
• Reportes financieros
• Configuración del estudio
• Gestión de usuarios y roles
