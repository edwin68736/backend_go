package tenantmigrations

import (
	"fmt"

	"gorm.io/gorm"
)

// V039FiscalLegacyCleanup elimina columnas fiscales legacy del tenant ERP.
type V039FiscalLegacyCleanup struct{}

func (V039FiscalLegacyCleanup) Version() int { return 39 }
func (V039FiscalLegacyCleanup) Name() string { return "fiscal_legacy_cleanup" }

var v039DropColumns = []string{
	"sunat_sol_user",
	"sunat_sol_pass",
	"sunat_certificate",
	"invoicing_mode",
	"pse_base_url",
	"pse_token",
	"pse_config_json",
	"tukifac_token",
}

func (V039FiscalLegacyCleanup) Up(db *gorm.DB) error {
	if !db.Migrator().HasTable("tenant_company_configs") {
		return nil
	}
	for _, col := range v039DropColumns {
		if db.Migrator().HasColumn("tenant_company_configs", col) {
			if err := db.Exec(fmt.Sprintf("ALTER TABLE tenant_company_configs DROP COLUMN `%s`", col)).Error; err != nil {
				return err
			}
		}
	}
	return nil
}
