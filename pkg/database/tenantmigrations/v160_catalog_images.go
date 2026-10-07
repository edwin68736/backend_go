package tenantmigrations

import (
	"fmt"
	"time"

	"gorm.io/gorm"
)

type v160Category struct {
	ID       uint   `gorm:"primaryKey"`
	ImageURL string `gorm:"column:image_url;size:255"`
}

func (v160Category) TableName() string { return "tenant_categories" }

type v160ProductImage struct {
	ID        uint   `gorm:"primaryKey"`
	ProductID uint   `gorm:"not null;index"`
	URL       string `gorm:"size:255;not null"`
	SortOrder int    `gorm:"default:0"`
	CreatedAt time.Time
}

func (v160ProductImage) TableName() string { return "tenant_product_images" }

// V160CatalogImages agrega la imagen de cada categoría (tenant_categories.image_url, para mostrarla en la
// tienda virtual) y la galería de imágenes adicionales de los productos (tenant_product_images). Sin
// backfill; idempotente.
type V160CatalogImages struct{}

func (V160CatalogImages) Version() int { return 160 }
func (V160CatalogImages) Name() string { return "catalog_images" }

func (V160CatalogImages) Up(db *gorm.DB) error {
	mig := db.Migrator()
	if mig.HasTable(&v160Category{}) && !mig.HasColumn(&v160Category{}, "ImageURL") {
		if err := mig.AddColumn(&v160Category{}, "ImageURL"); err != nil {
			return fmt.Errorf("add tenant_categories.image_url: %w", err)
		}
	}
	if !mig.HasTable(&v160ProductImage{}) {
		if err := mig.CreateTable(&v160ProductImage{}); err != nil {
			return fmt.Errorf("create tenant_product_images: %w", err)
		}
	}
	return nil
}
