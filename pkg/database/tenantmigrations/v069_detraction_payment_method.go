package tenantmigrations

import (
	"fmt"

	"tukifac/pkg/database"

	"gorm.io/gorm"
)

// V069DetractionPaymentMethod agrega método de pago interno detraccion_bn (SPOT).
type V069DetractionPaymentMethod struct{}

func (V069DetractionPaymentMethod) Version() int  { return 69 }
func (V069DetractionPaymentMethod) Name() string { return "detraction_payment_method" }

func (V069DetractionPaymentMethod) Up(db *gorm.DB) error {
	if err := database.EnsureDetractionPaymentMethod(db); err != nil {
		return fmt.Errorf("tenant detraction payment method: %w", err)
	}
	return nil
}
