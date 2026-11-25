package database

import "time"

// Recetas (V198, F4a). Son catálogo: no llevan sucursal; el costo y la disponibilidad se calculan con los datos de cada sucursal.

// TenantRecipe receta de un plato. PresentationID 0 = receta del plato (la usan las presentaciones que no tengan una propia).
type TenantRecipe struct {
	ID                uint      `gorm:"primaryKey" json:"id"`
	ProductID         uint      `gorm:"not null;uniqueIndex:uk_recipe_product_presentation" json:"product_id"`
	PresentationID    uint      `gorm:"not null;default:0;uniqueIndex:uk_recipe_product_presentation" json:"presentation_id"`
	YieldQty          float64   `gorm:"type:decimal(15,3);not null;default:1" json:"yield_qty"` // porciones que rinde
	TargetFoodCostPct *float64  `gorm:"type:decimal(5,2)" json:"target_food_cost_pct"`          // vacío = el de la empresa
	Version           uint      `gorm:"not null;default:1" json:"version"`
	UpdatedBy         uint      `json:"updated_by"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

// TenantRecipeLine ingrediente. QtyNet va en la unidad base del insumo; DisplayUnit es cómo la escribió el usuario (g, ml…).
type TenantRecipeLine struct {
	ID                 uint    `gorm:"primaryKey" json:"id"`
	RecipeID           uint    `gorm:"not null;index" json:"recipe_id"`
	ComponentProductID uint    `gorm:"not null;index" json:"component_product_id"`
	QtyNet             float64 `gorm:"type:decimal(15,6);not null" json:"qty_net"`
	DisplayUnit        string  `gorm:"size:6" json:"display_unit"`
	WastePct           float64 `gorm:"type:decimal(5,2);not null;default:0" json:"waste_pct"`
	SortOrder          int     `gorm:"default:0" json:"sort_order"`
}

// TenantPriceChange historial de precios aplicados desde recetas. BranchID 0 = precio general.
type TenantPriceChange struct {
	ID             uint      `gorm:"primaryKey" json:"id"`
	ProductID      uint      `gorm:"not null;index" json:"product_id"`
	PresentationID uint      `gorm:"not null;default:0" json:"presentation_id"`
	BranchID       uint      `gorm:"not null;default:0" json:"branch_id"`
	PriceBefore    float64   `gorm:"type:decimal(15,2);not null" json:"price_before"`
	PriceAfter     float64   `gorm:"type:decimal(15,2);not null" json:"price_after"`
	Reason         string    `gorm:"size:100" json:"reason"`
	UserID         uint      `gorm:"not null" json:"user_id"`
	CreatedAt      time.Time `json:"created_at"`
}
