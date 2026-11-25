# F4b — Pantallas de recetas en Tukichef: diagnóstico y propuesta

Fecha: 25-nov-2026 · Estado: **PROPUESTA; no se modificó código.** Espera autorización expresa.
Base: `F4_PROPUESTA.md` (aprobada) y `F4a_INFORME.md` (backend, commit 5ed0258, sin push).

## 1. Diagnóstico
- El patrón de Insumos sirve tal cual: `PageShell`, búsqueda, tarjetas, `SupplyModalShell`, permiso + módulo en `useNavAccess`.
- `ProductosPage.tsx` tiene 1854 líneas: solo se le añade el acceso «Receta» en cada plato.
- La lista de `GET /recipes` llega completa (sin paginar), así que se puede buscar y filtrar en el teléfono.
- Faltan dos datos en el backend: la **categoría** en la lista (para filtrar) y el **nombre de quien cambió el precio** en el historial.
- El detalle de un combo trae su costo total y lo que le falta, **no el desglose por componente**.

## 2. Qué verá el usuario
**Menú «Recetas»** (Gestión).
- Lista de platos (una fila por presentación): precio, para cuántos alcanza y qué insumo limita. Con permiso de costos: costo, food cost,
  margen y precio sugerido.
- Filtros: buscar, categoría y estado (todos · sin receta · incompletas · sobre el objetivo).

**Receta de un plato** (ventana, a pantalla completa en celular).
- Presentaciones como pestañas, más «Todas las presentaciones» (la común).
- Líneas: insumo (buscador), cantidad, unidad (la base o g/ml), merma %, y costo de la línea.
- «Rinde N porciones» y, con permiso de costos, objetivo propio de food cost.
- Guardar y, después, los números calculados por el servidor. Quitar receta.
- Combo: solo lectura (costo, disponibilidad y componentes sin receta).

**Precio** (solo con permiso).
- Botón «Aplicar precio», con el sugerido ya escrito y editable.
- La confirmación dice el precio actual, el nuevo, el food cost resultante y **dónde se aplica**: «solo en esta sucursal» o «precio general:
  afecta a todas las sucursales sin precio propio».
- Si el precio cambió entretanto, avisa y recarga. Historial de cambios.

**Configuración de precios**: food cost objetivo (o «margen deseado», que es lo mismo: margen 70 % = food cost 30 %), redondeo,
dirección y base de costo.

**Productos**: acción «Receta» en cada plato.

**Cocinero**: la misma lista y la receta con cantidades y disponibilidad, sin ningún costo ni botones.

## 3. Cambios
| Archivo | Cambio |
|---|---|
| `pages/RecetasPage.tsx` (nuevo) | lista y filtros |
| `components/recipes/*` (nuevo) | editor, selector de insumo, aplicar precio, historial, configuración |
| `services/recipes.service.ts`, `utils/recipes.ts` + pruebas (nuevos) | llamadas, validaciones del formulario, textos |
| `config/restaurantNav.ts`, `utils/restaurantPermissions.ts`, `hooks/useNavAccess.ts`, `App.tsx` | menú «Recetas» (`rc.v`/`rc.m` + módulo) |
| `pages/ProductosPage.tsx` | acceso «Receta» |
| Backend | `category_name` en la lista y `user_name` en el historial (dos campos, con prueba) |

Las fórmulas **no se repiten** en el teléfono: todos los números vienen del servidor.

## 4. Riesgos y dependencias
- «Aplicar precio» cambia lo que se cobra. El POS toma el precio nuevo al volver a cargar los productos; **verificar en el recorrido** que
  una mesa ya abierta conserva el precio con el que se pidió.
- El permiso `rc.v` es nuevo: quien tenga sesión abierta puede necesitar volver a ingresar para ver el menú.
- Para el recorrido hay que migrar la empresa local `demo` a V198 (solo local).
- En Tukichef hay 3 archivos modificados que no son de este trabajo: no se tocan ni se incluyen en el commit.
- Versión de Tukichef: sin subir ahora; se sube una vez al cerrar el módulo.
- Sigue sin respaldo: todo existe solo en esta PC.

## 5. Pruebas
`tsc`, pruebas de `utils/recipes` y de acceso por permiso; pruebas del backend para los dos campos. Recorrido en navegador (local,
`demo`): crear la receta de una hamburguesa, ver costo y disponibilidad, aplicar precio y comprobarlo en el POS, presentaciones, un combo,
usuario cocinero y ancho de celular.

## 6. Fuera de F4b
Descuento al cobrar (F5), disponibilidad en el POS (F5), reportes (F6), desglose de combos, vista previa del costo antes de guardar.

## 7. Decisiones
1. **Números al guardar**, sin vista previa mientras se escribe. Recomendado: sí (evita duplicar las fórmulas).
2. **Una sola entrega F4b** (con los dos campos de backend). Recomendado: sí.
3. **El cocinero ve el menú «Recetas»** (ya aprobado que ve sin costos). Recomendado: sí.
4. **Desglose del costo de un combo**: dejarlo para después. Recomendado: sí.

## 8. No verificado
Solo se leyó código. No se probó cómo se ve la lista con una carta grande, ni el efecto de un cambio de precio sobre mesas abiertas.
