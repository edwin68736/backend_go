# Cierre: revisión de producción antes de desplegar (solo lectura)

27-nov-2026. Solo consultas; no se cambió nada.

| Comprobación | Resultado |
|---|---|
| Motor | MySQL 8.0.46, mismo `sql_mode` que las pruebas locales |
| Versión de esquema | todas las empresas en 192, igual que el simulacro (`CIERRE2_...`) |
| Saldos de stock duplicados | ninguno: la V193 podrá crear el índice en todas |
| Tablas que toca la migración | pequeñas en todas las empresas. La única que se copia (`tenant_inventory_document_details`, V194) tiene menos de 2 mil filas en la mayor: el bloqueo es insignificante |
| Disco del servidor de base de datos | holgado |

## Riesgo encontrado: la duración
Las dos últimas veces que una sola migración llegó a todas las empresas, tardó cerca de 30 minutos según
`tenant_migration_history`. El despliegue corta el paso remoto a los 30 minutos (`command_timeout` en
`.github/workflows/deploy-production.yml`) y ahora son ocho migraciones. No se pudo ver si aquellas veces las aplicó el
despliegue o la tarea programada: eso está en el historial de GitHub Actions.

Si se corta, la versión anterior sigue atendiendo (el reinicio no llega a ejecutarse) y puede quedar parte de las empresas
migradas. El simulacro mostró que la versión anterior funciona sobre una base migrada.

**Propuesta (sin aplicar):** subir `command_timeout` antes de desplegar y desplegar en horario de poco movimiento con la red
entre servidores en valores normales (el día de la revisión estaba varias veces por encima).
