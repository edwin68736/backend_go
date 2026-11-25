package tenantmigrations

import (
	"fmt"

	"gorm.io/gorm"
)

// V194InventoryDocumentUnitCost amplía tenant_inventory_document_details.unit_cost de DECIMAL(15,2) a DECIMAL(15,6), conservando
// que admite NULL. Con (15,2) un costo de S/ 0.0042 por gramo se guardaba como 0.00 antes de llegar al kardex, que ya es (15,6).
// Mismo problema y solución que V136/V169/V170. Idempotente: si la columna ya tiene 6 decimales no repite el ALTER (que en
// MySQL puede copiar la tabla).
type V194InventoryDocumentUnitCost struct{}

func (V194InventoryDocumentUnitCost) Version() int { return 194 }
func (V194InventoryDocumentUnitCost) Name() string { return "inventory_document_unit_cost" }

func (V194InventoryDocumentUnitCost) Up(db *gorm.DB) error {
	if db.Dialector.Name() != "mysql" {
		return nil
	}
	var scale []int
	if err := db.Raw(`SELECT NUMERIC_SCALE FROM information_schema.columns
		WHERE table_schema = DATABASE() AND table_name = 'tenant_inventory_document_details' AND column_name = 'unit_cost'`).
		Scan(&scale).Error; err != nil {
		return fmt.Errorf("leer tenant_inventory_document_details.unit_cost: %w", err)
	}
	if len(scale) == 0 || scale[0] >= 6 {
		return nil // sin tabla/columna, o ya ampliada
	}
	if err := db.Exec("ALTER TABLE tenant_inventory_document_details MODIFY COLUMN unit_cost DECIMAL(15,6) NULL").Error; err != nil {
		return fmt.Errorf("tenant_inventory_document_details.unit_cost: %w", err)
	}
	return nil
}
