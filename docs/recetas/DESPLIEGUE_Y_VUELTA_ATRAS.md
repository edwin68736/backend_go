# Insumos y recetas: despliegue y vuelta atrás

27-nov-2026. El módulo llega **apagado**: nadie lo ve hasta que se enciende por empresa en el panel central.

## Antes
- Comprobar la red entre el servidor de la aplicación y el de la base de datos (normal: 1 a 2 ms). Con la red degradada,
  esperar: la migración tardaría mucho más.
- Horario de poco movimiento, **no a las 3:00 de Perú** (corre la tarea programada de migraciones).
- Anotar la versión desplegada, que es la de vuelta atrás: `1ec9088` (confirmar en el servidor).

## Orden
1. Backend (`git push origin main` y después GitHub → Actions → Deploy Production, **sin** omitir migrate). Cuenta con 30 a 60
   minutos: migra todas las empresas antes de reiniciar. Mientras tanto la versión anterior sigue atendiendo.
2. Panel central.
3. Tukifac.
4. Tukichef y las apps (antes: probar la gaveta con una impresora real, porque cambia cuándo se envía el pulso).

## Después
- Activar **una sola empresa de prueba** en el panel central.
- Emitir un comprobante real con un plato con receta y revisar lo guardado y su CDR antes de abrirlo a los demás.

## Si el despliegue se corta
La versión anterior sigue atendiendo. Volver a lanzar el despliegue: continúa con las empresas que faltan.

## Vuelta atrás
`deploy/scripts/rollback.sh` **no sirve** con este despliegue: lee `.deploy/previous-image`, que el flujo de GitHub no escribe.
A mano, en el servidor de la aplicación:

1. Si alguna empresa usó el módulo, desactivar antes sus insumos: la versión anterior los vería como productos con precio 0.
2. En el `.env` de `/opt/tukifac`, poner en `TUKIFAC_IMAGE` la etiqueta de la versión anterior (`:sha-1ec9088`) en lugar de
   `:latest`.
3. `docker compose -f docker-compose.production.yml pull backend-go`
4. `docker compose -f docker-compose.production.yml up -d --no-deps --force-recreate backend-go`

No hace falta deshacer la base: el simulacro mostró que la versión anterior funciona sobre una base ya migrada.
No lanzar el despliegue de `main` mientras tanto, porque volvería a poner la versión nueva.
Esta vuelta atrás **no se ha ensayado en el servidor**.
