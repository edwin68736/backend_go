package service

import (
	"errors"
	"strings"
	"time"

	"tukifac/pkg/database"

	"gorm.io/gorm"
)

func movementAlreadyReversed(tx *gorm.DB, table string, originalID uint) (bool, error) {
	var cnt int64
	if err := tx.Table(table).Where("reversal_of_id = ?", originalID).Count(&cnt).Error; err != nil {
		return false, err
	}
	return cnt > 0, nil
}

// CreateBankReversal genera un movimiento bancario compensatorio sin modificar el original.
func (s *CashBankService) CreateBankReversal(tx *gorm.DB, original database.TenantBankMovement, description, reference string, userID uint) error {
	exec := s.db
	if tx != nil {
		exec = tx
	}
	done, err := movementAlreadyReversed(exec, "tenant_bank_movements", original.ID)
	if err != nil {
		return err
	}
	if done {
		return nil
	}

	reversalType := "credit"
	delta := original.Amount
	if original.Type == "credit" {
		reversalType = "debit"
		delta = -original.Amount
	} else if original.Type != "debit" {
		return nil
	}

	uid := userID
	if uid == 0 {
		uid = original.UserID
	}
	origID := original.ID
	now := time.Now()
	rev := database.TenantBankMovement{
		BankAccountID: original.BankAccountID,
		Type:          reversalType,
		Amount:        original.Amount,
		Description:   description,
		Reference:     reference,
		Date:          now,
		UserID:        uid,
		ReversalOfID:  &origID,
		SaleID:        original.SaleID,
		PurchaseID:    original.PurchaseID,
		// CashSessionID: se conserva la del movimiento original (la sesión donde ocurrió
		// realmente el pago que se está revirtiendo) — nunca se inventa una nueva. Antes esta
		// reversión quedaba con cash_session_id=NULL aunque el original sí lo tuviera, lo que la
		// hacía invisible para GetSessionBalanceSummary(sessionID) de esa misma sesión (la
		// consulta filtra por cash_session_id=?, y NULL nunca coincide) — una venta/compra
		// anulada seguía apareciendo como ingreso activo en el balance en vivo de su sesión
		// original. Mismo criterio que ya usa CreateCashReversal para tenant_cash_movements.
		CashSessionID: original.CashSessionID,
		CreatedAt:     now,
	}
	if err := exec.Create(&rev).Error; err != nil {
		return err
	}
	return exec.Model(&database.TenantBankAccount{}).
		Where("id = ?", original.BankAccountID).
		Update("balance", gorm.Expr("balance + ?", delta)).Error
}

// ReverseBankMovementsByReference revierte movimientos bancarios del tipo indicado que comparten referencia.
func (s *CashBankService) ReverseBankMovementsByReference(tx *gorm.DB, reference, originalType, description, voidReference string, userID uint) error {
	exec := s.db
	if tx != nil {
		exec = tx
	}
	var originals []database.TenantBankMovement
	if err := exec.Where("reference = ? AND type = ?", reference, originalType).Find(&originals).Error; err != nil {
		return err
	}
	for _, orig := range originals {
		if err := s.CreateBankReversal(tx, orig, description, voidReference, userID); err != nil {
			return err
		}
	}
	return nil
}

// CreateCashReversal genera un movimiento de caja compensatorio sin modificar el original.
func (s *CashBankService) CreateCashReversal(tx *gorm.DB, original database.TenantCashMovement, category, reference, notes string, userID uint) error {
	exec := s.db
	if tx != nil {
		exec = tx
	}
	done, err := movementAlreadyReversed(exec, "tenant_cash_movements", original.ID)
	if err != nil {
		return err
	}
	if done {
		return nil
	}

	reversalType := "expense"
	if original.Type == "expense" {
		reversalType = "income"
	} else if original.Type != "income" {
		return nil
	}

	uid := userID
	if uid == 0 {
		uid = original.UserID
	}
	origID := original.ID
	now := time.Now()
	rev := database.TenantCashMovement{
		CashSessionID: original.CashSessionID,
		Type:          reversalType,
		Amount:        original.Amount,
		PaymentMethod: original.PaymentMethod,
		Category:      category,
		Reference:     reference,
		SaleID:        original.SaleID,
		PurchaseID:    original.PurchaseID,
		ReversalOfID:  &origID,
		Notes:         notes,
		UserID:        uid,
		CreatedAt:     now,
	}
	return exec.Create(&rev).Error
}

// ReverseManualMovement revierte un movimiento MANUAL de caja (ingreso/egreso sin venta ni
// compra asociada — AddMovement) — Fase 5. El original nunca se modifica ni se borra: se crea
// un movimiento nuevo de signo opuesto con reversal_of_id, igual que ya hacen
// CreateCashReversal/CreateBankReversal para ventas y compras.
//
// AddMovement, cuando el método de pago no es efectivo, además refleja el movimiento en
// tenant_bank_movements (para el saldo de la cuenta) — sin esa fila también revertida, el
// saldo de la cuenta quedaría desalineado tras la reversión. No existe un vínculo tipado entre
// ambas filas (mismo límite que ya tenía AddMovement al crearlas), así que se ubica la fila
// bancaria por sesión + referencia + monto + tipo esperado, sin reversión previa.
func (s *CashBankService) ReverseManualMovement(tx *gorm.DB, movementID, userID uint, notes string) error {
	exec := s.db
	if tx != nil {
		exec = tx
	}
	var original database.TenantCashMovement
	if err := exec.First(&original, movementID).Error; err != nil {
		return errors.New("movimiento no encontrado")
	}
	if original.SaleID != nil || original.PurchaseID != nil {
		return errors.New("este movimiento pertenece a una venta o compra; anúlela desde ahí")
	}
	if original.ReversalOfID != nil {
		return errors.New("no se puede revertir una reversión")
	}
	already, err := movementAlreadyReversed(exec, "tenant_cash_movements", original.ID)
	if err != nil {
		return err
	}
	if already {
		return errors.New("este movimiento ya fue revertido")
	}
	var session database.TenantCashSession
	if err := exec.First(&session, original.CashSessionID).Error; err != nil {
		return errors.New("sesión de caja no encontrada")
	}
	if session.Status != "open" {
		return errors.New("no se puede revertir un movimiento de una sesión ya cerrada")
	}

	category := "Reversión manual"
	if strings.TrimSpace(original.Category) != "" {
		category = "Reversión: " + original.Category
	}
	if err := s.CreateCashReversal(exec, original, category, original.Reference, notes, userID); err != nil {
		return err
	}

	if !IsCashPaymentMethod(original.PaymentMethod) {
		bankMovType := "credit"
		if original.Type == "expense" {
			bankMovType = "debit"
		}
		var bankMov database.TenantBankMovement
		err := exec.Where(
			"cash_session_id = ? AND reference = ? AND amount = ? AND type = ? AND sale_id IS NULL AND purchase_id IS NULL",
			original.CashSessionID, original.Reference, original.Amount, bankMovType,
		).Order("created_at DESC").First(&bankMov).Error
		if err == nil {
			reversed, rErr := movementAlreadyReversed(exec, "tenant_bank_movements", bankMov.ID)
			if rErr != nil {
				return rErr
			}
			if !reversed {
				if err := s.CreateBankReversal(exec, bankMov, notes, original.Reference, userID); err != nil {
					return err
				}
			}
		}
		// Si no se encuentra (p. ej. no había cuenta configurada para ese método cuando se
		// creó el movimiento original), no hay nada que revertir del lado banco — igual que
		// AddMovement no falla si no hay cuenta configurada.
	}
	return nil
}
