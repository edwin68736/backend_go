package tenantmigrations

import (
	"fmt"

	"gorm.io/gorm"
)

// Structs mínimos: solo las columnas nuevas (evita un AutoMigrate del struct completo, que emitiría ALTER ... MODIFY sobre tablas grandes).
type v188CompanyConfig struct {
	ID                   uint    `gorm:"primaryKey"`
	ServiceChargeEnabled bool    `gorm:"column:service_charge_enabled;default:false"`
	ServiceChargeRate    float64 `gorm:"column:service_charge_rate;type:decimal(5,2);default:10"`
}

func (v188CompanyConfig) TableName() string { return "tenant_company_configs" }

type v188Sale struct {
	ID                  uint    `gorm:"primaryKey"`
	ServiceChargeRate   float64 `gorm:"column:service_charge_rate;type:decimal(5,2);default:0"`
	ServiceChargeAmount float64 `gorm:"column:service_charge_amount;type:decimal(15,2);default:0"`
}

func (v188Sale) TableName() string { return "tenant_sales" }

// V188ServiceCharge agrega el recargo al consumo: interruptor y porcentaje por empresa, y tasa/monto cobrados en cada venta. Solo agrega
// columnas con default (0 / apagado): las ventas existentes quedan idénticas y no hace falta backfill. Idempotente.
type V188ServiceCharge struct{}

func (V188ServiceCharge) Version() int { return 188 }
func (V188ServiceCharge) Name() string { return "service_charge" }

func (V188ServiceCharge) Up(db *gorm.DB) error {
	mig := db.Migrator()
	cfg := &v188CompanyConfig{}
	for _, f := range []string{"ServiceChargeEnabled", "ServiceChargeRate"} {
		if !mig.HasColumn(cfg, f) {
			if err := mig.AddColumn(cfg, f); err != nil {
				return fmt.Errorf("add tenant_company_configs.%s: %w", f, err)
			}
		}
	}
	sale := &v188Sale{}
	for _, f := range []string{"ServiceChargeRate", "ServiceChargeAmount"} {
		if !mig.HasColumn(sale, f) {
			if err := mig.AddColumn(sale, f); err != nil {
				return fmt.Errorf("add tenant_sales.%s: %w", f, err)
			}
		}
	}
	return nil
}
