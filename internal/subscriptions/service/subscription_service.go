package service

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"tukifac/pkg/database"
	"tukifac/pkg/pagination"
	"tukifac/pkg/saas"
)

type SubscriptionService struct{}

func NewSubscriptionService() *SubscriptionService { return &SubscriptionService{} }

type SubscriptionDetail struct {
	database.SaasSubscription
	PlanName   string   `json:"plan_name"`
	TenantName string   `json:"tenant_name"`
	Modules    []string `json:"modules"`
}

type SubscriptionListParams struct {
	Status  string
	Query   string
	Page    int
	PerPage int
}

func (s *SubscriptionService) List(params SubscriptionListParams) ([]SubscriptionDetail, int64, error) {
	page, perPage := pagination.Normalize(params.Page, params.PerPage)
	q := database.CentralDB.Model(&database.SaasSubscription{})
	if params.Status != "" {
		q = q.Where("saas_subscriptions.status = ?", params.Status)
	}
	if strings.TrimSpace(params.Query) != "" {
		like := "%" + strings.TrimSpace(params.Query) + "%"
		q = q.Joins("JOIN tenants ON tenants.id = saas_subscriptions.tenant_id").
			Where("tenants.name LIKE ? OR tenants.ruc LIKE ? OR tenants.slug LIKE ?", like, like, like)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var subs []database.SaasSubscription
	if err := q.Order("saas_subscriptions.created_at desc").
		Limit(perPage).
		Offset(pagination.Offset(page, perPage)).
		Find(&subs).Error; err != nil {
		return nil, 0, err
	}

	tenantNames := map[uint]string{}
	if len(subs) > 0 {
		ids := make([]uint, 0, len(subs))
		for _, sub := range subs {
			ids = append(ids, sub.TenantID)
		}
		var tenants []database.Tenant
		database.CentralDB.Select("id", "name").Where("id IN ?", ids).Find(&tenants)
		for _, t := range tenants {
			tenantNames[t.ID] = t.Name
		}
	}

	result := make([]SubscriptionDetail, 0, len(subs))
	for _, sub := range subs {
		detail := SubscriptionDetail{
			SaasSubscription: sub,
			TenantName:       tenantNames[sub.TenantID],
		}
		var plan database.SaasPlan
		database.CentralDB.First(&plan, sub.PlanID)
		detail.PlanName = plan.Name
		detail.Modules = s.getPlanModules(sub.PlanID)
		result = append(result, detail)
	}
	return result, total, nil
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

type AdjustValidityInput struct {
	EndDate string `json:"end_date"`
	Reason  string `json:"reason"`
}

// AdjustValidity corrige end_date sin alterar plan, módulos ni ciclos de facturación.
func (s *SubscriptionService) AdjustValidity(id, saUserID uint, clientIP string, input AdjustValidityInput) (*database.SaasSubscription, error) {
	reason := strings.TrimSpace(input.Reason)
	if reason == "" {
		return nil, errors.New("el motivo es obligatorio")
	}
	endDay, err := parseEndDateLima(input.EndDate)
	if err != nil {
		return nil, err
	}
	newEnd := saas.EndOfDayLima(endDay)

	var sub database.SaasSubscription
	if err := database.CentralDB.First(&sub, id).Error; err != nil {
		return nil, errors.New("suscripción no encontrada")
	}

	var tenant database.Tenant
	if err := database.CentralDB.First(&tenant, sub.TenantID).Error; err != nil {
		return nil, errors.New("tenant no encontrado")
	}

	manuallySuspended := sub.Status == database.SaasSubSuspended
	oldEndStr := sub.EndDate.In(saas.LimaLocation()).Format("2006-01-02")
	newEndStr := newEnd.In(saas.LimaLocation()).Format("2006-01-02")

	if err := database.CentralDB.Model(&sub).Update("end_date", newEnd).Error; err != nil {
		return nil, err
	}
	sub.EndDate = newEnd

	cfg, _ := saas.LoadSettings()
	now := saas.NowLima()
	effective := saas.ResolveEffectiveStatus(&sub, &tenant, now, cfg.GracePeriodDays)

	if !manuallySuspended && sub.Status != database.SaasSubCancelled {
		newStatus := recalcStoredSubscriptionStatus(&sub, effective, now, cfg.GracePeriodDays)
		if newStatus != sub.Status {
			_ = database.CentralDB.Model(&sub).Update("status", newStatus).Error
			sub.Status = newStatus
		}
	}

	if shouldReactivateTenantAfterValidity(manuallySuspended, &tenant, effective) {
		if tenant.Status == database.TenantStatusSuspended {
			_ = database.CentralDB.Model(&tenant).Update("status", database.TenantStatusActive).Error
		}
	}

	saas.InvalidateTenantCache(sub.TenantID)

	meta, _ := json.Marshal(map[string]interface{}{
		"previous_end_date": oldEndStr,
		"new_end_date":      newEndStr,
		"effective_status":  effective,
		"ip":                clientIP,
	})
	sid := sub.ID
	actorID := saUserID
	saas.LogEvent(sub.TenantID, &sid, saas.EventValidityAdjusted, "admin", &actorID, reason, string(meta))

	payload, _ := json.Marshal(map[string]interface{}{
		"subscription_id":   sub.ID,
		"previous_end_date": oldEndStr,
		"new_end_date":      newEndStr,
		"reason":            reason,
		"effective_status":  effective,
	})
	_ = database.CentralDB.Create(&database.AuditLog{
		TenantID:  sub.TenantID,
		UserID:    saUserID,
		Action:    "subscription_validity_adjusted",
		Entity:    "saas_subscription",
		EntityID:  sub.ID,
		Payload:   string(payload),
		IPAddress: clientIP,
	}).Error

	return &sub, nil
}

func parseEndDateLima(raw string) (time.Time, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return time.Time{}, errors.New("fecha de vencimiento requerida")
	}
	t, err := time.ParseInLocation("2006-01-02", s, saas.LimaLocation())
	if err != nil {
		return time.Time{}, errors.New("fecha de vencimiento inválida (use YYYY-MM-DD)")
	}
	return t, nil
}

func recalcStoredSubscriptionStatus(sub *database.SaasSubscription, effective string, now time.Time, graceDays int) string {
	if sub.Status == database.SaasSubTrial {
		switch effective {
		case database.SaasSubTrial, database.SaasSubActive, database.SaasSubGracePeriod, database.SaasSubProvisionalActive:
			return database.SaasSubTrial
		}
	}
	switch effective {
	case database.SaasSubActive, database.SaasSubTrial, database.SaasSubGracePeriod,
		database.SaasSubProvisionalActive, database.SaasSubOverdue:
		if sub.Status == database.SaasSubTrial {
			return database.SaasSubTrial
		}
		return database.SaasSubActive
	}
	if saas.CalendarDaysAfterEnd(sub.EndDate, now) > 0 {
		if graceDays <= 0 || saas.CalendarDaysAfterEnd(sub.EndDate, now) > graceDays {
			return database.SaasSubExpired
		}
	}
	return database.SaasSubActive
}

func shouldReactivateTenantAfterValidity(manuallySuspended bool, tenant *database.Tenant, effective string) bool {
	if manuallySuspended || tenant == nil {
		return false
	}
	if tenant.Status == "inactive" {
		return false
	}
	cfg, _ := saas.LoadSettings()
	if tenant.Status == database.TenantStatusBlocked || tenant.PaymentBlocked ||
		tenant.StrikeCount >= saas.EffectiveStrikeMax(cfg) {
		return false
	}
	switch effective {
	case database.SaasSubActive, database.SaasSubTrial, database.SaasSubGracePeriod, database.SaasSubProvisionalActive:
		return true
	}
	return false
}
