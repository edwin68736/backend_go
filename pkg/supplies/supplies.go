// Package supplies concentra las reglas de los INSUMOS (F1): filas de tenant_products con is_supply=true que se compran y se mueven
// en inventario, pero nunca se venden ni aparecen en el POS, la carta o la tienda. Ver docs/recetas/F1_PROPUESTA.md.
package supplies

import (
	"errors"
	"fmt"
	"strings"

	"tukifac/pkg/database"

	"gorm.io/gorm"
)

const columnProbe = "col:tenant_products.is_supply"

// Mode: cómo se tratan los insumos en un listado de productos.
const (
	ModeExclude = ""        // por defecto: los insumos no salen (venta, carta, tienda)
	ModeOnly    = "only"    // solo insumos
	ModeInclude = "include" // productos e insumos (compras, inventario)
)

// ErrNotSellable: se intentó vender, cotizar, pedir o combinar un insumo.
var ErrNotSellable = errors.New("es un insumo: no se puede vender ni agregar a la carta")

// ParseMode normaliza el parámetro `supplies` de la API.
func ParseMode(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case ModeOnly:
		return ModeOnly
	case ModeInclude, "all":
		return ModeInclude
	}
	return ModeExclude
}

// Ready: true si la BD del tenant tiene tenant_products.is_supply (V195). Sin la columna no hay insumos y todo se comporta como
// antes, en vez de romper listados, cupo o ventas.
func Ready(db *gorm.DB) bool {
	return database.CachedSchemaProbe(db, columnProbe, func() bool {
		return db.Migrator().HasColumn(&database.TenantProduct{}, "is_supply")
	})
}

// NotSupplySQL es la condición "esta fila no es un insumo" para una tabla o alias de productos (NULL cuenta como producto).
func NotSupplySQL(alias string) string {
	return "COALESCE(" + alias + ".is_supply, 0) = 0"
}

// Exclude restringe una consulta sobre tenant_products a lo que no es insumo.
func Exclude(db *gorm.DB) func(*gorm.DB) *gorm.DB {
	return func(q *gorm.DB) *gorm.DB {
		if !Ready(db) {
			return q
		}
		return q.Where(NotSupplySQL("tenant_products"))
	}
}

// Reject devuelve error si alguno de los productos es un insumo. Es la barrera del SERVIDOR: el filtro de los listados no basta,
// porque un cliente puede enviar el id directamente.
func Reject(db *gorm.DB, productIDs ...uint) error {
	ids := make([]uint, 0, len(productIDs))
	for _, id := range productIDs {
		if id > 0 {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 || !Ready(db) {
		return nil
	}
	var names []string
	if err := db.Unscoped().Model(&database.TenantProduct{}).Where("id IN ? AND is_supply = ?", ids, true).Limit(1).
		Pluck("name", &names).Error; err != nil {
		return fmt.Errorf("verificar insumos: %w", err)
	}
	if len(names) > 0 {
		return fmt.Errorf("«%s» %w", names[0], ErrNotSellable)
	}
	return nil
}

// RejectPtr: igual que Reject para líneas con product_id opcional.
func RejectPtr(db *gorm.DB, productIDs ...*uint) error {
	ids := make([]uint, 0, len(productIDs))
	for _, p := range productIDs {
		if p != nil {
			ids = append(ids, *p)
		}
	}
	return Reject(db, ids...)
}

// KindRaw: materia prima. "prepared" queda reservado para producción (V2).
const KindRaw = "raw"

// Mark deja una fila de producto marcada como insumo. Es la ÚNICA escritura de is_supply (ver TenantProduct.IsSupply): escribe por
// nombre de tabla porque el campo es de solo lectura para GORM.
func Mark(db *gorm.DB, productID uint) error {
	return db.Table("tenant_products").Where("id = ?", productID).
		Updates(map[string]interface{}{"is_supply": true, "supply_kind": KindRaw}).Error
}

// IsSupply indica si el producto es un insumo.
func IsSupply(db *gorm.DB, productID uint) bool {
	return errors.Is(Reject(db, productID), ErrNotSellable)
}
