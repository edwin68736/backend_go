package tenantmigrations

import (
	"fmt"

	"gorm.io/gorm"
)

type v153CompanyConfig struct {
	ShowIgvBreakdownOnSaleNote bool `gorm:"column:show_igv_breakdown_on_sale_note;default:true"`
}

func (v153CompanyConfig) TableName() string { return "tenant_company_configs" }

// V153CompanyShowIgvBreakdownOnSaleNote ajuste solo para nota de venta: discriminar o no Op.
// gravadas / IGV en el impreso (ticket, A4 y ESC/POS). Default true = comportamiento previo.
type V153CompanyShowIgvBreakdownOnSaleNote struct{}

func (V153CompanyShowIgvBreakdownOnSaleNote) Version() int { return 153 }
func (V153CompanyShowIgvBreakdownOnSaleNote) Name() string {
	return "company_show_igv_breakdown_on_sale_note"
}

func (V153CompanyShowIgvBreakdownOnSaleNote) Up(db *gorm.DB) error {
	mig := db.Migrator()
	st := &v153CompanyConfig{}
	if !mig.HasColumn(st, "ShowIgvBreakdownOnSaleNote") {
		if err := mig.AddColumn(st, "ShowIgvBreakdownOnSaleNote"); err != nil {
			return fmt.Errorf("add tenant_company_configs.show_igv_breakdown_on_sale_note: %w", err)
		}
	}
	return nil
}
