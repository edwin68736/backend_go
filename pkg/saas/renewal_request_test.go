package saas

import (
	"testing"

	"tukifac/pkg/database"
)

// Sin comprobante: la solicitud queda pending_review, sin acceso provisional (no hay nada que
// respalde dárselo) y sin tocar el estado de la suscripción existente.
func TestSubmitRenewalRequest_withoutReceipt_noProvisional(t *testing.T) {
	db := setupApprovePaymentDB(t)

	plan := database.SaasPlan{Name: "Pro", Price: 99, BillingCycle: database.SaasCycleMonthly, Active: true}
	db.Create(&plan)
	tenant := database.Tenant{Name: "ACME", Slug: "acme", DBName: "acme", Status: database.TenantStatusSuspended}
	db.Create(&tenant)
	sub := database.SaasSubscription{
		TenantID: tenant.ID, PlanID: plan.ID, BillingCycle: database.SaasCycleMonthly,
		Status: database.SaasSubSuspended,
	}
	db.Create(&sub)

	payment, err := SubmitRenewalRequest(SubmitRenewalRequestInput{TenantID: tenant.ID, PlanID: plan.ID})
	if err != nil {
		t.Fatalf("SubmitRenewalRequest: %v", err)
	}
	if payment.Status != database.SaasPayPendingReview {
		t.Errorf("status = %q, want pending_review", payment.Status)
	}
	if payment.RequestedPlanID == nil || *payment.RequestedPlanID != plan.ID {
		t.Errorf("requested_plan_id = %v, want %d", payment.RequestedPlanID, plan.ID)
	}
	if payment.Amount != plan.Price {
		t.Errorf("amount = %v, want el precio del plan (%v) por defecto", payment.Amount, plan.Price)
	}
	if payment.ProvisionalApplied {
		t.Errorf("no debe otorgar provisional sin comprobante")
	}

	var reloadedSub database.SaasSubscription
	db.First(&reloadedSub, sub.ID)
	if reloadedSub.Status != database.SaasSubSuspended {
		t.Errorf("la suscripción no debe cambiar de estado sin comprobante, quedó %q", reloadedSub.Status)
	}
}

// Con comprobante y una suscripción suspendida: se comporta como un pago normal, otorga
// provisional (mismo mecanismo que SubmitPayment con billing_cycle_id).
func TestSubmitRenewalRequest_withReceipt_grantsProvisional(t *testing.T) {
	db := setupApprovePaymentDB(t)

	plan := database.SaasPlan{Name: "Pro", Price: 99, BillingCycle: database.SaasCycleMonthly, Active: true}
	db.Create(&plan)
	tenant := database.Tenant{Name: "ACME", Slug: "acme", DBName: "acme", Status: database.TenantStatusSuspended}
	db.Create(&tenant)
	sub := database.SaasSubscription{
		TenantID: tenant.ID, PlanID: plan.ID, BillingCycle: database.SaasCycleMonthly,
		Status: database.SaasSubSuspended,
	}
	db.Create(&sub)

	payment, err := SubmitRenewalRequest(SubmitRenewalRequestInput{
		TenantID: tenant.ID, PlanID: plan.ID, ReceiptURL: "/storage/saas/receipts/x.jpg",
	})
	if err != nil {
		t.Fatalf("SubmitRenewalRequest: %v", err)
	}
	if !payment.ProvisionalApplied {
		t.Errorf("debe otorgar provisional con comprobante y suscripción suspendida")
	}

	var reloadedSub database.SaasSubscription
	db.First(&reloadedSub, sub.ID)
	if reloadedSub.Status != database.SaasSubProvisionalActive {
		t.Errorf("status = %q, want provisional_active", reloadedSub.Status)
	}
}

// Plan inexistente o inactivo: rechazado antes de tocar nada.
func TestSubmitRenewalRequest_inactivePlan_rejected(t *testing.T) {
	db := setupApprovePaymentDB(t)

	plan := database.SaasPlan{Name: "Descontinuado", Price: 49, BillingCycle: database.SaasCycleMonthly, Active: true}
	db.Create(&plan)
	db.Model(&plan).Update("active", false)
	tenant := database.Tenant{Name: "ACME", Slug: "acme", DBName: "acme", Status: database.TenantStatusActive}
	db.Create(&tenant)

	if _, err := SubmitRenewalRequest(SubmitRenewalRequestInput{TenantID: tenant.ID, PlanID: plan.ID}); err == nil {
		t.Fatal("esperaba error por plan inactivo")
	}
}

// ApprovePayment sin planID explícito del admin debe usar el plan que el TENANT pidió
// (RequestedPlanID), no quedarse callado con el plan viejo de la suscripción.
func TestApprovePayment_defaultsToRequestedPlan(t *testing.T) {
	db := setupApprovePaymentDB(t)

	oldPlan := database.SaasPlan{Name: "Básico", Price: 49, BillingCycle: database.SaasCycleMonthly, Active: true}
	db.Create(&oldPlan)
	newPlan := database.SaasPlan{Name: "Pro", Price: 99, BillingCycle: database.SaasCycleMonthly, Active: true}
	db.Create(&newPlan)
	tenant := database.Tenant{Name: "ACME", Slug: "acme", DBName: "acme", Status: database.TenantStatusSuspended}
	db.Create(&tenant)
	sub := database.SaasSubscription{
		TenantID: tenant.ID, PlanID: oldPlan.ID, BillingCycle: database.SaasCycleMonthly,
		Status: database.SaasSubSuspended,
	}
	db.Create(&sub)

	payment, err := SubmitRenewalRequest(SubmitRenewalRequestInput{
		TenantID: tenant.ID, PlanID: newPlan.ID, ReceiptURL: "/storage/saas/receipts/x.jpg",
	})
	if err != nil {
		t.Fatalf("SubmitRenewalRequest: %v", err)
	}

	// El admin aprueba sin tocar el dropdown de reasignación de plan (planID=0): debe respetar
	// lo que el tenant pidió, no el plan de la suscripción vieja.
	if err := ApprovePayment(payment.ID, 0, 1, "ok", 1); err != nil {
		t.Fatalf("ApprovePayment: %v", err)
	}

	var active database.SaasSubscription
	if err := db.Where("tenant_id = ? AND status = ?", tenant.ID, database.SaasSubActive).
		Order("created_at desc").First(&active).Error; err != nil {
		t.Fatalf("sin suscripción activa: %v", err)
	}
	if active.PlanID != newPlan.ID {
		t.Errorf("plan_id = %d, want %d (el que pidió el tenant)", active.PlanID, newPlan.ID)
	}
}
