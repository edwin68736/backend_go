# F4 — Recetas, costo, precio sugerido y disponibilidad: diagnóstico y propuesta

Fecha: 25-nov-2026 · Estado: **PROPUESTA; no se modificó código.** Espera autorización expresa.
Marco: `PROPUESTA_INSUMOS_RECETAS_TUKICHEF.md` §6, §7 y §10 (D13: food cost sobre precio neto; redondeo al alza a S/ 0.10).

## 1. Qué verá el usuario
- Pantalla **Recetas**: los platos con su costo por porción, food cost, margen, precio sugerido y **para cuántas porciones alcanza**; se
  marcan los que superan el objetivo y los que no tienen receta.
- Editor de receta: ingredientes (insumos), cantidad servida, % de merma y porciones que rinde. Se puede escribir en **g o ml** aunque el
  insumo se guarde en kg o litros.
- **Precio sugerido** por food cost objetivo o por margen, con redondeo. Botón «Aplicar precio» con confirmación y registro de quién lo
  cambió. **Nunca se cambia solo.**

**No se toca la venta:** nada se descuenta al cobrar (eso es F5).

## 2. Cómo se calcula
| Dato | Fórmula |
|---|---|
| Cantidad bruta | cantidad servida ÷ (1 − merma) |
| Costo de línea | cantidad bruta × costo del insumo (**promedio de la sucursal**; si no hay, último costo) |
| Costo por porción | suma de líneas ÷ porciones que rinde |
| Precio neto | precio publicado ÷ (1 + IGV) si el plato es gravado y lo incluye; en Nuevo RUS, el precio tal cual |
| Food cost | costo por porción ÷ precio neto |
| Precio sugerido | costo ÷ food cost objetivo (o costo ÷ (1 − margen)), más IGV, redondeado |
| Disponibilidad | el menor, entre los ingredientes, de stock ÷ cantidad bruta por porción (hacia abajo) |

Ejemplo ya aprobado: costo 4.15, objetivo 30 % → 13.83 neto → 16.32 con IGV → **16.40** (food cost real 29.86 %).
Si algún ingrediente no tiene costo, la receta se marca **incompleta**: no se muestra food cost ni precio sugerido.

## 3. Cambios
### 3.1 Base de datos — V198 (aditiva)
`tenant_recipes` (plato, presentación, porciones, versión, activa, objetivo propio), `tenant_recipe_lines` (insumo, cantidad neta, unidad
mostrada, merma), `tenant_price_changes` (usuario, fecha, plato, sucursal, antes, después, motivo) y en la configuración de restaurante:
food cost objetivo (30 %), redondeo (0.10, al alza) y base de costo (promedio).

### 3.2 Backend
Rutas `/api/restaurant/recipes` (listar con costos y disponibilidad, ver, guardar, borrar), configuración de precios y «aplicar precio».
Costos calculados al consultar, con aritmética decimal. Un insumo usado en una receta no se puede borrar.

### 3.3 Permisos
| Permiso | Qué permite | Por defecto |
|---|---|---|
| `recipes.view` | ver recetas **sin costos** | administrador, supervisor, cocinero |
| `recipes.manage` | crear y editar recetas | administrador, supervisor |
| `recipes.view_cost` (ya existe) | ver costos, food cost y precio sugerido | administrador, supervisor |
| aplicar precio | `recipes.manage` + `recipes.view_cost` + gestionar productos | administrador, supervisor |

### 3.4 Tukichef
Pantalla **Recetas** aparte (no una pestaña dentro de `ProductosPage`, que ya tiene 1854 líneas) y acceso desde cada plato en Productos.

## 4. Alcance de los casos especiales
| Caso | En F4 |
|---|---|
| Plato con presentaciones (tamaños) | ver decisión 1 |
| Combo | costo y disponibilidad = suma y mínimo de sus componentes **por defecto**; si uno no tiene receta, incompleto |
| Extras y modificadores | no se costean (hoy no están ligados a un insumo) |
| Preparaciones intermedias (salsas) | V2 |

## 5. Entrega
**F4a** backend (V198, cálculo, rutas, permisos, auditoría de precio) y **F4b** pantallas y recorrido en navegador.

## 6. Pruebas
Fórmulas con el ejemplo aprobado y la hamburguesa (2000 g / 150 g = 13); merma y rendimiento; kg↔g y L↔ml; costo de 0.0035 por gramo;
cada redondeo; exonerado, Nuevo RUS y precio por sucursal; ingrediente sin costo; combos; permisos (cocinero sin costos); el precio **no
cambia** sin aplicar y cada cambio queda registrado; ventas idénticas a hoy. SQLite y MariaDB estricto.

## 7. Riesgos
- «Aplicar precio» cambia lo que se cobra: pide confirmación y dice **dónde** se aplica. Si la sucursal no tiene precio propio, se cambia
  el precio general, que afecta a todas.
- Los costos dependen de que las compras estén bien cargadas; un insumo mal registrado (unidad equivocada) distorsiona todo. La pantalla
  muestra el costo de cada línea para detectarlo.
- El stock se guarda con 3 decimales: en un insumo medido en kg, la resolución es 1 g. Importará en F5.
- Sigue sin respaldo: todo este trabajo existe solo en esta PC.

## 8. Decisiones que necesito
1. **Platos con presentaciones** (personal, familiar…): la v3.1 lo dejaba para V2. Recomiendo **adelantarlo** y permitir una receta por
   presentación: sin eso, el costo de una pizza familiar sería el de la personal y el precio sugerido saldría mal. Es poco trabajo extra
   porque la tabla ya lo contempla.
2. **Aplicar precio:** si la sucursal tiene precio propio, cambiar ese; si no, el general, avisando. Recomendado: sí.
3. **El cocinero ve las recetas sin costos.** Recomendado: sí.
4. **Disponibilidad en el POS:** dejarla para F5 y mostrarla ahora solo en Recetas. Recomendado: sí.
5. **Escribir cantidades en g y ml** aunque el insumo esté en kg o litros. Recomendado: sí.
6. **F4a y F4b por separado.** Recomendado: sí.

## 9. No verificado
Solo se leyó código y la propuesta anterior. No se revisó cuántos de tus clientes usan presentaciones o combos en sus platos.
