package tenantmigrations

import (
	"fmt"

	"gorm.io/gorm"
)

// V038CashSessionsPerUser columnas Fase C + soporte sesión por usuario.
type V038CashSessionsPerUser struct{}

func (V038CashSessionsPerUser) Version() int { return 38 }
func (V038CashSessionsPerUser) Name() string { return "cash_sessions_per_user_prep" }

type v038CashSession struct {
	RegisterCode *string `gorm:"column:register_code;size:50"`
	RegisterName *string `gorm:"column:register_name;size:100"`
}

func (v038CashSession) TableName() string { return "tenant_cash_sessions" }

func (V038CashSessionsPerUser) Up(db *gorm.DB) error {
	mig := db.Migrator()
	st := &v038CashSession{}
	if !mig.HasTable(st) {
		return fmt.Errorf("tenant_cash_sessions no existe")
	}
	if !mig.HasColumn(st, "RegisterCode") {
		if err := mig.AddColumn(st, "RegisterCode"); err != nil {
			return err
		}
	}
	if !mig.HasColumn(st, "RegisterName") {
		if err := mig.AddColumn(st, "RegisterName"); err != nil {
			return err
		}
	}
	return nil
}
