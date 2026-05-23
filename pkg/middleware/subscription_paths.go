package middleware

import (
	"strings"

	"tukifac/pkg/database"
)

// Rutas permitidas sin suscripción operativa activa (pagos / resumen).
var subscriptionExemptPrefixes = []string{
	"/api/login",
	"/api/subscription",
	"/health",
	"/metrics",
}

// IsSubscriptionExemptPath indica si la ruta es del módulo de pagos/suscripción o login.
func IsSubscriptionExemptPath(path string) bool {
	path = strings.TrimSpace(path)
	for _, p := range subscriptionExemptPrefixes {
		if strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}

// IsSubscriptionHubPath rutas del Billing Hub (/api/subscription/*).
func IsSubscriptionHubPath(path string) bool {
	return strings.HasPrefix(strings.TrimSpace(path), "/api/subscription")
}

// IsSubscriptionPaymentSubmit POST comprobante SaaS.
func IsSubscriptionPaymentSubmit(path, method string) bool {
	return strings.EqualFold(method, "POST") && strings.HasPrefix(strings.TrimSpace(path), "/api/subscription/payments")
}

// TenantAllowsBillingHubRead suspended/blocked/active pueden leer hub.
func TenantAllowsBillingHubRead(tenant *database.Tenant) bool {
	if tenant == nil {
		return false
	}
	switch tenant.Status {
	case database.TenantStatusActive, database.TenantStatusSuspended, database.TenantStatusBlocked:
		return true
	default:
		return false
	}
}
