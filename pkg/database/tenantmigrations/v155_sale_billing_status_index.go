package tenantmigrations

import (
	"fmt"

	"gorm.io/gorm"
)

const idxSaleBillingStatus = "idx_tenant_sales_billing_status"

type v155Sale struct {
	BillingStatus string `gorm:"column:billing_status;size:30;index:idx_tenant_sales_billing_status"`
}

func (v155Sale) TableName() string { return "tenant_sales" }

// V155SaleBillingStatusIndex agrega un índice sobre tenant_sales.billing_status.
//
// Sin él, filtrar ventas por estado de envío (campanita de pendientes del header del tenant, el
// resumen fiscal del panel central, el worker de conciliación) recorre toda la tabla. Es solo un
// índice: no cambia datos ni columnas. Idempotente.
type V155SaleBillingStatusIndex struct{}

func (V155SaleBillingStatusIndex) Version() int { return 155 }
func (V155SaleBillingStatusIndex) Name() string { return "sale_billing_status_index" }

func (V155SaleBillingStatusIndex) Up(db *gorm.DB) error {
	mig := db.Migrator()
	if !mig.HasTable("tenant_sales") {
		return nil
	}
	st := &v155Sale{}
	if mig.HasIndex(st, idxSaleBillingStatus) {
		return nil
	}
	if err := mig.CreateIndex(st, idxSaleBillingStatus); err != nil {
		return fmt.Errorf("crear %s: %w", idxSaleBillingStatus, err)
	}
	return nil
}
