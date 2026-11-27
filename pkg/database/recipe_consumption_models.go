package database

import "time"

// Consumo de insumos al vender (V199, F5a). Es el registro del que sale toda reversa: nunca se vuelve a mirar la receta.

// TenantRecipeConsumption: lo que una línea de venta consumió de un insumo, por la receta de un plato (SourceProductID: el de la línea o
// el componente de un combo). Quantity en la unidad base del insumo; MovementID 0 = la cantidad redondeó a cero.
type TenantRecipeConsumption struct {
	ID                   uint      `gorm:"primaryKey" json:"id"`
	SaleID               uint      `gorm:"not null;index" json:"sale_id"`
	SaleItemID           uint      `gorm:"not null;uniqueIndex:uk_recipe_consumption,priority:1" json:"sale_item_id"`
	SourceProductID      uint      `gorm:"not null;uniqueIndex:uk_recipe_consumption,priority:2" json:"source_product_id"`
	SourcePresentationID uint      `gorm:"not null;default:0;uniqueIndex:uk_recipe_consumption,priority:3" json:"source_presentation_id"`
	SupplyID             uint      `gorm:"not null;uniqueIndex:uk_recipe_consumption,priority:4;index" json:"supply_id"`
	BranchID             uint      `gorm:"not null" json:"branch_id"`
	RecipeID             uint      `gorm:"not null" json:"recipe_id"`
	RecipeVersion        uint      `gorm:"not null" json:"recipe_version"`
	Quantity             float64   `gorm:"type:decimal(15,3);not null" json:"quantity"`
	UnitCost             *float64  `gorm:"type:decimal(15,6)" json:"unit_cost"` // vacío = costo desconocido
	MovementID           uint      `gorm:"not null;default:0" json:"movement_id"`
	ReversedQty          float64   `gorm:"type:decimal(15,3);not null;default:0" json:"reversed_qty"`   // insumo ya repuesto
	ReturnedUnits        float64   `gorm:"type:decimal(15,3);not null;default:0" json:"returned_units"` // unidades de la línea ya devueltas
	CreatedAt            time.Time `json:"created_at"`
}

// TenantRecipeConsumptionReversal: cada reposición, única por consumo y referencia (anulación o nota de crédito).
type TenantRecipeConsumptionReversal struct {
	ID            uint      `gorm:"primaryKey" json:"id"`
	ConsumptionID uint      `gorm:"not null;uniqueIndex:uk_recipe_reversal,priority:1" json:"consumption_id"`
	Reference     string    `gorm:"size:100;not null;uniqueIndex:uk_recipe_reversal,priority:2" json:"reference"`
	Quantity      float64   `gorm:"type:decimal(15,3);not null" json:"quantity"`
	MovementID    uint      `gorm:"not null;default:0" json:"movement_id"`
	CreatedAt     time.Time `json:"created_at"`
}
