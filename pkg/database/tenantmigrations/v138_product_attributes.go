package tenantmigrations

import (
	"fmt"

	"tukifac/pkg/database"

	"gorm.io/gorm"
)

// V138ProductAttributes crea tenant_product_attributes: características descriptivas del
// producto (ej. "Color: Rojo") — Fase 5. Tabla nueva y vacía, no toca TenantProduct,
// TenantProductSaleUnit, TenantProductSaleUnitBranchPrice, TenantProductPresentation,
// TenantProductStock ni TenantStockMovement. Sin backfill: ningún producto existente recibe
// atributos automáticamente.
type V138ProductAttributes struct{}

func (V138ProductAttributes) Version() int { return 138 }
func (V138ProductAttributes) Name() string { return "product_attributes" }

func (V138ProductAttributes) Up(db *gorm.DB) error {
	if err := db.AutoMigrate(&database.TenantProductAttribute{}); err != nil {
		return fmt.Errorf("crear tenant_product_attributes: %w", err)
	}
	return nil
}
