package docusage

import "strings"

// DocumentUsageView resumen para Billing Hub y APIs.
type DocumentUsageView struct {
	IsUnlimited         bool   `json:"is_unlimited"`
	PlanLimit           int    `json:"plan_limit"`
	PlanUsed            int    `json:"plan_used"`
	PlanRemaining       int    `json:"plan_remaining"`
	PackageBonus        int    `json:"package_bonus"`
	PackageUsed         int    `json:"package_used"`
	PackageRemaining    int    `json:"package_remaining"`
	TotalAvailable      int    `json:"total_available"`
	TotalConsumed       int    `json:"total_consumed"`
	UsagePercent        int    `json:"usage_percent"`
	WarningLevel        string `json:"warning_level"` // none | low | high | exhausted
	WarningMessage      string `json:"warning_message,omitempty"`
	CanEmit             bool   `json:"can_emit"`
	BillingCycleID      uint   `json:"billing_cycle_id,omitempty"`
	BillingCycleEnd     string `json:"billing_cycle_end,omitempty"`
}

// ReserveInput intento de emisión (cuenta aunque SUNAT falle).
type ReserveInput struct {
	TenantID       uint
	DocumentType   string
	DocumentID     uint
	DocumentNumber string
	Source         string
	MetadataJSON   string
}

// SunatCodeToDocType mapea código SUNAT a tipo de dominio.
func SunatCodeToDocType(code string) string {
	switch strings.TrimSpace(code) {
	case "01":
		return "invoice"
	case "03":
		return "receipt"
	case "07":
		return "credit_note"
	case "08":
		return "debit_note"
	case "09":
		return "guide_remitter"
	case "31":
		return "guide_carrier"
	case "20":
		return "retention"
	case "40":
		return "perception"
	default:
		return "electronic"
	}
}

// IsCountableSunatCode indica si el código SUNAT consume cupo.
func IsCountableSunatCode(code string) bool {
	switch strings.TrimSpace(code) {
	case "00", "":
		return false
	default:
		return true
	}
}
