package database

import (
	"fmt"

	"gorm.io/gorm"
)

// PurgeTenantCentralData elimina registros del tenant en BD central (hard delete).
// Libera slug/db_name para poder reutilizar el mismo identificador.
func PurgeTenantCentralData(db *gorm.DB, tenantID uint) error {
	if db == nil {
		return fmt.Errorf("BD central no disponible")
	}
	if tenantID == 0 {
		return fmt.Errorf("tenant_id inválido")
	}

	return db.Transaction(func(tx *gorm.DB) error {
		byTenant := func(model interface{}) error {
			if err := tx.Unscoped().Where("tenant_id = ?", tenantID).Delete(model).Error; err != nil {
				return fmt.Errorf("purge %T: %w", model, err)
			}
			return nil
		}

		tables := []interface{}{
			&SaasElectronicDocumentUsage{},
			&SaasTenantDocumentPackage{},
			&SaasNotificationLog{},
			&SaasSubscriptionEvent{},
			&SaasPayment{},
			&SaasBillingCycle{},
			&SaasSubscription{},
			&TenantModule{},
			&TenantSchemaVersion{},
			&AuditLog{},
		}
		for _, m := range tables {
			if err := byTenant(m); err != nil {
				return err
			}
		}

		// Hard delete: GORM Delete con DeletedAt solo marca deleted_at y el slug único queda ocupado.
		res := tx.Unscoped().Delete(&Tenant{}, tenantID)
		if res.Error != nil {
			return fmt.Errorf("purge tenant row: %w", res.Error)
		}
		if res.RowsAffected == 0 {
			return fmt.Errorf("tenant id=%d no encontrado en BD central", tenantID)
		}
		return nil
	})
}
