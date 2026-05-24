package service

import (
	"errors"
	"log/slog"

	"tukifac/pkg/billingstate"
	"tukifac/pkg/database"
	"tukifac/pkg/saas/docusage"

	"gorm.io/gorm"
)

// AutoEnqueueOutcome resultado del auto-enqueue post-venta.
type AutoEnqueueOutcome struct {
	Enqueued bool   `json:"enqueued"`
	Skipped  bool   `json:"skipped"`
	Reason   string `json:"reason,omitempty"`
	Status   string `json:"status,omitempty"`
}

// MaybeAutoEnqueueAfterSaleCommit encola emisión fiscal tras commit exitoso de venta.
func (s *BillingService) MaybeAutoEnqueueAfterSaleCommit(saleID, tenantID uint, tenantSlug, tenantDB string, source FiscalOpSource) AutoEnqueueOutcome {
	if tenantID > 0 {
		s.centralTenantID = tenantID
	}
	if tenantSlug != "" {
		s.tenantSlug = tenantSlug
	}
	out := AutoEnqueueOutcome{}

	var cfg database.TenantCompanyConfig
	if err := s.db.First(&cfg).Error; err != nil {
		out.Skipped = true
		out.Reason = "config_no_disponible"
		return out
	}
	if !cfg.SunatEnabled {
		out.Skipped = true
		out.Reason = "sunat_disabled"
		return out
	}
	if !cfg.AutomaticSend {
		out.Skipped = true
		out.Reason = "automatic_send_disabled"
		out.Status = billingstate.BillingPending
		return out
	}

	sunatCode := s.saleSunatCode(saleID)
	if !IsElectronicSunatCode(sunatCode) {
		out.Skipped = true
		out.Reason = "not_electronic_document"
		return out
	}

	inv, err := s.EnqueueSendToSUNAT(saleID, tenantID, tenantSlug, tenantDB, source)
	if err != nil {
		if errors.Is(err, errAlreadyAccepted) {
			out.Skipped = true
			out.Reason = "already_accepted"
			out.Status = "already_accepted"
			return out
		}
		if errors.Is(err, errAlreadyProcessing) {
			out.Skipped = true
			out.Reason = "already_processing"
			out.Status = "already_processing"
			return out
		}
		if errors.Is(err, docusage.ErrQuotaExceeded) {
			out.Skipped = true
			out.Reason = "quota_exceeded"
			s.logFiscalOp(source, tenantID, saleID, "quota_exceeded", slog.Any("error", err))
			return out
		}
		out.Skipped = true
		out.Reason = err.Error()
		s.logFiscalOp(source, tenantID, saleID, "enqueue_failed", slog.Any("error", err))
		return out
	}

	out.Enqueued = true
	if inv != nil {
		out.Status = billingstate.DisplayPhaseFromPipeline(inv.PipelineStatus, inv.JobStatus)
	}
	s.logFiscalOp(source, tenantID, saleID, "enqueued",
		slog.String("document_type", sunatCode),
	)
	return out
}

// TriggerAutoEnqueueAfterSaleCommit hook post-venta (POST /sales, restaurant bill, etc.).
func TriggerAutoEnqueueAfterSaleCommit(db *gorm.DB, tenant *database.Tenant, saleID uint) AutoEnqueueOutcome {
	if db == nil || tenant == nil || saleID == 0 {
		return AutoEnqueueOutcome{}
	}
	svc := NewBillingService(db)
	svc.SetCentralTenantID(tenant.ID)
	svc.SetTenantSlug(tenant.Slug)
	return svc.MaybeAutoEnqueueAfterSaleCommit(saleID, tenant.ID, tenant.Slug, tenant.DBName, FiscalSourceAutoCreate)
}
