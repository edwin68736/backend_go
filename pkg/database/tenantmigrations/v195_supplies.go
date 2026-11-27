package tenantmigrations

import (
	"fmt"

	"gorm.io/gorm"
)

type v195Product struct {
	IsSupply   bool   `gorm:"column:is_supply;not null;default:false;index"`
	SupplyKind string `gorm:"column:supply_kind;size:12;not null;default:'raw'"`
}

func (v195Product) TableName() string { return "tenant_products" }

type v195RestaurantSetting struct {
	RecipesEnabled bool `gorm:"column:recipes_enabled;not null;default:false"`
}

func (v195RestaurantSetting) TableName() string { return "tenant_restaurant_settings" }

// V195Supplies (F1) agrega la marca de insumo a los productos y el interruptor del módulo en la configuración de restaurante.
// Solo columnas con valor por defecto: todo producto existente queda como NO insumo y el módulo apagado. Idempotente.
type V195Supplies struct{}

func (V195Supplies) Version() int { return 195 }
func (V195Supplies) Name() string { return "supplies" }

func (V195Supplies) Up(db *gorm.DB) error {
	mig := db.Migrator()
	if mig.HasTable("tenant_products") {
		for _, col := range []string{"IsSupply", "SupplyKind"} {
			if !mig.HasColumn(&v195Product{}, col) {
				if err := mig.AddColumn(&v195Product{}, col); err != nil {
					return fmt.Errorf("add tenant_products.%s: %w", col, err)
				}
			}
		}
		if !mig.HasIndex(&v195Product{}, "idx_tenant_products_is_supply") {
			if err := mig.CreateIndex(&v195Product{}, "IsSupply"); err != nil {
				return fmt.Errorf("index tenant_products.is_supply: %w", err)
			}
		}
	}
	if mig.HasTable("tenant_restaurant_settings") && !mig.HasColumn(&v195RestaurantSetting{}, "RecipesEnabled") {
		if err := mig.AddColumn(&v195RestaurantSetting{}, "RecipesEnabled"); err != nil {
			return fmt.Errorf("add tenant_restaurant_settings.recipes_enabled: %w", err)
		}
	}
	return nil
}
