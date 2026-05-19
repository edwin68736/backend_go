package service

import (
	"errors"
	"time"

	"tukifac/pkg/database"
)

type SubscriptionService struct{}

func NewSubscriptionService() *SubscriptionService { return &SubscriptionService{} }

type SubscriptionDetail struct {
	database.SaasSubscription
	PlanName string   `json:"plan_name"`
	Modules  []string `json:"modules"`
}

// List devuelve todas las suscripciones (con nombre del plan)
func (s *SubscriptionService) List(status string) ([]SubscriptionDetail, error) {
	result := make([]SubscriptionDetail, 0)
	query := database.CentralDB.Model(&database.SaasSubscription{}).Order("created_at desc")
	if status != "" {
		query = query.Where("status = ?", status)
	}
	var subs []database.SaasSubscription
	if err := query.Find(&subs).Error; err != nil {
		return nil, err
	}
	for _, sub := range subs {
		detail := SubscriptionDetail{SaasSubscription: sub}
		var plan database.SaasPlan
		database.CentralDB.First(&plan, sub.PlanID)
		detail.PlanName = plan.Name
		detail.Modules = s.getPlanModules(sub.PlanID)
		result = append(result, detail)
	}
	return result, nil
}

// GetByTenant devuelve la suscripción activa de un tenant
func (s *SubscriptionService) GetByTenant(tenantID uint) (*SubscriptionDetail, error) {
	var sub database.SaasSubscription
	err := database.CentralDB.
		Where("tenant_id = ? AND status IN ('active','trial')", tenantID).
		Order("created_at desc").
		First(&sub).Error
	if err != nil {
		return nil, errors.New("sin suscripción activa")
	}
	detail := &SubscriptionDetail{SaasSubscription: sub}
	var plan database.SaasPlan
	database.CentralDB.First(&plan, sub.PlanID)
	detail.PlanName = plan.Name
	detail.Modules = s.getPlanModules(sub.PlanID)
	return detail, nil
}

type CreateSubscriptionInput struct {
	TenantID uint   `json:"tenant_id"`
	PlanID   uint   `json:"plan_id"`
	Months   int    `json:"months"`
	Notes    string `json:"notes"`
}

// Create crea una nueva suscripción y sincroniza módulos del tenant
func (s *SubscriptionService) Create(input CreateSubscriptionInput) (*database.SaasSubscription, error) {
	if input.TenantID == 0 || input.PlanID == 0 {
		return nil, errors.New("tenant_id y plan_id son requeridos")
	}
	if input.Months <= 0 {
		input.Months = 1
	}

	var plan database.SaasPlan
	if err := database.CentralDB.First(&plan, input.PlanID).Error; err != nil {
		return nil, errors.New("plan no encontrado")
	}

	// Expirar suscripciones anteriores
	database.CentralDB.Model(&database.SaasSubscription{}).
		Where("tenant_id = ? AND status IN ('active','trial')", input.TenantID).
		Update("status", "expired")

	now := time.Now()
	sub := &database.SaasSubscription{
		TenantID:  input.TenantID,
		PlanID:    input.PlanID,
		StartDate: now,
		EndDate:   now.AddDate(0, input.Months, 0),
		Status:    "active",
		Notes:     input.Notes,
	}
	if err := database.CentralDB.Create(sub).Error; err != nil {
		return nil, err
	}

	// Sincronizar módulos del tenant según el plan
	s.syncTenantModules(input.TenantID, input.PlanID)

	// Actualizar campo plan del tenant
	database.CentralDB.Model(&database.Tenant{}).Where("id = ?", input.TenantID).
		Updates(map[string]interface{}{"plan": plan.Name, "status": "active"})

	return sub, nil
}

// Suspend suspende manualmente una suscripción
func (s *SubscriptionService) Suspend(id uint, reason string) error {
	var sub database.SaasSubscription
	if err := database.CentralDB.First(&sub, id).Error; err != nil {
		return errors.New("suscripción no encontrada")
	}
	database.CentralDB.Model(&sub).Updates(map[string]interface{}{
		"status": "suspended",
		"notes":  reason,
	})
	// Suspensión manual: sí actualiza el estado del tenant
	database.CentralDB.Model(&database.Tenant{}).Where("id = ?", sub.TenantID).
		Update("status", "suspended")
	return nil
}

// Reactivate reactiva una suscripción suspendida/expirada
func (s *SubscriptionService) Reactivate(id uint, extraMonths int) error {
	var sub database.SaasSubscription
	if err := database.CentralDB.First(&sub, id).Error; err != nil {
		return errors.New("suscripción no encontrada")
	}
	newEnd := time.Now()
	if sub.EndDate.After(newEnd) {
		newEnd = sub.EndDate
	}
	if extraMonths > 0 {
		newEnd = newEnd.AddDate(0, extraMonths, 0)
	}
	database.CentralDB.Model(&sub).Updates(map[string]interface{}{
		"status":   "active",
		"end_date": newEnd,
	})
	database.CentralDB.Model(&database.Tenant{}).Where("id = ?", sub.TenantID).
		Update("status", "active")
	return nil
}

// CheckExpirations marca suscripciones vencidas como "expired".
// NO modifica Tenant.Status — la suspensión del tenant es siempre manual desde el panel central.
func (s *SubscriptionService) CheckExpirations() int {
	now := time.Now()
	var expired []database.SaasSubscription
	database.CentralDB.
		Where("status = 'active' AND end_date < ?", now).
		Find(&expired)

	count := 0
	for _, sub := range expired {
		database.CentralDB.Model(&sub).Update("status", "expired")
		count++
	}
	return count
}

func (s *SubscriptionService) getPlanModules(planID uint) []string {
	modules := make([]string, 0)
	var pms []database.SaasPlanModule
	database.CentralDB.Where("plan_id = ?", planID).Find(&pms)
	for _, pm := range pms {
		modules = append(modules, pm.ModuleKey)
	}
	return modules
}

func (s *SubscriptionService) syncTenantModules(tenantID, planID uint) {
	planModules := s.getPlanModules(planID)
	planSet := make(map[string]bool)
	for _, m := range planModules {
		planSet[m] = true
	}

	// Desactivar todos primero
	database.CentralDB.Model(&database.TenantModule{}).
		Where("tenant_id = ?", tenantID).
		Update("enabled", false)

	// Activar solo los del plan
	for key := range planSet {
		var tm database.TenantModule
		err := database.CentralDB.Where("tenant_id = ? AND module_key = ?", tenantID, key).First(&tm).Error
		if err != nil {
			cfgJSON := "{}"
			database.CentralDB.Create(&database.TenantModule{
				TenantID:   tenantID,
				ModuleKey:  key,
				Enabled:    true,
				ConfigJSON: &cfgJSON,
			})
		} else {
			database.CentralDB.Model(&tm).Update("enabled", true)
		}
	}
}
