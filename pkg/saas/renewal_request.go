package saas

import (
	"errors"
	"time"

	"tukifac/pkg/database"
)

// SubmitRenewalRequestInput — el tenant elige un plan y, opcionalmente, adjunta comprobante en
// el mismo paso. A diferencia de SubmitPayment (que exige un billing_cycle_id ya emitido por el
// admin/cron), esta solicitud no necesita que exista ningún cobro pendiente: cubre tanto al
// tenant que nunca tuvo suscripción como al que la tiene vencida y quiere elegir otra.
type SubmitRenewalRequestInput struct {
	TenantID uint
	PlanID   uint
	// PeriodMonths: cuántos meses está pagando/pidiendo el tenant. <=0 se corrige a 1 — igual
	// de importante para renovaciones normales que para pagos adelantados (un tenant con
	// suscripción todavía activa que ya quiere pagar 3 meses más, sin esperar a vencer).
	PeriodMonths  int
	Amount        float64
	PaymentMethod string
	PaymentDate   *time.Time
	Reference     string
	// ReceiptURL opcional: el tenant puede pedir el plan sin comprobante todavía (queda
	// pending_review sin acceso provisional) o adjuntarlo de una vez (mismo trato que un pago
	// normal: provisional si la suscripción actual está suspendida/vencida, ver SubmitPayment).
	ReceiptURL  string
	Notes       string
	SubmittedBy *uint
}

// SubmitRenewalRequest valida el plan y delega en SubmitPayment (misma transacción, mismas
// reglas de strikes/bloqueo vía CanTenantSubmitPayment): un solo camino de escritura para pagos,
// evita que esta ruta nueva diverja en el futuro de las reglas de la ruta existente.
func SubmitRenewalRequest(in SubmitRenewalRequestInput) (*database.SaasPayment, error) {
	if in.TenantID == 0 {
		return nil, errors.New("tenant_id requerido")
	}
	if in.PlanID == 0 {
		return nil, errors.New("plan_id requerido")
	}
	var plan database.SaasPlan
	if err := database.CentralDB.Where("id = ? AND active = ?", in.PlanID, true).First(&plan).Error; err != nil {
		return nil, errors.New("plan no encontrado o inactivo")
	}
	months := in.PeriodMonths
	if months <= 0 {
		months = 1
	}
	amount := in.Amount
	if amount <= 0 {
		amount = plan.Price * float64(months)
	}
	return SubmitPayment(SubmitPaymentInput{
		TenantID:      in.TenantID,
		Amount:        amount,
		PeriodMonths:  months,
		PaymentMethod: in.PaymentMethod,
		PaymentDate:   in.PaymentDate,
		Reference:     in.Reference,
		ReceiptURL:    in.ReceiptURL,
		Notes:         in.Notes,
		SubmittedBy:   in.SubmittedBy,
		PlanID:        in.PlanID,
	})
}
