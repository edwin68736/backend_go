# F1b — Pantallas de insumos: informe

Fecha: 25-nov-2026 · Estado: **implementado y probado en local; sin push, sin deploy, sin tocar producción.**
Alcance: `F1b_PROPUESTA.md` con las 4 recomendaciones. F2 no se empezó.

## 1. Qué quedó
| Repositorio | Cambio |
|---|---|
| backend | `GET /api/superadmin/tenants/:id/restaurant-recipes` → `{enabled, available}` |
| panel central | interruptor «Insumos y recetas» en la ventana de módulos de cada empresa |
| Tukichef (1.6.0) | menú y pantalla **Insumos** (lista, alta/edición, unidades de compra, movimientos, stock bajo, costo con permiso); reporte de stock con insumos rotulados |
| Tukifac | insumos rotulados en selector de compra/orden/documentos de inventario, traslados e inventario; sin «Actualizar precio de venta» |

## 2. Pruebas
- `tsc`, build y pruebas: Tukichef 128/128, Tukifac 176/176, central 137/137. Backend: paquetes de superadmin en verde.
- Pruebas nuevas: acceso al menú y armado de la petición de insumos (`utils/supplies.test.ts`); ruta nueva en el test de permisos.
- Servicio del superadmin contra la BD local: empresa sin migrar → no disponible; `demo` migrada → apagada → activada.

### Recorrido en navegador (local, empresa `demo`)
1. Crear «Arroz QA F1b» (KGM, mínimo 5, costo «3,5») y la unidad «Saco 50 kg».
2. Comprarlo en Tukifac: 1 saco a S/ 150 → stock 50 KGM, costo S/ 3.00 por KGM, movimiento `COMPRA/F999-1001`.
3. No aparece en el POS de Tukichef ni en el listado de venta de Tukifac; el lector de códigos responde «no encontrado».
4. Inventario de Tukifac y reporte de stock de Tukichef lo muestran rotulado.
5. Edición a 375×620: la ventana se desplaza, los botones quedan fijos y la unidad base está bloqueada.
6. Módulo apagado: sin menú y `/insumos` redirige a Inicio.

## 3. No probado
- **El interruptor del panel central en pantalla** (no se inició sesión como superadmin). Sí compila y su backend se probó.
- Traslados y órdenes de compra de Tukifac con un insumo, desde pantalla (el servidor sí está cubierto en F1a).
- Usuario sin permiso de gestión o de costos, desde pantalla (cubierto por pruebas de rutas y unitarias).
- Apps de Android o escritorio compiladas.
- MySQL 8 y producción.

## 4. Observaciones
- En la compra, al elegir una unidad (saco), el costo sugerido sigue siendo el de la unidad base (S/ 3.50 y no S/ 175). Es el
  comportamiento que ya tiene cualquier producto con unidades; hay que escribir el costo del saco. No se cambió.
- Quien active el módulo debe volver a abrir Tukichef.
- Tukifac muestra los insumos en compras e inventario aunque el módulo se apague después: los datos siguen existiendo.
- Para probar Tukichef en local con una empresa por subdominio, el proxy de Vite no resuelve `*.localhost` en Windows y el modo de
  desarrollo envía la empresa como `dev`.

## 5. Datos que quedaron en la BD local
Empresa `demo`: migraciones V193–V195 aplicadas, módulo activado, el insumo «Arroz QA F1b», su unidad y la compra `F999-1001`.
El usuario de prueba `qa.f1b@example.test` quedó desactivado.

## 6. Despliegue (no autorizado)
1. F0a + F1a + F1b backend, con `migrate-fleet` (V193–V195).
2. Panel central, Tukifac, Tukichef web; luego compilar apps 1.6.0.
3. Activar el módulo en una empresa de prueba antes que en clientes.
