package service

import (
	"errors"
	"time"

	"tukifac/pkg/billingstate"
	"tukifac/pkg/database"
	"tukifac/pkg/fiscalqueue"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// EmitFiscal construye documento y encola en facturador (SSOT).
func (s *BillingService) EmitFiscal(saleID uint) (*database.TenantInvoice, error) {
	if err := requireFiscalClient(); err != nil {
		return nil, err
	}
	if s.centralTenantID == 0 || s.tenantSlug == "" {
		return nil, errors.New("contexto tenant SaaS requerido")
	}
	var cfg database.TenantCompanyConfig
	if err := s.db.First(&cfg).Error; err != nil || !cfg.SunatEnabled {
		return nil, errors.New("facturación electrónica no habilitada")
	}
	return s.sendToFacturador(saleID, &cfg)
}

// EnqueueFiscalEmit encola job en cola fiscal Redis (worker → facturador).
func (s *BillingService) EnqueueFiscalEmit(saleID, tenantID uint, tenantSlug, tenantDB, idemKey string) (*database.TenantInvoice, error) {
	if tenantID > 0 {
		s.centralTenantID = tenantID
	}
	tenantSlug = ResolveTenantSlug(tenantID, tenantDB, tenantSlug)
	if tenantSlug != "" {
		s.tenantSlug = tenantSlug
	}
	if tenantID == 0 || tenantSlug == "" {
		return nil, errors.New("contexto tenant SaaS requerido (tenant_id y tenant_slug)")
	}

	if !fiscalqueue.Enabled() {
		return s.EmitFiscal(saleID)
	}

	claimed, err := fiscalqueue.TryClaim(idemKey, 120*time.Second)
	if err != nil {
		return nil, err
	}
	if !claimed {
		var existing database.TenantInvoice
		if err := s.db.Where("sale_id = ?", saleID).First(&existing).Error; err != nil {
			return nil, err
		}
		return &existing, nil
	}

	enqueued := false
	defer func() {
		if !enqueued {
			fiscalqueue.ReleaseClaim(idemKey)
		}
	}()

	var inv database.TenantInvoice
	err = s.db.Transaction(func(tx *gorm.DB) error {
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("sale_id = ?", saleID).First(&inv).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			inv = database.TenantInvoice{
				SaleID:         saleID,
				JobStatus:      fiscalqueue.StatusPending,
				PipelineStatus: billingstate.PENDING_FISCAL,
				IdempotencyKey: idemKey,
				SunatStatus:    "pending",
			}
			return tx.Create(&inv).Error
		}
		if err != nil {
			return err
		}
		if billingstate.HasFinalSunatOutcome(&inv) && billingstate.HasAcceptanceEvidence(&inv) {
			return errAlreadyAccepted
		}
		return tx.Model(&inv).Updates(map[string]interface{}{
			"job_status":      fiscalqueue.StatusPending,
			"pipeline_status": billingstate.PENDING_FISCAL,
			"idempotency_key": idemKey,
			"job_last_error":  "",
		}).Error
	})
	if errors.Is(err, errAlreadyAccepted) {
		fiscalqueue.ReleaseClaim(idemKey)
		return &inv, errAlreadyAccepted
	}
	if err != nil {
		return nil, err
	}

	job := fiscalqueue.Job{
		TenantDB:       tenantDB,
		TenantID:       tenantID,
		TenantSlug:     tenantSlug,
		SaleID:         saleID,
		IdempotencyKey: idemKey,
	}
	if err := fiscalqueue.Enqueue(job); err != nil {
		_ = s.db.Model(&inv).Updates(map[string]interface{}{
			"job_status":      fiscalqueue.StatusFailed,
			"pipeline_status": billingstate.FAILED,
			"job_last_error":  err.Error(),
		}).Error
		return &inv, err
	}

	enqueued = true
	_ = billingstate.SyncSaleBillingStatus(s.db, saleID, billingstate.PENDING_FISCAL)
	inv.JobStatus = fiscalqueue.StatusPending
	inv.PipelineStatus = billingstate.PENDING_FISCAL
	return &inv, nil
}

