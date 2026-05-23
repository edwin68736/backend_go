package saas

import (
	"errors"
	"testing"

	"tukifac/pkg/database"
)

func TestCanTenantSubmitPayment_blocked(t *testing.T) {
	tenant := &database.Tenant{Status: database.TenantStatusBlocked, StrikeCount: 2, PaymentBlocked: true}
	err := CanTenantSubmitPayment(tenant)
	if !errors.Is(err, ErrPaymentBlocked) {
		t.Fatalf("expected ErrPaymentBlocked, got %v", err)
	}
}

func TestCanTenantSubmitPayment_ok(t *testing.T) {
	tenant := &database.Tenant{Status: database.TenantStatusSuspended, StrikeCount: 1}
	if err := CanTenantSubmitPayment(tenant); err != nil {
		t.Fatalf("suspended with 1 strike should submit: %v", err)
	}
}

func TestChargeReconnectionFee_onlyWhenSuspended(t *testing.T) {
	tenant := &database.Tenant{Status: database.TenantStatusActive}
	subGrace := &database.SaasSubscription{Status: database.SaasSubGracePeriod}
	if ChargeReconnectionFee(tenant, subGrace) {
		t.Fatal("grace must not charge reconnection")
	}
	subOverdue := &database.SaasSubscription{Status: database.SaasSubOverdue}
	if ChargeReconnectionFee(tenant, subOverdue) {
		t.Fatal("overdue without suspend must not charge reconnection")
	}
	tenantSuspended := &database.Tenant{Status: database.TenantStatusSuspended}
	if !ChargeReconnectionFee(tenantSuspended, subGrace) {
		t.Fatal("suspended tenant must charge reconnection")
	}
}

func TestBillingCycleAmountDue(t *testing.T) {
	cycle := &database.SaasBillingCycle{Amount: 49, ReconnectionFee: 50}
	tenant := &database.Tenant{Status: database.TenantStatusActive}
	sub := &database.SaasSubscription{Status: database.SaasSubActive}
	if got := BillingCycleAmountDue(cycle, tenant, sub); got != 49 {
		t.Fatalf("active want 49, got %v", got)
	}
	tenant.Status = database.TenantStatusSuspended
	if got := BillingCycleAmountDue(cycle, tenant, sub); got != 99 {
		t.Fatalf("suspended want 99, got %v", got)
	}
}

func TestCanTenantSubmitPayment_strike2(t *testing.T) {
	tenant := &database.Tenant{Status: "active", StrikeCount: 2}
	if err := CanTenantSubmitPayment(tenant); err == nil {
		t.Fatal("strike 2 must block uploads")
	}
}
