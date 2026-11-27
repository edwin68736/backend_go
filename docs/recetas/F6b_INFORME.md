# F6b: pantallas de reportes de insumos y platos (Tukichef)

27-nov-2026. Rama `insumos-recetas`. Sin desplegar. Usa los endpoints de F6a (`F6a_INFORME.md`); el backend no cambia.

## Qué se entregó
En Tukichef → Reportes, seis pestañas nuevas, con Excel y PDF. Solo aparecen con el módulo de insumos activado.

| Pestaña | Contenido |
|---|---|
| Existencias de insumos | stock, mínimo, estado, costo promedio, último costo, valor; opción «solo avisos» |
| Compras por insumo | compras del período en unidad base y sin IGV |
| Consumo de insumos | consumido, repuesto, neto, costo y cantidad sin costo |
| Mermas y desperdicios | lo registrado a mano, con usuario y motivo |
| Costos de la carta | costo por porción, precio, food cost, margen y porciones disponibles hoy |
| Rentabilidad de platos | ventas del período sin IGV contra el costo de lo que salió |

## Reglas de pantalla
- Lo que no se conoce sale como «—» en pantalla y PDF, y vacío en Excel. Nunca como S/ 0.00.
- Sin permiso de ver costos, el servidor no manda costos ni valores.
- El «último costo» es el de la última compra del insumo en cualquier sucursal; el promedio y el stock son de la sucursal elegida.
- Los filtros se reinician al cambiar de reporte (vale también para los reportes que ya existían).

## Cambios que tocan los reportes existentes
- Las pestañas pasan a una fila propia debajo del título; en celular se desliza hasta la pestaña activa.
- El PDF admite columnas con texto en varias líneas (`wrap`). Solo lo usan los reportes nuevos: sin esa opción el PDF sale igual.
  Los reportes anteriores siguen cortando el texto largo a la primera línea.

## Encontrado al probar y corregido
- Al recargar la página de un reporte nuevo se volvía a «ventas»: no esperaba a saber si el módulo estaba activo.
- Consumo y Rentabilidad mostraban S/ 0.00 donde el costo se desconocía.
- El PDF cortaba «Hamburguesa doble» a «Hamburguesa».

## Pruebas
- `tsc -b`, `vitest` (25 archivos, 155 pruebas) y `npm run build`: bien.
- Lint: en los archivos nuevos solo `react-refresh/only-export-components` en las dos definiciones, igual que las existentes.
- En pantalla, local (MySQL 8.0.30), empresa desechable con compras, merma, desperdicio, ventas de platos con y sin receta y una nota de
  venta anulada:
  - las seis pantallas, con cifras cuadradas entre sí (consumo S/ 57.36 = costo de rentabilidad; stock = inicial + compras − salidas);
  - Excel y PDF de las seis (contenido del archivo leído dentro del navegador);
  - módulo apagado: sin pestañas, el enlace directo vuelve a «ventas» y las cinco rutas de la API responden 403;
  - selector de sucursal con una segunda sucursal vacía;
  - ancho de celular.

## Sin probar
- Roles distintos de administrador (los permisos están probados en el backend, F6a).
- Más de una página de resultados en pantalla ni exportación de varias páginas (compras y mermas).
- Nota de crédito parcial vista en pantalla (probada en el backend, F6a).
- Apps compiladas (Android y escritorio), incluida la descarga de Excel y PDF en ellas.
- MySQL 8.0.46, producción y SUNAT (F6b no emite nada).

## Límites
- El PDF no lleva fila de totales ni el período; el Excel sí lleva totales. Es como ya funcionaban los reportes.
