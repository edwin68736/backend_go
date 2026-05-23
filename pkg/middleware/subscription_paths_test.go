package middleware

import (
	"testing"

	"tukifac/pkg/database"
)

func TestIsSubscriptionHubPath(t *testing.T) {
	if !IsSubscriptionHubPath("/api/subscription/summary") {
		t.Fatal("summary must be hub path")
	}
	if IsSubscriptionHubPath("/api/sales") {
		t.Fatal("sales is not hub")
	}
}

func TestIsSubscriptionPaymentSubmit(t *testing.T) {
	if !IsSubscriptionPaymentSubmit("/api/subscription/payments", "POST") {
		t.Fatal("POST payments must match")
	}
	if IsSubscriptionPaymentSubmit("/api/subscription/payments", "GET") {
		t.Fatal("GET must not match payment submit")
	}
	if IsSubscriptionPaymentSubmit("/api/subscription/summary", "POST") {
		t.Fatal("summary POST is not payment submit")
	}
}

func TestTenantAllowsBillingHubRead(t *testing.T) {
	for _, st := range []string{
		database.TenantStatusActive,
		database.TenantStatusSuspended,
		database.TenantStatusBlocked,
	} {
		if !TenantAllowsBillingHubRead(&database.Tenant{Status: st}) {
			t.Fatalf("hub read allowed for %s", st)
		}
	}
	if TenantAllowsBillingHubRead(&database.Tenant{Status: "pending"}) {
		t.Fatal("pending must not read hub")
	}
}
