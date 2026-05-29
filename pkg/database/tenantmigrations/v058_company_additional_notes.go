package tenantmigrations

import (
	"fmt"

	"gorm.io/gorm"
)

type v058CompanyConfig struct {
	AdditionalNotes string `gorm:"column:additional_notes;type:text"`
}

func (v058CompanyConfig) TableName() string { return "tenant_company_configs" }

// V058CompanyAdditionalNotes notas/información adicional de la empresa.
type V058CompanyAdditionalNotes struct{}

func (V058CompanyAdditionalNotes) Version() int { return 58 }
func (V058CompanyAdditionalNotes) Name() string { return "company_additional_notes" }

func (V058CompanyAdditionalNotes) Up(db *gorm.DB) error {
	mig := db.Migrator()
	cfg := &v058CompanyConfig{}
	if !mig.HasTable(cfg) {
		return nil
	}
	if !mig.HasColumn(cfg, "AdditionalNotes") {
		if err := mig.AddColumn(cfg, "AdditionalNotes"); err != nil {
			return fmt.Errorf("add tenant_company_configs.additional_notes: %w", err)
		}
	}
	return nil
}
