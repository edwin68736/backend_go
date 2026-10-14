package tenantmigrations

import (
	"fmt"

	"gorm.io/gorm"
)

type v161EcommerceSettings struct {
	ID        uint   `gorm:"primaryKey"`
	HeroStyle string `gorm:"column:hero_style;size:20;default:'split'"`
}

func (v161EcommerceSettings) TableName() string { return "tenant_ecommerce_settings" }

// V161EcommerceHeroStyle agrega hero_style ('split' | 'image') para que el tenant elija si el banner
// principal de la tienda muestra texto + botón junto a la imagen, o solo la imagen completa. Las filas
// existentes quedan en 'split' (el diseño de siempre). Idempotente.
type V161EcommerceHeroStyle struct{}

func (V161EcommerceHeroStyle) Version() int { return 161 }
func (V161EcommerceHeroStyle) Name() string { return "ecommerce_hero_style" }

func (V161EcommerceHeroStyle) Up(db *gorm.DB) error {
	mig := db.Migrator()
	if !mig.HasTable(&v161EcommerceSettings{}) {
		return nil
	}
	if mig.HasColumn(&v161EcommerceSettings{}, "HeroStyle") {
		return nil
	}
	if err := mig.AddColumn(&v161EcommerceSettings{}, "HeroStyle"); err != nil {
		return fmt.Errorf("add tenant_ecommerce_settings.hero_style: %w", err)
	}
	return nil
}
