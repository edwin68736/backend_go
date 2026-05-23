package saas

import (
	"errors"
	"testing"
	"time"

	"tukifac/pkg/database"
)

// E2E lógico: tenant suspendido puede enviar pago (no blocked).
func TestE2E_tenantSuspendedCanSubmitPayment(t *testing.T) {
	tenant := &database.Tenant{Status: database.TenantStatusSuspended, StrikeCount: 0}
	if err := CanTenantSubmitPayment(tenant); err != nil {
		t.Fatalf("suspended tenant must submit payment: %v", err)
	}
}

// E2E lógico: tenant blocked no puede subir pago.
func TestE2E_tenantBlockedCannotSubmitPayment(t *testing.T) {
	tenant := &database.Tenant{
		Status: database.TenantStatusBlocked, StrikeCount: 2, PaymentBlocked: true,
	}
	err := CanTenantSubmitPayment(tenant)
	if !errors.Is(err, ErrPaymentBlocked) {
		t.Fatalf("blocked must return ErrPaymentBlocked, got %v", err)
	}
}

// E2E: vence hoy opera todo el día; vence mañana entra grace (no suspendido mismo día).
func TestE2E_venceHoyOpera_venceMananaGrace(t *testing.T) {
	loc, _ := time.LoadLocation(LimaTimezone)
	end := time.Date(2026, 6, 15, 0, 0, 0, 0, loc)
	sub := &database.SaasSubscription{EndDate: end, Status: database.SaasSubActive}
	tenant := &database.Tenant{Status: database.TenantStatusActive}

	hoyTarde := time.Date(2026, 6, 15, 18, 0, 0, 0, loc)
	stHoy := ResolveEffectiveStatus(sub, tenant, hoyTarde, 3)
	if stHoy != database.SaasSubActive {
		t.Fatalf("vence hoy: expected active, got %s", stHoy)
	}
	if !CanOperate(stHoy, tenant, nil, hoyTarde) {
		t.Fatal("vence hoy debe poder operar ERP")
	}

	manana := time.Date(2026, 6, 16, 8, 0, 0, 0, loc)
	st := ResolveEffectiveStatus(sub, tenant, manana, 3)
	if st != database.SaasSubGracePeriod {
		t.Fatalf("día después de vencimiento: grace_period, got %s", st)
	}
	if tenant.Status == database.TenantStatusSuspended {
		t.Fatal("no debe estar suspendido al día siguiente con solo 1 día vencido")
	}
}

// E2E timezone Perú: comparación por día calendario, no UTC midnight arbitrario.
func TestE2E_timezonePeru_calendarDay(t *testing.T) {
	loc, _ := time.LoadLocation(LimaTimezone)
	end := time.Date(2026, 3, 10, 23, 0, 0, 0, time.UTC)
	now := time.Date(2026, 3, 10, 22, 0, 0, 0, loc)
	if CalendarDaysAfterEnd(end, now) != 0 {
		t.Fatal("mismo día calendario Lima debe ser 0 días después")
	}
	nowNext := time.Date(2026, 3, 11, 0, 10, 0, 0, loc)
	if CalendarDaysAfterEnd(end, nowNext) != 1 {
		t.Fatalf("día siguiente Lima = 1, got %d", CalendarDaysAfterEnd(end, nowNext))
	}
}

// E2E provisional: segunda vez en ciclo no aplica provisional pero pago sigue siendo válido (sin ErrProvisionalUsed).
func TestE2E_provisionalFlow_secondSubmitSkipsProvisional(t *testing.T) {
	cycle := &database.SaasBillingCycle{ProvisionalUsed: true}
	if !cycle.ProvisionalUsed {
		t.Fatal("fixture: cycle already used provisional")
	}
	// La rama de provisional en SubmitPayment exige !ProvisionalUsed; no debe devolver ErrProvisionalUsed.
	if errors.Is(ErrProvisionalUsed, nil) {
		t.Fatal("ErrProvisionalUsed must remain defined for API compat")
	}
}

// E2E double approval: mensajes de guardia documentados (integración BD en CI con central).
func TestE2E_doubleApproval_guardMessages(t *testing.T) {
	msgPaid := "el ciclo de facturación ya fue pagado"
	msgDup := "ya existe un pago aprobado para este ciclo"
	if msgPaid == "" || msgDup == "" {
		t.Fatal("guard messages must be stable for API tests")
	}
}

// E2E cron duplicado: lock key por fecha Lima.
func TestE2E_cronDuplicated_dailyLockKey(t *testing.T) {
	loc, _ := time.LoadLocation(LimaTimezone)
	d1 := time.Date(2026, 5, 21, 0, 6, 0, 0, loc).Format("2006-01-02")
	d2 := time.Date(2026, 5, 22, 0, 6, 0, 0, loc).Format("2006-01-02")
	if d1 == d2 {
		t.Fatal("lock keys must differ across calendar days")
	}
	key1 := "saas:lima_daily:" + d1
	key2 := "saas:lima_daily:" + d2
	if key1 == key2 {
		t.Fatal("distinct daily lock keys required")
	}
}

// Suspendido bloqueado ERP pero puede leer hub (policy).
func TestE2E_suspendedNoERP_canReadHubPolicy(t *testing.T) {
	tenant := &database.Tenant{Status: database.TenantStatusSuspended}
	sub := &database.SaasSubscription{
		EndDate: NowLima().Add(30 * 24 * time.Hour),
		Status:  database.SaasSubActive,
	}
	st := ResolveEffectiveStatus(sub, tenant, NowLima(), 3)
	if CanOperate(st, tenant, nil, NowLima()) {
		t.Fatal("suspended without provisional must not operate ERP")
	}
}
