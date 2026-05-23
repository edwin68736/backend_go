// Package billingstate define la máquina de estados de facturación electrónica (conservadora).
// Regla de oro: sin evidencia real de SUNAT, nunca marcar éxito.
package billingstate

import (
	"strconv"
	"strings"

	"tukifac/pkg/database"
)

// Estados del pipeline (orden lógico; no saltar sin transición válida).
const (
	DRAFT                 = "DRAFT"
	PENDING_QUEUE         = "PENDING_QUEUE"
	PROCESSING            = "PROCESSING"
	SENDING_TO_FACTURADOR = "SENDING_TO_FACTURADOR"
	FACTURADOR_RECEIVED   = "FACTURADOR_RECEIVED"
	SENDING_TO_SUNAT      = "SENDING_TO_SUNAT"
	SUNAT_ACCEPTED        = "SUNAT_ACCEPTED"
	SUNAT_REJECTED        = "SUNAT_REJECTED"
	OBSERVED              = "OBSERVED"
	FAILED                = "FAILED"
	RETRYING              = "RETRYING"
	DEAD_LETTER           = "DEAD_LETTER"
	UNKNOWN               = "UNKNOWN"
)

// orderedStages define el orden para validar que no se salte hacia adelante sin pasar por intermedios clave.
var stageRank = map[string]int{
	DRAFT:                 0,
	PENDING_QUEUE:         10,
	PROCESSING:            20,
	RETRYING:              25,
	SENDING_TO_FACTURADOR: 30,
	FACTURADOR_RECEIVED:   40,
	SENDING_TO_SUNAT:      50,
	OBSERVED:              60,
	SUNAT_ACCEPTED:        70,
	SUNAT_REJECTED:        70,
	FAILED:                80,
	DEAD_LETTER:           90,
	UNKNOWN:               5,
}

// CanTransition permite avanzar en el pipeline o ir a estados terminales/fallo desde cualquier etapa activa.
func CanTransition(from, to string) bool {
	from = NormalizePipeline(from)
	to = NormalizePipeline(to)
	if from == "" || from == to {
		return true
	}
	if isTerminal(to) {
		return true
	}
	if isTerminal(from) {
		// Solo re-emisión explícita desde terminal (re-queue) vuelve a cola.
		return to == PENDING_QUEUE || to == PROCESSING || to == RETRYING
	}
	rf, okF := stageRank[from]
	rt, okT := stageRank[to]
	if !okF || !okT {
		return false
	}
	return rt >= rf
}

func isTerminal(s string) bool {
	switch s {
	case SUNAT_ACCEPTED, SUNAT_REJECTED, OBSERVED, FAILED, DEAD_LETTER:
		return true
	default:
		return false
	}
}

// NormalizePipeline mapea valores legacy/vacíos.
func NormalizePipeline(s string) string {
	s = strings.ToUpper(strings.TrimSpace(s))
	if s == "" {
		return DRAFT
	}
	switch s {
	case "PENDING", "PENDING_QUEUE":
		return PENDING_QUEUE
	case "PROCESSING":
		return PROCESSING
	case "RETRYING":
		return RETRYING
	case "SENT", "SENDING_TO_FACTURADOR":
		return SENDING_TO_FACTURADOR
	case "FACTURADOR_RECEIVED":
		return FACTURADOR_RECEIVED
	case "SENDING_TO_SUNAT":
		return SENDING_TO_SUNAT
	case "ACCEPTED", "SUNAT_ACCEPTED":
		return SUNAT_ACCEPTED
	case "REJECTED", "SUNAT_REJECTED":
		return SUNAT_REJECTED
	case "OBSERVED":
		return OBSERVED
	case "FAILED", "ERROR":
		return FAILED
	case "DEAD_LETTER":
		return DEAD_LETTER
	case "UNKNOWN":
		return UNKNOWN
	default:
		return s
	}
}

// LegacyBillingStatus mapea pipeline → billing_status de venta (UI/listados).
func LegacyBillingStatus(pipeline string) string {
	switch NormalizePipeline(pipeline) {
	case SUNAT_ACCEPTED, OBSERVED:
		return "accepted"
	case SUNAT_REJECTED:
		return "rejected"
	case FAILED, DEAD_LETTER:
		return "error"
	default:
		return "pending"
	}
}

// LegacySunatStatus mapea pipeline → sunat_status en tenant_invoices.
func LegacySunatStatus(pipeline string) string {
	switch NormalizePipeline(pipeline) {
	case SUNAT_ACCEPTED:
		return "accepted"
	case OBSERVED:
		return "observed"
	case SUNAT_REJECTED:
		return "rejected"
	case FAILED, DEAD_LETTER:
		return "error"
	case SENDING_TO_FACTURADOR, FACTURADOR_RECEIVED, SENDING_TO_SUNAT, PROCESSING, PENDING_QUEUE, RETRYING:
		return "pending"
	default:
		return "pending"
	}
}

// JobStatusFromPipeline mapea a job_status técnico de cola.
func JobStatusFromPipeline(pipeline string) string {
	switch NormalizePipeline(pipeline) {
	case SUNAT_ACCEPTED, OBSERVED:
		return "sent"
	case SUNAT_REJECTED, FAILED:
		return "failed"
	case DEAD_LETTER:
		return "dead_letter"
	case RETRYING:
		return "retrying"
	case PROCESSING, SENDING_TO_FACTURADOR, FACTURADOR_RECEIVED, SENDING_TO_SUNAT:
		return "processing"
	default:
		return "pending"
	}
}

// DocumentIdempotencyKey clave por comprobante (tenant + tipo + serie + correlativo).
func DocumentIdempotencyKey(tenantDB, tipoDoc, serie, correlativo string) string {
	return strings.TrimSpace(tenantDB) + ":" + strings.TrimSpace(tipoDoc) + ":" +
		strings.TrimSpace(serie) + "-" + strings.TrimSpace(correlativo)
}

// SaleIdempotencyKey clave por venta (cola async existente).
func SaleIdempotencyKey(tenantDB string, saleID uint) string {
	return strings.TrimSpace(tenantDB) + ":" + strconv.FormatUint(uint64(saleID), 10)
}

// HasFinalSunatOutcome indica si ya hay resultado SUNAT definitivo (no reenviar).
func HasFinalSunatOutcome(inv *database.TenantInvoice) bool {
	if inv == nil {
		return false
	}
	p := NormalizePipeline(inv.PipelineStatus)
	if p == SUNAT_ACCEPTED || p == SUNAT_REJECTED || p == OBSERVED {
		return true
	}
	return HasAcceptanceEvidence(inv) || (inv.SunatStatus == "rejected" && inv.SentAt != nil)
}
