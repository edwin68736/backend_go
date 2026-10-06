package tenantmigrations

import (
	"fmt"

	"gorm.io/gorm"
)

type v157PurchaseItem struct {
	ID             uint  `gorm:"primaryKey"`
	PresentationID *uint `gorm:"column:presentation_id;index"`
}

func (v157PurchaseItem) TableName() string { return "tenant_purchase_items" }

// V157PurchaseItemPresentation agrega tenant_purchase_items.presentation_id: la presentación o
// variante comprada en cada línea (ej. "Talla M"), para ingresar el stock a esa presentación.
// Columna nullable sin backfill: las compras existentes quedan igual. Idempotente.
type V157PurchaseItemPresentation struct{}

func (V157PurchaseItemPresentation) Version() int { return 157 }
func (V157PurchaseItemPresentation) Name() string { return "purchase_item_presentation" }

func (V157PurchaseItemPresentation) Up(db *gorm.DB) error {
	mig := db.Migrator()
	if !mig.HasTable(&v157PurchaseItem{}) || mig.HasColumn(&v157PurchaseItem{}, "PresentationID") {
		return nil
	}
	if err := mig.AddColumn(&v157PurchaseItem{}, "PresentationID"); err != nil {
		return fmt.Errorf("add tenant_purchase_items.presentation_id: %w", err)
	}
	return nil
}
