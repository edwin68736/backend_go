package service

import (
	"errors"
	"fmt"
	"time"

	"tukifac/internal/subscriptions/service"
	"tukifac/pkg/database"
)

type PaymentService struct {
	subSvc *service.SubscriptionService
}

func NewPaymentService() *PaymentService {
	return &PaymentService{subSvc: service.NewSubscriptionService()}
}

type PaymentDetail struct {
	database.SaasPayment
	TenantName string `json:"tenant_name"`
	TenantSlug string `json:"tenant_slug"`
}

func (s *PaymentService) List(status string) ([]PaymentDetail, error) {
	result := make([]PaymentDetail, 0)
	query := database.CentralDB.Model(&database.SaasPayment{}).Order("created_at desc")
	if status != "" {
		query = query.Where("status = ?", status)
	}
	var payments []database.SaasPayment
	if err := query.Find(&payments).Error; err != nil {
		return nil, err
	}
	for _, p := range payments {
		d := PaymentDetail{SaasPayment: p}
		var tenant database.Tenant
		database.CentralDB.First(&tenant, p.TenantID)
		d.TenantName = tenant.Name
		d.TenantSlug = tenant.Slug
		result = append(result, d)
	}
	return result, nil
}

func (s *PaymentService) GetByID(id uint) (*PaymentDetail, error) {
	var payment database.SaasPayment
	if err := database.CentralDB.First(&payment, id).Error; err != nil {
		return nil, errors.New("pago no encontrado")
	}
	d := &PaymentDetail{SaasPayment: payment}
	var tenant database.Tenant
	database.CentralDB.First(&tenant, payment.TenantID)
	d.TenantName = tenant.Name
	d.TenantSlug = tenant.Slug
	return d, nil
}

type CreatePaymentInput struct {
	TenantID     uint    `json:"tenant_id" form:"tenant_id"`
	Amount       float64 `json:"amount" form:"amount"`
	Currency     string  `json:"currency" form:"currency"`
	PeriodMonths int     `json:"period_months" form:"period_months"`
	Notes        string  `json:"notes" form:"notes"`
	ReceiptURL   string  `json:"receipt_url"`
}

func (s *PaymentService) Create(input CreatePaymentInput) (*database.SaasPayment, error) {
	if input.TenantID == 0 {
		return nil, errors.New("tenant_id es requerido")
	}
	if input.Amount <= 0 {
		return nil, errors.New("el monto debe ser mayor a 0")
	}
	if input.Currency == "" {
		input.Currency = "PEN"
	}
	if input.PeriodMonths <= 0 {
		input.PeriodMonths = 1
	}

	payment := &database.SaasPayment{
		TenantID:     input.TenantID,
		Amount:       input.Amount,
		Currency:     input.Currency,
		PeriodMonths: input.PeriodMonths,
		ReceiptURL:   input.ReceiptURL,
		Notes:        input.Notes,
		Status:       "pending",
	}
	if err := database.CentralDB.Create(payment).Error; err != nil {
		return nil, err
	}
	return payment, nil
}

type ApproveInput struct {
	PlanID     uint   `json:"plan_id"`
	AdminNotes string `json:"admin_notes"`
	ReviewerID uint
}

// Approve aprueba el pago, crea/extiende la suscripción y activa el tenant
func (s *PaymentService) Approve(paymentID uint, input ApproveInput) error {
	var payment database.SaasPayment
	if err := database.CentralDB.First(&payment, paymentID).Error; err != nil {
		return errors.New("pago no encontrado")
	}
	if payment.Status != "pending" {
		return fmt.Errorf("el pago ya fue %s", payment.Status)
	}

	now := time.Now()
	database.CentralDB.Model(&payment).Updates(map[string]interface{}{
		"status":      "approved",
		"admin_notes": input.AdminNotes,
		"reviewed_by": input.ReviewerID,
		"reviewed_at": now,
	})

	// Si se especificó un plan, crear/extender suscripción
	if input.PlanID > 0 {
		_, err := s.subSvc.Create(service.CreateSubscriptionInput{
			TenantID: payment.TenantID,
			PlanID:   input.PlanID,
			Months:   payment.PeriodMonths,
			Notes:    fmt.Sprintf("Pago aprobado ID #%d", paymentID),
		})
		if err != nil {
			return fmt.Errorf("error creando suscripción: %w", err)
		}
	} else {
		// Sin plan explícito: solo reactivar tenant y extender suscripción existente
		sub, err := s.subSvc.GetByTenant(payment.TenantID)
		if err == nil {
			s.subSvc.Reactivate(sub.ID, payment.PeriodMonths)
		} else {
			database.CentralDB.Model(&database.Tenant{}).
				Where("id = ?", payment.TenantID).
				Update("status", "active")
		}
	}

	// Vincular pago a la suscripción activa
	var activeSub database.SaasSubscription
	if err := database.CentralDB.Where("tenant_id = ? AND status = 'active'", payment.TenantID).
		Order("created_at desc").First(&activeSub).Error; err == nil {
		database.CentralDB.Model(&payment).Update("subscription_id", activeSub.ID)
	}

	return nil
}

// Reject rechaza el pago
func (s *PaymentService) Reject(paymentID uint, adminNotes string, reviewerID uint) error {
	var payment database.SaasPayment
	if err := database.CentralDB.First(&payment, paymentID).Error; err != nil {
		return errors.New("pago no encontrado")
	}
	if payment.Status != "pending" {
		return fmt.Errorf("el pago ya fue %s", payment.Status)
	}
	now := time.Now()
	return database.CentralDB.Model(&payment).Updates(map[string]interface{}{
		"status":      "rejected",
		"admin_notes": adminNotes,
		"reviewed_by": reviewerID,
		"reviewed_at": now,
	}).Error
}
