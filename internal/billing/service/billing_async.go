package service

import (
	"errors"
	"fmt"
	"time"

	"tukifac/pkg/billingqueue"
	"tukifac/pkg/billingstate"
	"tukifac/pkg/database"
	"tukifac/pkg/saas/docusage"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ProcessSendToSUNAT ejecuta emisión (worker de cola). Solo éxito con evidencia SUNAT real.
func (s *BillingService) ProcessSendToSUNAT(saleID uint, tenantID uint) (*database.TenantInvoice, error) {
	if tenantID == 0 {
		return nil, docusage.ErrTenantRequired
	}
	if err := s.reserveSaleDocument(tenantID, saleID); err != nil {
		return nil, err
	}
	var existing database.TenantInvoice
	if err := s.db.Where("sale_id = ?", saleID).First(&existing).Error; err == nil {
		if billingstate.HasFinalSunatOutcome(&existing) {
			if billingstate.HasAcceptanceEvidence(&existing) {
				return &existing, nil
			}
		}
	}

	_ = billingstate.TransitionPipeline(s.db, saleID, billingstate.PROCESSING)

	inv, err := s.SendToSUNAT(saleID)
	if err != nil {
		if inv != nil {
			p := billingstate.NormalizePipeline(inv.PipelineStatus)
			if p == "" || p == billingstate.DRAFT {
				p = billingstate.FAILED
			}
			patch := billingstate.BuildPatch(p, inv, err.Error())
			billingstate.ApplyToInvoice(inv, patch)
			_ = s.db.Save(inv).Error
			_ = billingstate.SyncSaleBillingStatus(s.db, saleID, patch.PipelineStatus)
		}
		return inv, err
	}
	if inv == nil {
		return nil, errors.New("sin respuesta de facturación")
	}

	if billingstate.HasAcceptanceEvidence(inv) {
		patch := billingstate.BuildPatch(billingstate.SUNAT_ACCEPTED, inv, inv.SunatMessage)
		billingstate.ApplyToInvoice(inv, patch)
		_ = s.db.Model(inv).Updates(map[string]interface{}{
			"pipeline_status": patch.PipelineStatus,
			"job_status":      patch.JobStatus,
			"job_last_error":  "",
		}).Error
		_ = billingstate.SyncSaleBillingStatus(s.db, saleID, billingstate.SUNAT_ACCEPTED)
		return inv, nil
	}

	p := billingstate.NormalizePipeline(inv.PipelineStatus)
	if p == billingstate.SUNAT_REJECTED {
		_ = s.db.Model(inv).Updates(map[string]interface{}{
			"job_status":     billingqueue.StatusFailed,
			"job_last_error": inv.SunatMessage,
		}).Error
		_ = billingstate.SyncSaleBillingStatus(s.db, saleID, p)
		msg := inv.SunatMessage
		if msg == "" {
			msg = "comprobante rechazado por SUNAT"
		}
		return inv, fmt.Errorf("%s", msg)
	}

	if p == billingstate.SUNAT_ACCEPTED || p == billingstate.OBSERVED {
		return inv, nil
	}

	msg := inv.SunatMessage
	if msg == "" {
		msg = "sin confirmación SUNAT (estado " + p + ")"
	}
	_ = s.db.Model(inv).Updates(map[string]interface{}{
		"pipeline_status": billingstate.UNKNOWN,
		"job_status":      billingqueue.StatusFailed,
		"job_last_error":  msg,
	}).Error
	_ = billingstate.SyncSaleBillingStatus(s.db, saleID, billingstate.UNKNOWN)
	return inv, fmt.Errorf("%s", msg)
}

// EnqueueSendToSUNAT encola emisión y responde rápido (idempotente por sale_id).
func (s *BillingService) EnqueueSendToSUNAT(saleID uint, tenantID uint, tenantSlug, tenantDB string) (*database.TenantInvoice, error) {
	if tenantID == 0 {
		return nil, docusage.ErrTenantRequired
	}
	if err := s.reserveSaleDocument(tenantID, saleID); err != nil {
		return nil, err
	}
	if !billingqueue.Enabled() {
		return s.ProcessSendToSUNAT(saleID, tenantID)
	}

	idemKey := billingstate.SaleIdempotencyKey(tenantDB, saleID)

	claimed, err := billingqueue.TryClaimEnqueue(idemKey)
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
			billingqueue.ReleaseClaim(idemKey)
		}
	}()

	var inv database.TenantInvoice
	err = s.db.Transaction(func(tx *gorm.DB) error {
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("sale_id = ?", saleID).First(&inv).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			inv = database.TenantInvoice{
				SaleID:         saleID,
				JobStatus:      billingqueue.StatusPending,
				PipelineStatus: billingstate.PENDING_QUEUE,
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
		switch inv.JobStatus {
		case billingqueue.StatusPending, billingqueue.StatusProcessing, billingqueue.StatusRetrying:
			return nil
		case billingqueue.StatusSent:
			if billingstate.HasAcceptanceEvidence(&inv) {
				return errAlreadyAccepted
			}
		}
		return tx.Model(&inv).Updates(map[string]interface{}{
			"job_status":       billingqueue.StatusPending,
			"pipeline_status":  billingstate.PENDING_QUEUE,
			"idempotency_key":  idemKey,
			"job_last_error":   "",
		}).Error
	})
	if errors.Is(err, errAlreadyAccepted) {
		return &inv, errAlreadyAccepted
	}
	if err != nil {
		return nil, err
	}

	_ = s.db.Model(&database.TenantSale{}).Where("id = ?", saleID).
		Update("billing_status", "pending").Error

	job := billingqueue.Job{
		TenantDB:       tenantDB,
		TenantID:       tenantID,
		TenantSlug:     tenantSlug,
		SaleID:         saleID,
		IdempotencyKey: idemKey,
	}
	if err := billingqueue.Enqueue(job); err != nil {
		_ = s.db.Model(&inv).Updates(map[string]interface{}{
			"job_status":      billingqueue.StatusFailed,
			"pipeline_status": billingstate.FAILED,
			"job_last_error":  err.Error(),
		}).Error
		return &inv, err
	}
	enqueued = true
	inv.JobStatus = billingqueue.StatusPending
	inv.PipelineStatus = billingstate.PENDING_QUEUE
	return &inv, nil
}

var errAlreadyAccepted = errors.New("comprobante ya enviado y aceptado por SUNAT")

// GetBillingJobStatus devuelve estado del job para polling del frontend.
func (s *BillingService) GetBillingJobStatus(saleID uint) (*database.TenantInvoice, error) {
	var inv database.TenantInvoice
	if err := s.db.Where("sale_id = ?", saleID).First(&inv).Error; err != nil {
		return nil, err
	}
	return &inv, nil
}

// WaitForJob opcional: bloquea hasta timeout (dev/tests).
func (s *BillingService) WaitForJob(saleID uint, timeout time.Duration) (*database.TenantInvoice, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		inv, err := s.GetBillingJobStatus(saleID)
		if err != nil {
			return nil, err
		}
		switch inv.JobStatus {
		case billingqueue.StatusSent, billingqueue.StatusFailed, billingqueue.StatusDeadLetter:
			return inv, nil
		}
		p := billingstate.NormalizePipeline(inv.PipelineStatus)
		if p == billingstate.SUNAT_ACCEPTED || p == billingstate.SUNAT_REJECTED || p == billingstate.FAILED {
			return inv, nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return s.GetBillingJobStatus(saleID)
}

