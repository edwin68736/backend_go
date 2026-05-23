package saas

import (
	"fmt"
	"time"

	"tukifac/pkg/database"
)

// BillingContextView reglas UX para header, banner y colores (según reminder_days del panel central).
type BillingContextView struct {
	ReminderDays        []int   `json:"reminder_days"`
	MaxReminderDays     int     `json:"max_reminder_days"`
	UrgencyTier         string  `json:"urgency_tier"` // normal, reminder, grace, overdue, suspended, blocked, provisional, review
	PlanAmount          float64 `json:"plan_amount"`
	CurrentPaymentLabel string  `json:"current_payment_label"`
	CurrentPaymentTone  string  `json:"current_payment_tone"` // success, warning, danger, info, muted
	HasRealDebt         bool    `json:"has_real_debt"`
	DisplayDebtAmount   float64 `json:"display_debt_amount,omitempty"`
	ShowStatusBanner    bool    `json:"show_status_banner"`
	StatusBannerVariant string  `json:"status_banner_variant,omitempty"`
	StatusBannerMessage string  `json:"status_banner_message,omitempty"`
}

// MaxReminderDay mayor día configurado en recordatorios (0 si vacío).
func MaxReminderDay(days []int) int {
	max := 0
	for _, d := range days {
		if d > max {
			max = d
		}
	}
	return max
}

// BuildBillingContext calcula urgencia, pago del ciclo actual y si hay deuda real.
func BuildBillingContext(sub TenantSubscriptionView, cfg PlatformSettings, tenantID uint) BillingContextView {
	maxRem := MaxReminderDay(cfg.ReminderDays)
	out := BillingContextView{
		ReminderDays:    append([]int(nil), cfg.ReminderDays...),
		MaxReminderDays: maxRem,
		UrgencyTier:     "normal",
	}

	var plan database.SaasPlan
	if sub.PlanID > 0 {
		if database.CentralDB.First(&plan, sub.PlanID).Error == nil {
			out.PlanAmount = plan.Price
		}
	}

	var currentCycle *database.SaasBillingCycle
	if sub.PendingInvoiceID > 0 {
		var c database.SaasBillingCycle
		if database.CentralDB.First(&c, sub.PendingInvoiceID).Error == nil {
			currentCycle = &c
			if out.PlanAmount == 0 {
				out.PlanAmount = c.Amount
			}
		}
	}

	var tenant database.Tenant
	_ = database.CentralDB.First(&tenant, tenantID).Error

	out.HasRealDebt = billingDebtApplies(currentCycle, sub, cfg)
	if out.HasRealDebt && currentCycle != nil {
		out.DisplayDebtAmount = BillingCycleAmountDue(currentCycle, &tenant, nil)
		if sub.SubscriptionID > 0 {
			var s database.SaasSubscription
			if database.CentralDB.First(&s, sub.SubscriptionID).Error == nil {
				out.DisplayDebtAmount = BillingCycleAmountDue(currentCycle, &tenant, &s)
			}
		}
	}

	out.UrgencyTier, out.CurrentPaymentLabel, out.CurrentPaymentTone = resolvePaymentUX(sub, cfg, out.HasRealDebt, out.DisplayDebtAmount)
	out.ShowStatusBanner, out.StatusBannerVariant, out.StatusBannerMessage = resolveStatusBanner(sub, out)

	return out
}

func billingDebtApplies(cycle *database.SaasBillingCycle, sub TenantSubscriptionView, cfg PlatformSettings) bool {
	if sub.IsBlocked {
		return sub.PendingAmount > 0
	}
	if sub.IsSuspended || sub.InGracePeriod || sub.IsOverdue {
		return sub.PendingAmount > 0
	}
	if cycle == nil {
		return false
	}
	if cycle.Status == database.SaasInvoiceOverdue {
		return true
	}
	if cycle.Status != database.SaasInvoicePending {
		return false
	}
	now := NowLima()
	due := cycle.DueDate.In(lima())
	daysUntilDue := calendarDaysBetween(now, due)
	if daysUntilDue <= 0 {
		return true
	}
	maxRem := MaxReminderDay(cfg.ReminderDays)
	if maxRem > 0 && daysUntilDue <= maxRem {
		return true
	}
	return false
}

func calendarDaysBetween(from, to time.Time) int {
	a := from.In(lima())
	b := to.In(lima())
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	t1 := time.Date(ay, am, ad, 0, 0, 0, 0, lima())
	t2 := time.Date(by, bm, bd, 0, 0, 0, 0, lima())
	return int(t2.Sub(t1).Hours() / 24)
}

func resolvePaymentUX(sub TenantSubscriptionView, cfg PlatformSettings, hasDebt bool, debtAmt float64) (tier, label, tone string) {
	tier = "normal"
	if sub.IsBlocked {
		tier = "blocked"
		return tier, "Cuenta bloqueada", "muted"
	}
	if sub.Status == database.SaasSubProvisionalActive && sub.ProvisionalHoursLeft > 0 {
		tier = "provisional"
		return tier, "Acceso provisional", "info"
	}
	if sub.HasPendingPaymentReview {
		tier = "review"
		return tier, "En revisión", "info"
	}
	if sub.IsSuspended || (!sub.CanOperate && sub.ShowSuspendedBanner) {
		tier = "suspended"
		if hasDebt {
			return tier, fmt.Sprintf("Pendiente S/ %.2f", debtAmt), "danger"
		}
		return tier, "Suspendido", "danger"
	}
	if sub.InGracePeriod {
		tier = "grace"
		if hasDebt {
			return tier, fmt.Sprintf("Pendiente S/ %.2f", debtAmt), "warning"
		}
		return tier, "Periodo de gracia", "warning"
	}
	if sub.IsOverdue {
		tier = "overdue"
		if hasDebt {
			return tier, fmt.Sprintf("Pendiente S/ %.2f", debtAmt), "danger"
		}
		return tier, "Vencido", "danger"
	}
	maxRem := MaxReminderDay(cfg.ReminderDays)
	if sub.DaysUntilExpiry > 0 && maxRem > 0 && sub.DaysUntilExpiry <= maxRem && sub.Status == database.SaasSubActive {
		tier = "reminder"
		if hasDebt {
			return tier, fmt.Sprintf("Pendiente S/ %.2f", debtAmt), "warning"
		}
		return tier, "Renovación próxima", "warning"
	}
	if hasDebt {
		return "reminder", fmt.Sprintf("Pendiente S/ %.2f", debtAmt), "warning"
	}
	return tier, "Pagado", "success"
}

func resolveStatusBanner(sub TenantSubscriptionView, ctx BillingContextView) (show bool, variant, msg string) {
	switch ctx.UrgencyTier {
	case "blocked":
		return true, "danger", sub.SupportMessage
	case "provisional":
		return true, "info", fmt.Sprintf("Acceso provisional activo (%d h restantes aprox.)", sub.ProvisionalHoursLeft)
	case "review":
		return true, "info", "Tienes un pago en validación. Te avisaremos cuando sea aprobado."
	case "suspended":
		if ctx.HasRealDebt {
			return true, "danger", fmt.Sprintf("Tu cuenta está suspendida. Regulariza S/ %.2f para reactivar.", ctx.DisplayDebtAmount)
		}
		return true, "danger", "Tu cuenta está suspendida. Contacta a soporte o envía tu comprobante."
	case "grace":
		return true, "warning", "Periodo de gracia: realiza tu pago para evitar la suspensión."
	case "overdue":
		return true, "warning", "Tu suscripción está vencida. Regulariza tu pago."
	case "reminder":
		if sub.DaysUntilExpiry > 0 {
			return true, "warning", fmt.Sprintf("Tu plan vence en %d día(s). Programa tu renovación.", sub.DaysUntilExpiry)
		}
		if ctx.HasRealDebt {
			return true, "warning", fmt.Sprintf("Tienes un pago pendiente de S/ %.2f.", ctx.DisplayDebtAmount)
		}
		return true, "warning", "Renovación próxima."
	default:
		return false, "success", "Tu suscripción está activa."
	}
}

// ShouldShowRenewalBanner solo dentro del rango de recordatorios configurados.
func ShouldShowRenewalBanner(daysUntilExpiry int, effectiveStatus string, reminderDays []int) bool {
	if daysUntilExpiry <= 0 || effectiveStatus != database.SaasSubActive {
		return false
	}
	maxRem := MaxReminderDay(reminderDays)
	return maxRem > 0 && daysUntilExpiry <= maxRem
}
