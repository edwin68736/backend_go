# F4b — Pantallas de recetas en Tukichef: informe

Fecha: 25-nov-2026 · Estado: **implementado y probado en local; sin push, sin deploy, sin tocar producción.** F5 no se empezó.
Alcance: `F4b_PROPUESTA.md` con las 4 recomendaciones.

## 1. Qué quedó
- Menú **Recetas** (permiso `rc.v` o `rc.m` y módulo activado): lista con búsqueda, categoría y estado; porciones disponibles e insumo
  limitante; con permiso de costos, costo, food cost y precio sugerido.
- Editor: ingredientes con cantidad en la unidad base o g/ml, merma, rendimiento y objetivo propio; pestañas por presentación; combos en
  solo lectura. Los números se muestran al guardar.
- **Aplicar precio** con confirmación (precio actual, nuevo, food cost resultante y dónde se aplica) e historial de cambios.
- **Precio sugerido**: food cost objetivo (muestra el margen equivalente), redondeo, dirección y base de costo.
- En **Productos**, botón «Receta» en cada plato. El cocinero ve las recetas sin costos ni botones.
- Backend: `category_id`/`category_name` en la lista, `user_name` en el historial y `net_factor` en los costos.

La versión de Tukichef no se subió (se sube al cerrar el módulo).

## 2. Pruebas
- `tsc -b` y 141 pruebas de Tukichef (nuevas en `utils/recipes.test.ts`). Se añadió un simulacro de `useNavAccess` a
  `catalogPermissions.test.tsx`, porque Productos ahora consulta el acceso a Recetas.
- Backend: pruebas de recetas en SQLite y MariaDB estricto.
- Recorrido en navegador (local, empresa `demo` migrada a V198, datos creados para la prueba):
  1. Desde Productos, receta de una hamburguesa (150 g carne, 1 pan, 10 g queso) → costo 4.15, 13 porciones, food cost 32.65 %,
     sugerido 16.40.
  2. Aplicar 16.40 → catálogo con el precio nuevo e historial con usuario. **Una mesa abierta antes del cambio conservó S/ 15.00.**
  3. Pizza con presentaciones: receta común heredada y propia para la familiar (10.50, alcanza para 3).
  4. Combo de dos hamburguesas: 8.30, alcanza para 6.
  5. Cocinero en ancho de celular: menú, lista y receta sin ningún costo.
  6. Cambiar el redondeo a 0.50 → sugerido 16.50 (se volvió a 0.10).

## 3. No probado
- Desde pantalla: quitar receta, el aviso de precio cambiado (409, probado solo en backend), sucursal con precio propio, plato incompleto,
  merma y rendimiento distinto de 1, usuario con `rc.m` pero sin costos, negocio con el módulo apagado.
- Usar el POS (se comprobó el precio que entrega el catálogo, no la venta en pantalla).
- Apps compiladas (Android/escritorio), MySQL 8 y producción.

## 4. Observaciones
- En celular, la barra inferior no incluye Recetas: se entra desde Inicio o el menú lateral.
- Quien ya tenga sesión abierta puede necesitar volver a ingresar para ver el menú.
- Los platos de prueba «QA Hamburguesa», «QA Pizza», «QA Combo» y sus insumos quedaron en la `demo` local.
- El análisis de código (`eslint`) marca `setState` dentro de efectos, igual que ya ocurre en la pantalla de Insumos.
