package tenantmigrations

import (
	"fmt"

	"gorm.io/gorm"
)

type v158BankAccount struct {
	ID  uint   `gorm:"primaryKey"`
	CCI string `gorm:"column:cci;size:40"`
}

func (v158BankAccount) TableName() string { return "tenant_bank_accounts" }

// V158BankAccountCCI agrega tenant_bank_accounts.cci: el Código de Cuenta Interbancario (20
// dígitos) de la cuenta, que se imprime junto al número de cuenta en los comprobantes y
// cotizaciones. Columna nullable sin backfill; idempotente.
type V158BankAccountCCI struct{}

func (V158BankAccountCCI) Version() int { return 158 }
func (V158BankAccountCCI) Name() string { return "bank_account_cci" }

func (V158BankAccountCCI) Up(db *gorm.DB) error {
	mig := db.Migrator()
	if !mig.HasTable(&v158BankAccount{}) || mig.HasColumn(&v158BankAccount{}, "CCI") {
		return nil
	}
	if err := mig.AddColumn(&v158BankAccount{}, "CCI"); err != nil {
		return fmt.Errorf("add tenant_bank_accounts.cci: %w", err)
	}
	return nil
}
