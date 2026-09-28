package tenantmigrations

import (
	"fmt"

	"gorm.io/gorm"
)

type v147CompanyConfig struct {
	LogoSizeTicket string `gorm:"column:logo_size_ticket;size:10;default:'mediano'"`
	LogoSizeA4     string `gorm:"column:logo_size_a4;size:10;default:'mediano'"`
}

func (v147CompanyConfig) TableName() string { return "tenant_company_configs" }

// V147CompanyLogoSizeByFormat tamaño del logo en TODOS los comprobantes, separado por
// formato (ticket vs A4): las proporciones de rollo térmico y hoja A4 son muy distintas.
type V147CompanyLogoSizeByFormat struct{}

func (V147CompanyLogoSizeByFormat) Version() int { return 147 }
func (V147CompanyLogoSizeByFormat) Name() string { return "company_logo_size_by_format" }

func (V147CompanyLogoSizeByFormat) Up(db *gorm.DB) error {
	mig := db.Migrator()
	st := &v147CompanyConfig{}
	if !mig.HasColumn(st, "LogoSizeTicket") {
		if err := mig.AddColumn(st, "LogoSizeTicket"); err != nil {
			return fmt.Errorf("add tenant_company_configs.logo_size_ticket: %w", err)
		}
	}
	if !mig.HasColumn(st, "LogoSizeA4") {
		if err := mig.AddColumn(st, "LogoSizeA4"); err != nil {
			return fmt.Errorf("add tenant_company_configs.logo_size_a4: %w", err)
		}
	}
	return nil
}
