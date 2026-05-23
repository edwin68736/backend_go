package saas

import (
	"tukifac/pkg/database"
	"tukifac/pkg/saas/docusage"
)

// EnsureBillingCycle crea ciclo de cobro pendiente si no existe para el período actual.
func EnsureBillingCycle(sub *database.SaasSubscription) (*database.SaasBillingCycle, error) {
	if sub == nil {
		return nil, nil
	}
	return docusage.EnsureBillingCycleForSubscription(sub)
}

// MarkCyclePaid vincula ciclo con pago aprobado.
func MarkCyclePaid(cycleID uint, paymentID uint) error {
	now := NowLima()
	return database.CentralDB.Model(&database.SaasBillingCycle{}).Where("id = ?", cycleID).
		Updates(map[string]interface{}{
			"status":     database.SaasInvoicePaid,
			"paid_at":    now,
			"payment_id": paymentID,
		}).Error
}
