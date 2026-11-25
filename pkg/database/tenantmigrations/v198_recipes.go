package tenantmigrations

import (
	"fmt"

	"tukifac/pkg/database"

	"gorm.io/gorm"
)

type v198Setting struct {
	TargetFoodCostPct float64 `gorm:"column:target_food_cost_pct;type:decimal(5,2);not null;default:30"`
	PriceRounding     string  `gorm:"column:price_rounding;size:6;not null;default:'0.10'"`
	RoundingDirection string  `gorm:"column:rounding_direction;size:8;not null;default:'up'"`
	RecipeCostBasis   string  `gorm:"column:recipe_cost_basis;size:8;not null;default:'avg'"`
}

func (v198Setting) TableName() string { return "tenant_restaurant_settings" }

// V198Recipes (F4a) crea las tablas de recetas y del historial de precios, y la configuración de precio sugerido. Aditiva e idempotente.
type V198Recipes struct{}

func (V198Recipes) Version() int { return 198 }
func (V198Recipes) Name() string { return "recipes" }

func (V198Recipes) Up(db *gorm.DB) error {
	mig := db.Migrator()
	for _, m := range []interface{}{&database.TenantRecipe{}, &database.TenantRecipeLine{}, &database.TenantPriceChange{}} {
		if !mig.HasTable(m) {
			if err := mig.CreateTable(m); err != nil {
				return fmt.Errorf("crear tabla de recetas: %w", err)
			}
		}
	}
	if mig.HasTable("tenant_restaurant_settings") {
		for _, f := range []string{"TargetFoodCostPct", "PriceRounding", "RoundingDirection", "RecipeCostBasis"} {
			if !mig.HasColumn(&v198Setting{}, f) {
				if err := mig.AddColumn(&v198Setting{}, f); err != nil {
					return fmt.Errorf("add tenant_restaurant_settings.%s: %w", f, err)
				}
			}
		}
	}
	return nil
}
