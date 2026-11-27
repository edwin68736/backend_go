package tenantmigrations

import (
	"fmt"

	"tukifac/pkg/database"

	"gorm.io/gorm"
)

type v199Setting struct {
	RecipesConsume     bool   `gorm:"column:recipes_consume;not null;default:false"`
	RecipesStockPolicy string `gorm:"column:recipes_stock_policy;size:8;not null;default:'allow'"`
}

func (v199Setting) TableName() string { return "tenant_restaurant_settings" }

type v199SaleItem struct {
	RecipeCost      *float64 `gorm:"column:recipe_cost;type:decimal(15,6)"`
	RecipeCostTotal *float64 `gorm:"column:recipe_cost_total;type:decimal(15,6)"`
}

func (v199SaleItem) TableName() string { return "tenant_sale_items" }

// V199RecipeConsumption (F5a) prepara el descuento de insumos al vender: tipos de movimiento, interruptor (apagado), costo de receta en
// la línea de venta y registro de consumos y reversas. Aditiva e idempotente. La tabla de consumos se crea AL FINAL: el código la usa
// como señal de que todo lo demás ya está.
type V199RecipeConsumption struct{}

func (V199RecipeConsumption) Version() int { return 199 }
func (V199RecipeConsumption) Name() string { return "recipe_consumption" }

func (V199RecipeConsumption) Up(db *gorm.DB) error {
	mig := db.Migrator()
	if mig.HasTable("tenant_inventory_operation_types") {
		if err := database.SeedInventoryOperationTypes(db); err != nil {
			return fmt.Errorf("tipos de movimiento de receta: %w", err)
		}
	}
	add := func(table string, model interface{}, fields ...string) error {
		if !mig.HasTable(table) {
			return nil
		}
		for _, f := range fields {
			if !mig.HasColumn(model, f) {
				if err := mig.AddColumn(model, f); err != nil {
					return fmt.Errorf("add %s.%s: %w", table, f, err)
				}
			}
		}
		return nil
	}
	if err := add("tenant_restaurant_settings", &v199Setting{}, "RecipesConsume", "RecipesStockPolicy"); err != nil {
		return err
	}
	if err := add("tenant_sale_items", &v199SaleItem{}, "RecipeCost", "RecipeCostTotal"); err != nil {
		return err
	}
	for _, m := range []interface{}{&database.TenantRecipeConsumptionReversal{}, &database.TenantRecipeConsumption{}} {
		if !mig.HasTable(m) {
			if err := mig.CreateTable(m); err != nil {
				return fmt.Errorf("crear tabla de consumos de receta: %w", err)
			}
		}
	}
	return nil
}
