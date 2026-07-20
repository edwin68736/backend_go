package database

import "time"

// Tipos de documento electrónico (dominio SaaS, no tenant DB).
const (
	ElecDocInvoice       = "invoice"
	ElecDocReceipt       = "receipt"
	ElecDocCreditNote    = "credit_note"
	ElecDocDebitNote     = "debit_note"
	ElecDocGuideRemitter = "guide_remitter"
	ElecDocGuideCarrier  = "guide_carrier"
	ElecDocRetention     = "retention"
	ElecDocPerception    = "perception"
	ElecDocSummary       = "summary"
	ElecDocVoided        = "voided"
	ElecDocReversion     = "reversion"
)

// Estados paquete documentos por tenant.
const (
	SaasDocPkgPendingReview = "pending_review"
	SaasDocPkgApproved      = "approved"
	SaasDocPkgRejected      = "rejected"
	SaasDocPkgExpired       = "expired"
)

// SaasDocumentPackage catálogo de paquetes adicionales (BD central).
type SaasDocumentPackage struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	Name         string    `gorm:"size:120;not null" json:"name"`
	Description  string    `gorm:"size:500" json:"description"`
	DocumentsQty int       `gorm:"not null" json:"documents_qty"`
	Price        float64   `gorm:"not null;default:0" json:"price"`
	Currency     string    `gorm:"size:10;default:'PEN'" json:"currency"`
	IsActive     bool      `gorm:"default:true;index" json:"is_active"`
	SortOrder    int       `gorm:"default:0;index" json:"sort_order"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func (SaasDocumentPackage) TableName() string { return "saas_document_packages" }

// SaasTenantDocumentPackage compra de paquete por tenant (vence con el ciclo).
type SaasTenantDocumentPackage struct {
	ID             uint `gorm:"primaryKey" json:"id"`
	TenantID       uint `gorm:"not null;index" json:"tenant_id"`
	SubscriptionID uint `gorm:"not null;index" json:"subscription_id"`
	BillingCycleID uint `gorm:"not null;index" json:"billing_cycle_id"`
	PackageID      uint `gorm:"not null;index" json:"package_id"`

	DocumentsQty       int `gorm:"not null" json:"documents_qty"`
	UsedDocuments      int `gorm:"not null;default:0" json:"used_documents"`
	RemainingDocuments int `gorm:"not null" json:"remaining_documents"`

	Status string `gorm:"size:30;index;default:'pending_review'" json:"status"`

	PaymentID   *uint   `gorm:"index" json:"payment_id,omitempty"`
	Amount      float64 `gorm:"default:0" json:"amount"`
	ReceiptURL  string  `gorm:"size:500" json:"receipt_url"`
	Reference   string  `gorm:"size:120" json:"reference"`
	SubmittedBy *uint   `json:"submitted_by,omitempty"`

	ApprovedAt     *time.Time `json:"approved_at,omitempty"`
	ApprovedBy     *uint      `json:"approved_by,omitempty"`
	RejectedAt     *time.Time `json:"rejected_at,omitempty"`
	RejectedReason string     `gorm:"size:500" json:"rejected_reason"`

	ExpiresAt time.Time `gorm:"not null;index" json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (SaasTenantDocumentPackage) TableName() string { return "saas_tenant_document_packages" }

// SaasElectronicDocumentUsage registro de consumo (source of truth).
type SaasElectronicDocumentUsage struct {
	ID             uint `gorm:"primaryKey" json:"id"`
	TenantID       uint `gorm:"not null;uniqueIndex:idx_doc_usage_unique,priority:1" json:"tenant_id"`
	SubscriptionID uint `gorm:"not null;index" json:"subscription_id"`
	BillingCycleID uint `gorm:"not null;index" json:"billing_cycle_id"`

	DocumentType   string `gorm:"size:40;not null;uniqueIndex:idx_doc_usage_unique,priority:2" json:"document_type"`
	DocumentID     uint   `gorm:"not null;uniqueIndex:idx_doc_usage_unique,priority:3" json:"document_id"`
	DocumentNumber string `gorm:"size:60" json:"document_number"`

	ConsumedFrom string `gorm:"size:20;not null" json:"consumed_from"` // plan_base | package
	PackageID    *uint  `gorm:"index" json:"package_id,omitempty"`

	Source       string `gorm:"size:20;default:'sync'" json:"source"` // sync | async | retry
	MetadataJSON string `gorm:"type:text" json:"metadata_json"`

	ConsumedAt time.Time `gorm:"not null;index" json:"consumed_at"`
	CreatedAt  time.Time `json:"created_at"`
}

func (SaasElectronicDocumentUsage) TableName() string { return "saas_electronic_document_usages" }
