package service

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	prepaymentsvc "tukifac/internal/prepayment"
	salesvc "tukifac/internal/sales/service"
	"tukifac/pkg/billingstate"
	"tukifac/pkg/database"
	"tukifac/pkg/logger"
	"tukifac/pkg/paymentcondition"
	"tukifac/pkg/saas"

	"gorm.io/gorm"
)

// laterCollectionGrace margen para distinguir el adelanto cobrado al vender (mismo instante que la
// venta) de un cobro posterior de cuentas por cobrar.
const laterCollectionGrace = 2 * time.Minute

// VoidRejectedInput datos de la anulación local de un comprobante rechazado por SUNAT.
type VoidRejectedInput struct {
	SaleID     uint
	Reason     string // obligatorio; queda en la venta y en la auditoría
	ActorID    uint
	ActorEmail string
	ClientIP   string
}

// VoidRejectedSale anula LOCALMENTE una factura o boleta que SUNAT rechazó.
//
// Un comprobante rechazado nunca existió para SUNAT, así que no se anula con nota de crédito ni
// con comunicación de baja (ambas exigen un comprobante aceptado). Pero la venta sí movió stock,
// caja/bancos, seriales y anticipos al registrarse; sin esta acción esos efectos quedaban vivos
// para siempre. Reusa SaleService.Cancel (stock, seriales, caja y bancos) y suma la reposición de
// anticipos. La venta queda con status "cancelled" y por eso deja de contar en dashboards,
// analytics y reporte de caja, sin tocarlos.
//
// Guardas: solo factura/boleta con billing_status "rejected", sin evidencia de aceptación (se
// sincroniza con el facturador antes de decidir), y no se permite en ventas a crédito con cobros
// posteriores — esos cobros pueden estar en otras cajas y se resuelven primero a mano.
//
// El correlativo NO se reutiliza: queda consumido, igual que cualquier otra venta anulada.
func (s *BillingService) VoidRejectedSale(in VoidRejectedInput) error {
	reason := strings.TrimSpace(in.Reason)
	if reason == "" {
		return errors.New("indique el motivo de la anulación")
	}
	if in.SaleID == 0 {
		return errors.New("venta requerida")
	}

	var sale database.TenantSale
	if err := s.db.First(&sale, in.SaleID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New("venta no encontrada")
		}
		return err
	}
	if strings.EqualFold(sale.Status, "cancelled") {
		return errors.New("la venta ya está anulada")
	}
	// Una NC/ND rechazada no movió stock ni caja: aplicarle Cancel repondría stock que nunca salió.
	if sale.DocType != "FACTURA" && sale.DocType != "BOLETA" {
		return errors.New("solo se puede anular de esta forma una factura o boleta rechazada")
	}

	// El estado local puede estar atrasado frente al facturador (p. ej. un webhook perdido): se
	// sincroniza antes de decidir, para no anular algo que SUNAT en realidad aceptó.
	s.SyncSaleWithSSOT(in.SaleID)
	if err := s.db.First(&sale, in.SaleID).Error; err != nil {
		return err
	}
	if sale.BillingStatus != billingstate.BillingRejected {
		return fmt.Errorf("el comprobante no está rechazado por SUNAT (estado actual: %s)", sale.BillingStatus)
	}
	var inv database.TenantInvoice
	if err := s.db.Where("sale_id = ?", in.SaleID).First(&inv).Error; err == nil {
		if billingstate.HasAcceptanceEvidence(&inv) {
			return errors.New("SUNAT aceptó este comprobante; anúlelo con nota de crédito")
		}
	}

	if hasLaterCollections(s.db, &sale) {
		return errors.New("la venta a crédito ya tiene cobros registrados; revierta primero esos cobros desde Cuentas por cobrar")
	}

	reasonFull := "Comprobante rechazado por SUNAT. " + reason
	if err := salesvc.NewSaleService(s.db).Cancel(in.SaleID, in.ActorID, reasonFull); err != nil {
		return fmt.Errorf("anulando la venta: %w", err)
	}

	// Anticipos deducidos por esta venta: devuelve el saldo a los vouchers origen. Va aparte de
	// Cancel (que no lo hace) y un fallo aquí no deshace la anulación, solo se registra.
	if err := s.db.Transaction(func(tx *gorm.DB) error {
		return prepaymentsvc.NewService(tx).ReverseApplicationsForConsumerSaleTx(tx, in.SaleID)
	}); err != nil {
		logger.L.Warn("void_rejected_reverse_prepayment_failed",
			slog.Uint64("tenant_id", uint64(s.centralTenantID)),
			slog.Uint64("sale_id", uint64(in.SaleID)),
			slog.Any("error", err),
		)
	}

	s.auditVoidRejected(in, &sale)
	return nil
}

// hasLaterCollections indica si una venta a crédito recibió cobros después de registrarse. El
// adelanto cobrado al vender queda fuera (misma marca de tiempo que la venta).
func hasLaterCollections(db *gorm.DB, sale *database.TenantSale) bool {
	isCredit := strings.EqualFold(sale.Status, "credit") ||
		paymentcondition.IsCreditCode(sale.PaymentConditionCode) ||
		paymentcondition.IsCreditCode(sale.PaymentMethod)
	if !isCredit {
		return false
	}
	var pays []database.TenantSalePayment
	if err := db.Where("sale_id = ? AND amount > 0 AND created_at > ?", sale.ID, sale.CreatedAt.Add(laterCollectionGrace)).
		Find(&pays).Error; err != nil {
		// Ante la duda, bloquear: mejor pedir revisión manual que revertir cobros a ciegas.
		return true
	}
	for _, p := range pays {
		if paymentcondition.IsCreditCode(p.Method) {
			continue
		}
		return true
	}
	return false
}

func (s *BillingService) auditVoidRejected(in VoidRejectedInput, sale *database.TenantSale) {
	database.WriteAuditLog(&database.AuditLog{
		TenantID: s.centralTenantID,
		UserID:   in.ActorID,
		Action:   "sale_void_rejected",
		Entity:   "tenant_sale",
		EntityID: in.SaleID,
		Payload: saas.MetaJSON(map[string]interface{}{
			"sale_id":     in.SaleID,
			"document":    sale.Number,
			"doc_type":    sale.DocType,
			"total":       sale.Total,
			"reason":      strings.TrimSpace(in.Reason),
			"actor_email": in.ActorEmail,
			"tenant_slug": s.tenantSlug,
		}),
		IPAddress: in.ClientIP,
	})
}
