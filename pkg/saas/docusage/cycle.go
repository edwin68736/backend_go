package docusage

import (
	"errors"

	"tukifac/pkg/database"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// CurrentBillingCycle obtiene o crea el ciclo vigente para cuota de documentos.
func CurrentBillingCycle(tenantID uint) (*database.SaasBillingCycle, *database.SaasSubscription, error) {
	if database.CentralDB == nil {
		return nil, nil, errors.New("BD central no disponible")
	}
	var sub database.SaasSubscription
	err := database.CentralDB.Where("tenant_id = ?", tenantID).
		Where("status NOT IN ?", []string{database.SaasSubCancelled, database.SaasSubExpired}).
		Order("created_at desc").First(&sub).Error
	if err != nil {
		return nil, nil, ErrNoActiveCycle
	}
	c, err := EnsureBillingCycleForSubscription(&sub)
	if err != nil || c == nil {
		return nil, &sub, ErrNoActiveCycle
	}
	_ = SyncCycleDocumentQuotaFromPlan(c, sub.PlanID)
	_ = database.CentralDB.First(c, c.ID).Error
	return c, &sub, nil
}

func lockCycle(tx *gorm.DB, cycleID uint) (*database.SaasBillingCycle, error) {
	var cycle database.SaasBillingCycle
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&cycle, cycleID).Error; err != nil {
		return nil, err
	}
	return &cycle, nil
}
