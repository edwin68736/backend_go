package tenantmigrations

import (
	"fmt"

	"gorm.io/gorm"
)

type v146CompanyConfig struct {
	ShowBusinessNameOnSaleNote bool `gorm:"column:show_business_name_on_sale_note;default:true"`
}

func (v146CompanyConfig) TableName() string { return "tenant_company_configs" }

// V146CompanyShowBusinessNameOnSaleNote ajuste solo para nota de venta: mostrar u ocultar
// la razón social del emisor en el impreso (con fallback a nombre comercial y viceversa,
// resuelto en el frontend al armar el PDF).
type V146CompanyShowBusinessNameOnSaleNote struct{}

func (V146CompanyShowBusinessNameOnSaleNote) Version() int { return 146 }
func (V146CompanyShowBusinessNameOnSaleNote) Name() string {
	return "company_show_business_name_on_sale_note"
}

func (V146CompanyShowBusinessNameOnSaleNote) Up(db *gorm.DB) error {
	mig := db.Migrator()
	st := &v146CompanyConfig{}
	if !mig.HasColumn(st, "ShowBusinessNameOnSaleNote") {
		if err := mig.AddColumn(st, "ShowBusinessNameOnSaleNote"); err != nil {
			return fmt.Errorf("add tenant_company_configs.show_business_name_on_sale_note: %w", err)
		}
	}
	return nil
}
