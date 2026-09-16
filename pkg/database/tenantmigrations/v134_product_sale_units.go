package tenantmigrations

import (
	"fmt"

	"tukifac/pkg/database"

	"gorm.io/gorm"
)

// V134ProductSaleUnits crea tenant_product_sale_units: unidades comerciales de venta de un
// producto (ej. "Saco 100 KG" con factor 100 sobre un producto en KG), independientes de
// TenantProductPresentation (variante con stock propio, sin tocar). La tabla nace vacía — no hay
// backfill: los productos existentes siguen usando TenantProduct.Unit/SalePrice hasta que se cree
// explícitamente una unidad de venta. Todavía no está conectada a ventas/compras/Kardex/precios
// por sucursal (fases posteriores).
type V134ProductSaleUnits struct{}

func (V134ProductSaleUnits) Version() int { return 134 }
func (V134ProductSaleUnits) Name() string { return "product_sale_units" }

func (V134ProductSaleUnits) Up(db *gorm.DB) error {
	if err := db.AutoMigrate(&database.TenantProductSaleUnit{}); err != nil {
		return fmt.Errorf("crear tenant_product_sale_units: %w", err)
	}
	return nil
}
