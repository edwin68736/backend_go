package tenantmigrations

import (
	"fmt"

	"gorm.io/gorm"
)

type v057Branch struct {
	FiscalDomicileCode string `gorm:"column:fiscal_domicile_code;size:20"`
}

func (v057Branch) TableName() string { return "tenant_branches" }

// V057BranchFiscalDomicile agrega código de domicilio fiscal por sucursal.
type V057BranchFiscalDomicile struct{}

func (V057BranchFiscalDomicile) Version() int { return 57 }
func (V057BranchFiscalDomicile) Name() string { return "branch_fiscal_domicile_code" }

func (V057BranchFiscalDomicile) Up(db *gorm.DB) error {
	mig := db.Migrator()
	b := &v057Branch{}
	if !mig.HasTable(b) {
		return nil
	}
	if !mig.HasColumn(b, "FiscalDomicileCode") {
		if err := mig.AddColumn(b, "FiscalDomicileCode"); err != nil {
			return fmt.Errorf("add tenant_branches.fiscal_domicile_code: %w", err)
		}
	}
	return nil
}
