package tenantmigrations

import (
	"fmt"

	"gorm.io/gorm"
)

type v142Branch struct {
	ID      uint   `gorm:"primaryKey"`
	LogoURL string `gorm:"column:logo_url;type:longtext"`
}

func (v142Branch) TableName() string { return "tenant_branches" }

// V142BranchLogo agrega logo_url a tenant_branches: logo propio opcional por sucursal. Vacío =
// se sigue usando el logo global de TenantCompanyConfig (ver pkg/branchlogo.ResolveURL) — mismo
// patrón override+fallback que ya usa TenantProductSaleUnitBranchPrice para precios por sucursal.
//
// Puramente aditiva: columna nullable/vacía, ningún tenant pierde el comportamiento actual
// (logo único global) hasta que configure explícitamente un logo por sucursal.
type V142BranchLogo struct{}

func (V142BranchLogo) Version() int { return 142 }
func (V142BranchLogo) Name() string { return "branch_logo" }

func (V142BranchLogo) Up(db *gorm.DB) error {
	mig := db.Migrator()
	if !mig.HasTable(&v142Branch{}) {
		return nil
	}
	if !mig.HasColumn(&v142Branch{}, "LogoURL") {
		if err := mig.AddColumn(&v142Branch{}, "LogoURL"); err != nil {
			return fmt.Errorf("add tenant_branches.logo_url: %w", err)
		}
	}
	return nil
}
