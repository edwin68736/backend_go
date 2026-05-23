package saas

import (
	"testing"
	"time"

	"tukifac/pkg/database"
)

func TestShouldShowRenewalBanner(t *testing.T) {
	days := []int{7, 5, 3, 1}
	if ShouldShowRenewalBanner(365, database.SaasSubActive, days) {
		t.Fatal("365 days should not show renewal")
	}
	if !ShouldShowRenewalBanner(5, database.SaasSubActive, days) {
		t.Fatal("5 days should show renewal")
	}
	if ShouldShowRenewalBanner(0, database.SaasSubActive, days) {
		t.Fatal("expired should not show renewal banner")
	}
}

func TestBillingDebtApplies_farDueDate(t *testing.T) {
	cfg := PlatformSettings{ReminderDays: []int{7, 5, 3, 1}}
	sub := TenantSubscriptionView{
		Status:          database.SaasSubActive,
		DaysUntilExpiry: 365,
		PendingAmount:   99,
	}
	due := time.Now().In(lima()).AddDate(1, 0, 0)
	cycle := &database.SaasBillingCycle{
		Status:  database.SaasInvoicePending,
		DueDate: due,
		Amount:  99,
	}
	if billingDebtApplies(cycle, sub, cfg) {
		t.Fatal("pending invoice far in future is not real debt")
	}
}

func TestBillingDebtApplies_withinReminder(t *testing.T) {
	cfg := PlatformSettings{ReminderDays: []int{7, 5, 3, 1}}
	sub := TenantSubscriptionView{Status: database.SaasSubActive, DaysUntilExpiry: 5}
	due := time.Now().In(lima()).AddDate(0, 0, 5)
	cycle := &database.SaasBillingCycle{Status: database.SaasInvoicePending, DueDate: due}
	if !billingDebtApplies(cycle, sub, cfg) {
		t.Fatal("due within reminder window is real debt")
	}
}

func TestMaxReminderDay(t *testing.T) {
	if MaxReminderDay([]int{7, 3, 12}) != 12 {
		t.Fatalf("expected 12 got %d", MaxReminderDay([]int{7, 3, 12}))
	}
}
