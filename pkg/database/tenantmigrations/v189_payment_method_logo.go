package tenantmigrations

import (
	"fmt"

	"gorm.io/gorm"
)

// Struct mínimo: solo la columna nueva (evita un AutoMigrate del struct completo).
type v189PaymentMethod struct {
	ID      uint   `gorm:"primaryKey"`
	LogoURL string `gorm:"column:logo_url;type:text"`
}

func (v189PaymentMethod) TableName() string { return "tenant_payment_methods" }

// V189PaymentMethodLogo agrega el logo propio de cada método de pago (data URL pequeño, ya reducido por el panel).
// Solo agrega una columna nullable: los métodos existentes siguen usando el icono por defecto. Idempotente.
type V189PaymentMethodLogo struct{}

func (V189PaymentMethodLogo) Version() int { return 189 }
func (V189PaymentMethodLogo) Name() string { return "payment_method_logo" }

func (V189PaymentMethodLogo) Up(db *gorm.DB) error {
	mig := db.Migrator()
	m := &v189PaymentMethod{}
	if !mig.HasColumn(m, "LogoURL") {
		if err := mig.AddColumn(m, "LogoURL"); err != nil {
			return fmt.Errorf("add tenant_payment_methods.logo_url: %w", err)
		}
	}
	return nil
}
