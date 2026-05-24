package billingevents

// EventStatusUpdated nombre del evento SSE / Redis.
const EventStatusUpdated = "billing.status.updated"

// StatusUpdatedPayload evento push hacia frontends tenant (una fila, sin reload).
type StatusUpdatedPayload struct {
	Event          string `json:"event"`
	TenantID       uint   `json:"tenant_id"`
	SaleID         uint   `json:"sale_id"`
	Status         string `json:"status"` // billing_status: pending, accepted, rejected, error
	PipelineStatus string `json:"pipeline_status,omitempty"`
	SunatMessage   string `json:"sunat_message,omitempty"`
}

func NewStatusUpdated(tenantID, saleID uint, billingStatus, pipeline, sunatMessage string) StatusUpdatedPayload {
	return StatusUpdatedPayload{
		Event:          EventStatusUpdated,
		TenantID:       tenantID,
		SaleID:         saleID,
		Status:         billingStatus,
		PipelineStatus: pipeline,
		SunatMessage:   sunatMessage,
	}
}
