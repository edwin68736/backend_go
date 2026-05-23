package service

import (
	"errors"

	"tukifac/pkg/database"
	"tukifac/pkg/saas"
)

type PaymentService struct{}

func NewPaymentService() *PaymentService { return &PaymentService{} }

type PaymentDetail struct {
	database.SaasPayment
	TenantName string `json:"tenant_name"`
	TenantSlug string `json:"tenant_slug"`
}

func (s *PaymentService) List(status string) ([]PaymentDetail, error) {
	result := make([]PaymentDetail, 0)
	query := database.CentralDB.Model(&database.SaasPayment{}).Order("created_at desc")
	if status == "pending" {
		query = query.Where("status IN ?", []string{database.SaasPayPending, database.SaasPayPendingReview})
	} else if status != "" {
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
	PaymentMethod string `json:"payment_method" form:"payment_method"`
}

func (s *PaymentService) Create(input CreatePaymentInput) (*database.SaasPayment, error) {
	return saas.SubmitPayment(saas.SubmitPaymentInput{
		TenantID:     input.TenantID,
		Amount:       input.Amount,
		PaymentMethod: input.PaymentMethod,
		ReceiptURL:   input.ReceiptURL,
		Notes:        input.Notes,
		FromAdmin:    true,
		PeriodMonths: input.PeriodMonths,
	})
}

type ApproveInput struct {
	PlanID     uint   `json:"plan_id"`
	AdminNotes string `json:"admin_notes"`
	ReviewerID uint
}

func (s *PaymentService) Approve(paymentID uint, input ApproveInput) error {
	return saas.ApprovePayment(paymentID, input.PlanID, 0, input.AdminNotes, input.ReviewerID)
}

func (s *PaymentService) Reject(paymentID uint, adminNotes string, reviewerID uint) error {
	return saas.RejectPayment(paymentID, adminNotes, reviewerID)
}

// PendingCount para dashboard.
func PendingCount() (int64, error) {
	var n int64
	err := database.CentralDB.Model(&database.SaasPayment{}).
		Where("status IN ?", []string{database.SaasPayPendingReview, database.SaasPayPending}).
		Count(&n).Error
	return n, err
}
