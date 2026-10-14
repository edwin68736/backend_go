package tenantmigrations

import (
	"fmt"
	"time"

	"gorm.io/gorm"
)

type v162ProductBranch struct {
	ID        uint `gorm:"primaryKey"`
	ProductID uint `gorm:"not null;uniqueIndex:idx_product_branch"`
	BranchID  uint `gorm:"not null;uniqueIndex:idx_product_branch;index"`
	CreatedAt time.Time
}

func (v162ProductBranch) TableName() string { return "tenant_product_branches" }

// V162ProductBranches crea tenant_product_branches: en qué sucursales se vende cada producto. Sin
// filas = todas las sucursales (comportamiento previo), así que no hace falta backfill. Solo
// aditiva; idempotente.
type V162ProductBranches struct{}

func (V162ProductBranches) Version() int { return 162 }
func (V162ProductBranches) Name() string { return "product_branches" }

func (V162ProductBranches) Up(db *gorm.DB) error {
	if db.Migrator().HasTable(&v162ProductBranch{}) {
		return nil
	}
	if err := db.Migrator().CreateTable(&v162ProductBranch{}); err != nil {
		return fmt.Errorf("create tenant_product_branches: %w", err)
	}
	return nil
}
