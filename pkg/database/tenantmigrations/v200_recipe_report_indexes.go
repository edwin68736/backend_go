package tenantmigrations

import (
	"fmt"

	"tukifac/pkg/database"

	"gorm.io/gorm"
)

// V200RecipeReportIndexes (F6a): el reporte de consumo filtra por sucursal y fecha. Las tablas nacen vacías con el módulo, así que
// crear los índices no bloquea datos existentes. Aditiva e idempotente.
type V200RecipeReportIndexes struct{}

func (V200RecipeReportIndexes) Version() int { return 200 }
func (V200RecipeReportIndexes) Name() string { return "recipe_report_indexes" }

func (V200RecipeReportIndexes) Up(db *gorm.DB) error {
	mig := db.Migrator()
	add := func(model interface{}, table, name, columns string) error {
		if !mig.HasTable(model) || mig.HasIndex(model, name) {
			return nil
		}
		if err := db.Exec(fmt.Sprintf("CREATE INDEX %s ON %s (%s)", name, table, columns)).Error; err != nil {
			return fmt.Errorf("crear índice %s: %w", name, err)
		}
		return nil
	}
	if err := add(&database.TenantRecipeConsumption{}, "tenant_recipe_consumptions", "idx_recipe_consumption_branch_date",
		"branch_id, created_at"); err != nil {
		return err
	}
	return add(&database.TenantRecipeConsumptionReversal{}, "tenant_recipe_consumption_reversals", "idx_recipe_reversal_date",
		"created_at")
}
