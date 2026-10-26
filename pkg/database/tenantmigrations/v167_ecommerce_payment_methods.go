package tenantmigrations

import (
	"fmt"

	"gorm.io/gorm"
)

type v167EcommerceSettings struct {
	ID                 uint   `gorm:"primaryKey"`
	PaymentMethodsJSON string `gorm:"column:payment_methods_json;type:text"`
}

func (v167EcommerceSettings) TableName() string { return "tenant_ecommerce_settings" }

// V167EcommercePaymentMethods agrega payment_methods_json: los medios de pago (Yape, Plin, cuentas
// bancarias, efectivo…) que el tenant muestra al cliente al hacer el pedido en la tienda virtual.
// Solo informativo: la tienda no cobra ni valida pagos. Idempotente.
type V167EcommercePaymentMethods struct{}

func (V167EcommercePaymentMethods) Version() int { return 167 }
func (V167EcommercePaymentMethods) Name() string { return "ecommerce_payment_methods" }

func (V167EcommercePaymentMethods) Up(db *gorm.DB) error {
	mig := db.Migrator()
	if !mig.HasTable(&v167EcommerceSettings{}) {
		return nil
	}
	if mig.HasColumn(&v167EcommerceSettings{}, "PaymentMethodsJSON") {
		return nil
	}
	if err := mig.AddColumn(&v167EcommerceSettings{}, "PaymentMethodsJSON"); err != nil {
		return fmt.Errorf("add tenant_ecommerce_settings.payment_methods_json: %w", err)
	}
	return nil
}
