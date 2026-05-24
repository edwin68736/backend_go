package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"tukifac/pkg/billingstate"
	"tukifac/pkg/database"

	"gorm.io/gorm"
)

// StatusWebhookPayload POST /api/internal/fiscal/status desde facturador_lycet.
type StatusWebhookPayload struct {
	TenantID          uint   `json:"tenant_id"`
	TenantSlug        string `json:"tenant_slug"`
	SaleID            uint   `json:"sale_id"`
	DocumentUUID      string `json:"document_uuid"`
	FiscalFingerprint string `json:"fiscal_fingerprint"`
	EventID           string `json:"event_id"`
	Status            string `json:"status"`
	Provider          string `json:"provider"`
	SendMode          string `json:"send_mode"`
	XMLURL            string `json:"xml_url"`
	XMLSignedURL      string `json:"xml_signed_url"`
	UnsignedXMLURL    string `json:"unsigned_xml_url"`
	CDRURL            string `json:"cdr_url"`
	PDFURL            string `json:"pdf_url"`
	Hash              string `json:"hash"`
	Ticket            string `json:"ticket"`
	SunatCode         string `json:"sunat_code"`
	SunatMessage      string `json:"sunat_message"`
	SunatCDRNotes     json.RawMessage `json:"sunat_cdr_notes"`
	RetryCount        int    `json:"retry_count"`
}

// SyncService aplica estado fiscal del facturador en la BD tenant del ERP.
type SyncService struct{}

func NewSyncService() *SyncService { return &SyncService{} }

func (s *SyncService) ApplyStatus(db *gorm.DB, p *StatusWebhookPayload) error {
	if p == nil || p.SaleID == 0 {
		return errors.New("sale_id requerido")
	}

	pipeline := mapFiscalStatusToPipeline(p.Status, p.SunatCode)

	var inv database.TenantInvoice
	err := db.Where("sale_id = ?", p.SaleID).First(&inv).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		inv = database.TenantInvoice{
			SaleID:         p.SaleID,
			ExternalID:     p.DocumentUUID,
			PipelineStatus: pipeline,
			JobStatus:      billingstate.JobStatusFromPipeline(pipeline),
			SunatStatus:    billingstate.LegacySunatStatus(pipeline),
		}
		if err := db.Create(&inv).Error; err != nil {
			if reloadErr := db.Where("sale_id = ?", p.SaleID).First(&inv).Error; reloadErr != nil {
				return fmt.Errorf("tenant_invoices upsert sale_id=%d: %w", p.SaleID, err)
			}
		}
	} else if err != nil {
		return err
	}

	now := time.Now()
	currentPipeline := billingstate.NormalizePipeline(inv.PipelineStatus)
	if !billingstate.CanTransition(currentPipeline, pipeline) {
		pipeline = currentPipeline
	}

	inv.ExternalID = p.DocumentUUID
	inv.PipelineStatus = pipeline
	inv.JobStatus = billingstate.JobStatusFromPipeline(pipeline)
	inv.SunatStatus = billingstate.LegacySunatStatus(pipeline)
	if p.SunatMessage != "" {
		inv.SunatMessage = p.SunatMessage
	}
	if p.SunatCode != "" {
		inv.SunatCDRCode = p.SunatCode
	}
	if len(p.SunatCDRNotes) > 0 && string(p.SunatCDRNotes) != "null" {
		inv.SunatCDRNotes = string(p.SunatCDRNotes)
	}
	if p.Hash != "" {
		inv.SunatHash = p.Hash
	}
	if p.XMLSignedURL != "" {
		inv.XMLURL = p.XMLSignedURL
	} else if p.XMLURL != "" {
		inv.XMLURL = p.XMLURL
	}
	if p.CDRURL != "" {
		inv.CDRURL = p.CDRURL
	}
	if p.PDFURL != "" {
		inv.PDFURL = p.PDFURL
	}
	if p.RetryCount > 0 {
		inv.RetryCount = p.RetryCount
	}
	if pipeline != billingstate.DRAFT && pipeline != billingstate.PENDING_QUEUE {
		if inv.SentAt == nil {
			inv.SentAt = &now
		}
		inv.ResponseAt = &now
	}

	if err := db.Save(&inv).Error; err != nil {
		return err
	}
	return billingstate.SyncSaleBillingStatus(db, p.SaleID, pipeline)
}

func mapFiscalStatusToPipeline(status, sunatCode string) string {
	status = strings.ToLower(strings.TrimSpace(status))
	code := strings.TrimSpace(sunatCode)

	if code != "" {
		if n, err := strconv.Atoi(code); err == nil {
			if n >= 4000 {
				return billingstate.OBSERVED
			}
			if n != 0 && status != "accepted" && status != "observed" {
				return billingstate.SUNAT_REJECTED
			}
		}
	}

	switch status {
	case "accepted":
		if isSunatObservedCode(code) {
			return billingstate.OBSERVED
		}
		return billingstate.SUNAT_ACCEPTED
	case "observed":
		return billingstate.OBSERVED
	case "rejected":
		return billingstate.SUNAT_REJECTED
	case "queued", "pending":
		return billingstate.PENDING_FISCAL
	case "sending", "sent":
		return billingstate.SENDING_TO_SUNAT
	case "retrying":
		return billingstate.RETRYING
	case "error":
		if code != "" && code != "0" {
			return billingstate.SUNAT_REJECTED
		}
		return billingstate.FAILED
	default:
		if isSunatObservedCode(code) {
			return billingstate.OBSERVED
		}
		if code == "0" {
			return billingstate.SUNAT_ACCEPTED
		}
		return billingstate.UNKNOWN
	}
}

func isSunatObservedCode(code string) bool {
	code = strings.TrimSpace(code)
	if code == "" {
		return false
	}
	n, err := strconv.Atoi(code)
	if err != nil {
		return false
	}
	return n >= 4000
}
