package tenantmigrations

import (
	"fmt"

	"gorm.io/gorm"
)

// V141ProductNameWiden amplía tenant_products.name de varchar(255) a varchar(500) — el límite
// de 255 bloqueaba nombres legítimos más largos en la importación masiva de productos (reportado
// por el usuario). No tiene índice propio, así que ampliarlo no afecta longitud de clave.
type V141ProductNameWiden struct{}

func (V141ProductNameWiden) Version() int { return 141 }
func (V141ProductNameWiden) Name() string { return "product_name_widen" }

func (V141ProductNameWiden) Up(db *gorm.DB) error {
	if !db.Migrator().HasTable("tenant_products") || !db.Migrator().HasColumn("tenant_products", "name") {
		return nil
	}
	if err := db.Exec("ALTER TABLE tenant_products MODIFY COLUMN name VARCHAR(500) NOT NULL").Error; err != nil {
		return fmt.Errorf("tenant_products.name: %w", err)
	}
	return nil
}
