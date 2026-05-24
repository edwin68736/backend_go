package service

import (
	"context"
	"strconv"
	"strings"

	fiscalsvc "tukifac/internal/fiscal/service"
	"tukifac/pkg/billingevents"
	"tukifac/pkg/billingstate"
	"tukifac/pkg/database"
)

// NotifyBillingStatusUpdated emite evento realtime tras actualizar BD tenant.
func NotifyBillingStatusUpdated(tenantID, saleID uint, pipeline, sunatMessage string) {
	if tenantID == 0 || saleID == 0 {
		return
	}
	p := billingstate.NormalizePipeline(pipeline)
	billingevents.PublishStatusUpdated(context.Background(), billingevents.NewStatusUpdated(
		tenantID,
		saleID,
		billingstate.LegacyBillingStatus(p),
		p,
		sunatMessage,
	))
}

// NotifyFromWebhookPayload emite tras webhook fiscal.
func NotifyFromWebhookPayload(tenantID uint, p *fiscalsvc.StatusWebhookPayload) {
	if p == nil {
		return
	}
	pipeline := pipelineFromFiscalStatus(p.Status, p.SunatCode)
	NotifyBillingStatusUpdated(tenantID, p.SaleID, pipeline, p.SunatMessage)
	RunFiscalWebhookSideEffects(tenantID, p.TenantSlug, p.SaleID, pipeline)
}

// RunFiscalWebhookSideEffects sincroniza entidades ligadas (guía, anulación NC).
func RunFiscalWebhookSideEffects(tenantID uint, tenantSlug string, saleID uint, pipeline string) {
	if tenantID == 0 || saleID == 0 {
		return
	}
	var tenant database.Tenant
	if err := database.CentralDB.First(&tenant, tenantID).Error; err != nil {
		return
	}
	if tenant.DBName == "" {
		return
	}
	db, err := database.GetTenantDB(tenant.DBName)
	if err != nil {
		return
	}
	defer database.ReleaseTenantDB(tenant.DBName)
	svc := NewBillingService(db)
	svc.SetCentralTenantID(tenantID)
	if tenant.Slug != "" {
		svc.SetTenantSlug(tenant.Slug)
	} else if tenantSlug != "" {
		svc.SetTenantSlug(tenantSlug)
	}
	svc.PostFiscalAcceptSideEffects(saleID, pipeline)
}

func pipelineFromFiscalStatus(status, sunatCode string) string {
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
		if n, err := strconv.Atoi(code); err == nil && n >= 4000 {
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
		if n, err := strconv.Atoi(code); err == nil && n >= 4000 {
			return billingstate.OBSERVED
		}
		if code == "0" {
			return billingstate.SUNAT_ACCEPTED
		}
		return billingstate.UNKNOWN
	}
}
