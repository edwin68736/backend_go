package service

import (
	"errors"

	"tukifac/pkg/database"
	"tukifac/pkg/saas"
)

type SubscriptionService struct{}

func NewSubscriptionService() *SubscriptionService { return &SubscriptionService{} }

type SubscriptionDetail struct {
	database.SaasSubscription
	PlanName string   `json:"plan_name"`
	Modules  []string `json:"modules"`
}

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

func (s *SubscriptionService) GetByTenant(tenantID uint) (*SubscriptionDetail, error) {
	view, err := saas.GetTenantView(tenantID)
	if err != nil || !view.HasSubscription {
		return nil, errors.New("sin suscripción activa")
	}
	var sub database.SaasSubscription
	database.CentralDB.First(&sub, view.SubscriptionID)
	detail := &SubscriptionDetail{SaasSubscription: sub, PlanName: view.PlanName}
	detail.Modules = s.getPlanModules(sub.PlanID)
	detail.Status = view.Status
	return detail, nil
}

type CreateSubscriptionInput struct {
	TenantID uint   `json:"tenant_id"`
	PlanID   uint   `json:"plan_id"`
	Months   int    `json:"months"`
	Notes    string `json:"notes"`
}

func (s *SubscriptionService) Create(input CreateSubscriptionInput) (*database.SaasSubscription, error) {
	sub, err := saas.ExtendSubscription(input.TenantID, input.PlanID, input.Months, input.Notes)
	if err != nil {
		return nil, err
	}
	saas.LogEvent(input.TenantID, &sub.ID, "subscription_created", "admin", nil, input.Notes, "")
	return sub, nil
}

func (s *SubscriptionService) Suspend(id uint, reason string) error {
	var sub database.SaasSubscription
	if err := database.CentralDB.First(&sub, id).Error; err != nil {
		return errors.New("suscripción no encontrada")
	}
	database.CentralDB.Model(&sub).Updates(map[string]interface{}{
		"status": database.SaasSubSuspended, "provisional_until": nil,
	})
	database.CentralDB.Model(&database.Tenant{}).Where("id = ?", sub.TenantID).Update("status", "suspended")
	sid := sub.ID
	saas.LogEvent(sub.TenantID, &sid, "suspended", "admin", nil, reason, "")
	saas.InvalidateTenantCache(sub.TenantID)
	return nil
}

func (s *SubscriptionService) Reactivate(id uint, extraMonths int) error {
	var sub database.SaasSubscription
	if err := database.CentralDB.First(&sub, id).Error; err != nil {
		return errors.New("suscripción no encontrada")
	}
	_, err := saas.ExtendSubscription(sub.TenantID, sub.PlanID, extraMonths, "reactivación manual")
	if err != nil {
		return err
	}
	sid := sub.ID
	saas.LogEvent(sub.TenantID, &sid, "reactivated", "admin", nil, "", "")
	return nil
}

func (s *SubscriptionService) CheckExpirations() int {
	_, _, suspended := saas.RunDailyJobs()
	return suspended
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

// ListEvents auditoría por tenant.
func (s *SubscriptionService) ListEvents(tenantID uint) ([]database.SaasSubscriptionEvent, error) {
	var events []database.SaasSubscriptionEvent
	err := database.CentralDB.Where("tenant_id = ?", tenantID).Order("created_at desc").Limit(100).Find(&events).Error
	return events, err
}

// ListNotificationLogs por tenant.
func (s *SubscriptionService) ListNotificationLogs(tenantID uint) ([]database.SaasNotificationLog, error) {
	var logs []database.SaasNotificationLog
	err := database.CentralDB.Where("tenant_id = ?", tenantID).Order("created_at desc").Limit(100).Find(&logs).Error
	return logs, err
}
