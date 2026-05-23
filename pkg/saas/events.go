package saas

// Tipos de evento de auditoría (saas_subscription_events).
const (
	EventProvisionalGranted = "PROVISIONAL_GRANTED"
	EventPaymentRejected    = "PAYMENT_REJECTED"
	EventTenantBlocked      = "TENANT_BLOCKED"
	EventTenantUnblocked    = "TENANT_UNBLOCKED"
	EventPaymentApproved    = "PAYMENT_APPROVED"
	EventSuspended          = "SUSPENDED"
	EventReactivated        = "REACTIVATED"
)
