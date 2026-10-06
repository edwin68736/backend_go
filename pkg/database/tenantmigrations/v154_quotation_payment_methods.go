package tenantmigrations

import (
	"fmt"

	"gorm.io/gorm"
)

type v154Quotation struct {
	PaymentMethodsJSON string `gorm:"column:payment_methods_json;type:text"`
}

func (v154Quotation) TableName() string { return "tenant_quotations" }

// V154QuotationPaymentMethods agrega tenant_quotations.payment_methods_json: métodos de pago de
// REFERENCIA de la cotización (cómo piensa pagar el cliente). No es un cobro: no toca caja ni saldos.
type V154QuotationPaymentMethods struct{}

func (V154QuotationPaymentMethods) Version() int { return 154 }
func (V154QuotationPaymentMethods) Name() string { return "quotation_payment_methods" }

func (V154QuotationPaymentMethods) Up(db *gorm.DB) error {
	mig := db.Migrator()
	if !mig.HasTable("tenant_quotations") {
		return nil
	}
	st := &v154Quotation{}
	if !mig.HasColumn(st, "PaymentMethodsJSON") {
		if err := mig.AddColumn(st, "PaymentMethodsJSON"); err != nil {
			return fmt.Errorf("add tenant_quotations.payment_methods_json: %w", err)
		}
	}
	return nil
}
