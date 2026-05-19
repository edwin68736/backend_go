package service

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"tukifac/pkg/database"

	"gorm.io/gorm"
)

// Marcador para concatenar notas de cierre sin perder las de apertura (un solo campo Notes en BD).
const sessionNotesClosingMarker = "\n\n[Notas de cierre]\n"

func mergeSessionNotesOnClose(existingNotes, closingNotes string) string {
	opening := strings.TrimSpace(existingNotes)
	closing := strings.TrimSpace(closingNotes)
	if closing == "" {
		return opening
	}
	if opening == "" {
		return closing
	}
	return opening + sessionNotesClosingMarker + closing
}

type CashBankService struct {
	db *gorm.DB
}

func NewCashBankService(db *gorm.DB) *CashBankService {
	return &CashBankService{db: db}
}

// =================== CAJA ===================

// OpenSession abre una nueva sesión de caja.
func (s *CashBankService) OpenSession(branchID, userID uint, openingBalance float64, notes string) (*database.TenantCashSession, error) {
	// Verificar que no haya una sesión abierta
	var existing database.TenantCashSession
	if err := s.db.Where("branch_id = ? AND status = ?", branchID, "open").First(&existing).Error; err == nil {
		return nil, errors.New("ya existe una sesión de caja abierta para esta sucursal")
	}

	now := time.Now()
	session := &database.TenantCashSession{
		BranchID:       branchID,
		UserID:         userID,
		OpenedBy:       userID,
		OpeningBalance: openingBalance,
		Notes:          notes,
		Status:         "open",
		OpenedAt:       now,
	}
	err := s.db.Create(session).Error
	return session, err
}

// CloseSession cierra una sesión de caja. Arqueo opcional: si se envía, se valida la suma con el saldo esperado y se guarda.
// Si no se envía arqueo, closing_balance puede ser el esperado o uno manual; si se envía arqueo, closing_balance se sobrescribe con la suma del arqueo.
func (s *CashBankService) CloseSession(sessionID, userID uint, closingBalance float64, notes string, arqueo map[string]float64) error {
	var session database.TenantCashSession
	if err := s.db.First(&session, sessionID).Error; err != nil {
		return errors.New("sesión de caja no encontrada")
	}
	if session.Status == "closed" {
		return errors.New("la sesión ya está cerrada")
	}

	expected := s.getExpectedBalance(sessionID)
	now := time.Now()
	combinedNotes := mergeSessionNotesOnClose(session.Notes, notes)
	updates := map[string]interface{}{
		"closed_by":        userID,
		"expected_balance": expected,
		"notes":            combinedNotes,
		"status":           "closed",
		"closed_at":        now,
	}

	if len(arqueo) > 0 {
		arqueoSum := sumArqueo(arqueo)
		arqueoJSON, _ := json.Marshal(arqueo)
		updates["arqueo_json"] = string(arqueoJSON)
		updates["closing_balance"] = arqueoSum
		updates["difference"] = arqueoSum - expected
	} else {
		updates["closing_balance"] = closingBalance
		updates["difference"] = closingBalance - expected
	}

	return s.db.Model(&session).Updates(updates).Error
}

func (s *CashBankService) getExpectedBalance(sessionID uint) float64 {
	var session database.TenantCashSession
	if err := s.db.First(&session, sessionID).Error; err != nil {
		return 0
	}
	var totalIncome, totalExpense float64
	s.db.Model(&database.TenantCashMovement{}).
		Where("cash_session_id = ? AND type = ?", sessionID, "income").
		Select("COALESCE(SUM(amount), 0)").Scan(&totalIncome)
	s.db.Model(&database.TenantCashMovement{}).
		Where("cash_session_id = ? AND type = ?", sessionID, "expense").
		Select("COALESCE(SUM(amount), 0)").Scan(&totalExpense)
	return session.OpeningBalance + totalIncome - totalExpense
}

// sumArqueo suma denominaciones: keys "200","100",...,"0.1" * cantidad.
func sumArqueo(arqueo map[string]float64) float64 {
	var total float64
	for denomStr, qty := range arqueo {
		denom, _ := strconv.ParseFloat(denomStr, 64)
		total += denom * qty
	}
	return total
}

// SaveArqueo guarda o actualiza el arqueo. Si la sesión está abierta, se puede actualizar cuando sea. Si está cerrada y no tiene arqueo, se puede registrar una vez (ya no se modifica).
func (s *CashBankService) SaveArqueo(sessionID, userID uint, arqueo map[string]float64) (sum float64, err error) {
	var session database.TenantCashSession
	if err := s.db.First(&session, sessionID).Error; err != nil {
		return 0, errors.New("sesión de caja no encontrada")
	}
	arqueoJSON, _ := json.Marshal(arqueo)
	sum = sumArqueo(arqueo)

	if session.Status == "open" {
		// Caja abierta: actualizar arqueo (borrador) cuando sea
		return sum, s.db.Model(&session).Update("arqueo_json", string(arqueoJSON)).Error
	}
	// Caja cerrada
	if session.ArqueoJSON != "" {
		return 0, errors.New("esta caja ya tiene arqueo registrado y no se puede modificar")
	}
	// Registrar arqueo por primera vez en caja cerrada
	expected := s.getExpectedBalance(sessionID)
	diff := sum - expected
	return sum, s.db.Model(&session).Updates(map[string]interface{}{
		"arqueo_json":     string(arqueoJSON),
		"closing_balance": sum,
		"expected_balance": expected,
		"difference":      diff,
	}).Error
}

// GetSessionByID devuelve una sesión por ID (incluye arqueo_json para ver en modal).
func (s *CashBankService) GetSessionByID(sessionID uint) (*database.TenantCashSession, error) {
	var session database.TenantCashSession
	if err := s.db.First(&session, sessionID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("sesión no encontrada")
		}
		return nil, err
	}
	return &session, nil
}

// GetOpenSession retorna la sesión abierta. Si branchID > 0 filtra por sucursal; si branchID == 0 retorna la primera sesión abierta (cualquier sucursal) para que la vista muestre la caja cuando no se envía branch_id.
func (s *CashBankService) GetOpenSession(branchID, _ uint) (*database.TenantCashSession, error) {
	var session database.TenantCashSession
	q := s.db.Where("status = ?", "open")
	if branchID > 0 {
		q = q.Where("branch_id = ?", branchID)
	}
	err := q.Order("opened_at DESC").First(&session).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &session, err
}

// AddMovement registra un movimiento manual de caja. Verifica que la sesión exista y esté abierta.
func (s *CashBankService) AddMovement(sessionID, userID uint, movType, category, reference, paymentMethod string, amount float64, notes string) error {
	if amount <= 0 {
		return errors.New("el monto debe ser mayor a cero")
	}
	if movType != "income" && movType != "expense" {
		return errors.New("tipo de movimiento inválido")
	}
	var session database.TenantCashSession
	if err := s.db.First(&session, sessionID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New("sesión de caja no encontrada")
		}
		return err
	}
	if session.Status != "open" {
		return errors.New("no se pueden registrar movimientos en una caja cerrada")
	}
	if err := s.db.Create(&database.TenantCashMovement{
		CashSessionID: sessionID,
		Type:          movType,
		Amount:        amount,
		PaymentMethod: paymentMethod,
		Category:      category,
		Reference:     reference,
		Notes:         notes,
		UserID:        userID,
		CreatedAt:     time.Now(),
	}).Error; err != nil {
		return err
	}
	// Actualizar saldo de la cuenta financiera asociada al método de pago
	desc := "Caja: " + category
	if reference != "" {
		desc += " " + reference
	}
	return s.RecordPaymentToAccount(nil, paymentMethod, amount, movType == "income", reference, desc, userID)
}

func (s *CashBankService) GetMovements(sessionID uint) ([]database.TenantCashMovement, error) {
	var movements []database.TenantCashMovement
	err := s.db.Where("cash_session_id = ?", sessionID).Order("created_at DESC").Find(&movements).Error
	return movements, err
}

func (s *CashBankService) ListSessions(branchID uint) ([]database.TenantCashSession, error) {
	var sessions []database.TenantCashSession
	q := s.db.Model(&database.TenantCashSession{})
	if branchID > 0 {
		q = q.Where("branch_id = ?", branchID)
	}
	err := q.Order("opened_at DESC").Limit(50).Find(&sessions).Error
	return sessions, err
}

// =================== BANCOS ===================

// NormalizePaymentMethod normaliza el método de pago para buscar la cuenta asociada.
func NormalizePaymentMethod(m string) string {
	if m == "" {
		return "efectivo"
	}
	switch m {
	case "Efectivo", "efectivo":
		return "efectivo"
	case "Yape", "yape":
		return "yape"
	case "Plin", "plin":
		return "plin"
	case "Tarjeta", "tarjeta":
		return "tarjeta"
	case "Transferencia", "transferencia":
		return "transferencia"
	default:
		return m
	}
}

// GetAccountByPaymentMethod devuelve la primera cuenta activa asociada al método de pago.
func (s *CashBankService) GetAccountByPaymentMethod(paymentMethod string) (*database.TenantBankAccount, error) {
	method := NormalizePaymentMethod(paymentMethod)
	var acc database.TenantBankAccount
	err := s.db.Where("active = ? AND payment_method = ?", true, method).First(&acc).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &acc, nil
}

// RecordPaymentToAccount registra un movimiento en la cuenta asociada al método de pago y actualiza el saldo.
// db puede ser una transacción (tx) o nil para usar s.db. isCredit=true = ingreso (aumenta saldo), false = egreso.
func (s *CashBankService) RecordPaymentToAccount(db *gorm.DB, paymentMethod string, amount float64, isCredit bool, reference, description string, userID uint) error {
	if amount <= 0 {
		return nil
	}
	exec := s.db
	if db != nil {
		exec = db
	}
	acc, err := s.GetAccountByPaymentMethod(paymentMethod)
	if err != nil || acc == nil {
		return nil // sin cuenta configurada para este método, no fallar
	}
	movType := "debit"
	delta := -amount
	if isCredit {
		movType = "credit"
		delta = amount
	}
	now := time.Now()
	if err := exec.Create(&database.TenantBankMovement{
		BankAccountID: acc.ID,
		Type:          movType,
		Amount:        amount,
		Description:   description,
		Reference:     reference,
		Date:          now,
		UserID:        userID,
		CreatedAt:     now,
	}).Error; err != nil {
		return err
	}
	return exec.Model(&database.TenantBankAccount{}).
		Where("id = ?", acc.ID).
		Update("balance", gorm.Expr("balance + ?", delta)).Error
}

func (s *CashBankService) ListBankAccounts() ([]database.TenantBankAccount, error) {
	var accounts []database.TenantBankAccount
	err := s.db.Where("active = ?", true).Order("type ASC, name ASC").Find(&accounts).Error
	return accounts, err
}

// ListPaymentMethods devuelve los códigos de métodos de pago (legacy). Preferir ListPaymentMethodRecords.
func (s *CashBankService) ListPaymentMethods() []string {
	var records []database.TenantPaymentMethod
	if err := s.db.Where("active = ?", true).Order("sort_order ASC, id ASC").Find(&records).Error; err != nil {
		// Fallback si la tabla no existe o está vacía
		return []string{"cash", "yape", "plin", "transferencia", "tarjeta"}
	}
	codes := make([]string, 0, len(records))
	for _, r := range records {
		codes = append(codes, r.Code)
	}
	return codes
}

// ListPaymentMethodRecords devuelve todos los métodos de pago del tenant (para gestión y ventas).
func (s *CashBankService) ListPaymentMethodRecords() ([]database.TenantPaymentMethod, error) {
	var list []database.TenantPaymentMethod
	err := s.db.Where("active = ?", true).Order("sort_order ASC, id ASC").Find(&list).Error
	return list, err
}

// ListAllPaymentMethodRecords incluye inactivos (para administración).
func (s *CashBankService) ListAllPaymentMethodRecords() ([]database.TenantPaymentMethod, error) {
	var list []database.TenantPaymentMethod
	err := s.db.Order("sort_order ASC, id ASC").Find(&list).Error
	return list, err
}

// GetPaymentMethodByID devuelve un método de pago por ID.
func (s *CashBankService) GetPaymentMethodByID(id uint) (*database.TenantPaymentMethod, error) {
	var pm database.TenantPaymentMethod
	if err := s.db.First(&pm, id).Error; err != nil {
		return nil, err
	}
	return &pm, nil
}

// GetPaymentMethodByCode devuelve un método de pago por código (cash, yape, etc.).
func (s *CashBankService) GetPaymentMethodByCode(code string) (*database.TenantPaymentMethod, error) {
	code = NormalizePaymentMethodCode(code)
	var pm database.TenantPaymentMethod
	if err := s.db.Where("code = ? AND active = ?", code, true).First(&pm).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &pm, nil
}

// NormalizePaymentMethodCode normaliza para búsqueda: efectivo->cash, etc.
func NormalizePaymentMethodCode(c string) string {
	switch c {
	case "Efectivo", "efectivo":
		return "cash"
	case "Yape", "yape":
		return "yape"
	case "Plin", "plin":
		return "plin"
	case "Tarjeta", "tarjeta":
		return "tarjeta"
	case "Transferencia", "transferencia":
		return "transferencia"
	default:
		if c == "" {
			return "cash"
		}
		return c
	}
}

// CreatePaymentMethod crea un método de pago.
func (s *CashBankService) CreatePaymentMethod(name, code, destinationType string, bankAccountID *uint) (*database.TenantPaymentMethod, error) {
	if name == "" || code == "" {
		return nil, errors.New("nombre y código son requeridos")
	}
	if destinationType != "cash" && destinationType != "bank_account" {
		return nil, errors.New("destination_type debe ser cash o bank_account")
	}
	if destinationType == "bank_account" && (bankAccountID == nil || *bankAccountID == 0) {
		return nil, errors.New("debe indicar la cuenta bancaria cuando el destino es bank_account")
	}
	code = NormalizePaymentMethodCode(code)
	// Evitar duplicados de código
	var existing database.TenantPaymentMethod
	if err := s.db.Where("code = ?", code).First(&existing).Error; err == nil {
		return nil, errors.New("ya existe un método de pago con ese código")
	}
	var maxOrder int
	s.db.Model(&database.TenantPaymentMethod{}).Select("COALESCE(MAX(sort_order), 0)").Scan(&maxOrder)
	pm := &database.TenantPaymentMethod{
		Name:            name,
		Code:            code,
		DestinationType: destinationType,
		BankAccountID:   bankAccountID,
		IsSystem:        false,
		SortOrder:       maxOrder + 1,
		Active:          true,
	}
	return pm, s.db.Create(pm).Error
}

// UpdatePaymentMethod actualiza un método de pago.
func (s *CashBankService) UpdatePaymentMethod(id uint, name, code, destinationType string, bankAccountID *uint, active bool) error {
	var pm database.TenantPaymentMethod
	if err := s.db.First(&pm, id).Error; err != nil {
		return err
	}
	if pm.IsSystem && code != "" && NormalizePaymentMethodCode(code) != "cash" {
		return errors.New("no se puede cambiar el código del método efectivo (sistema)")
	}
	if name != "" {
		pm.Name = name
	}
	if code != "" && !pm.IsSystem {
		pm.Code = NormalizePaymentMethodCode(code)
	}
	if destinationType != "" {
		if destinationType != "cash" && destinationType != "bank_account" {
			return errors.New("destination_type debe ser cash o bank_account")
		}
		pm.DestinationType = destinationType
		if destinationType == "bank_account" && (bankAccountID == nil || *bankAccountID == 0) {
			return errors.New("debe indicar la cuenta bancaria cuando el destino es bank_account")
		}
		if destinationType == "cash" {
			pm.BankAccountID = nil
		} else {
			pm.BankAccountID = bankAccountID
		}
	}
	pm.Active = active
	return s.db.Save(&pm).Error
}

// DeletePaymentMethod elimina (soft) un método de pago. No permite eliminar is_system (cash).
func (s *CashBankService) DeletePaymentMethod(id uint) error {
	var pm database.TenantPaymentMethod
	if err := s.db.First(&pm, id).Error; err != nil {
		return err
	}
	if pm.IsSystem {
		return errors.New("no se puede eliminar el método efectivo (es obligatorio)")
	}
	return s.db.Delete(&pm).Error
}

// RecordPayment distribuye un pago según la configuración del método: a caja (TenantCashMovement) o a cuenta bancaria (TenantBankMovement).
// cashSessionID: requerido cuando destination_type=cash. saleNumber y description para referencias.
func (s *CashBankService) RecordPayment(tx *gorm.DB, paymentMethodCode string, amount float64, cashSessionID *uint, saleNumber, description string, saleID *uint, userID uint) error {
	if amount <= 0 {
		return nil
	}
	exec := s.db
	if tx != nil {
		exec = tx
	}
	pm, err := s.GetPaymentMethodByCode(paymentMethodCode)
	if err != nil || pm == nil {
		// Fallback legacy: intentar RecordPaymentToAccount (cuenta por payment_method en TenantBankAccount)
		return s.RecordPaymentToAccount(tx, paymentMethodCode, amount, true, saleNumber, description, userID)
	}
	switch pm.DestinationType {
	case "cash":
		if cashSessionID == nil || *cashSessionID == 0 {
			return nil // sin sesión de caja, no registrar en caja (evitar error)
		}
		return exec.Create(&database.TenantCashMovement{
			CashSessionID: *cashSessionID,
			Type:          "income",
			Amount:        amount,
			PaymentMethod: pm.Code,
			Category:      "Venta",
			Reference:     saleNumber,
			SaleID:        saleID,
			UserID:        userID,
			CreatedAt:     time.Now(),
		}).Error
	case "bank_account":
		// Cuenta explícita en el método de pago, o la primera cuenta activa con payment_method coincidente
		// (misma lógica que Caja → Cuentas: muchos tenants vinculan solo TenantBankAccount.payment_method).
		bankAccID := uint(0)
		if pm.BankAccountID != nil && *pm.BankAccountID > 0 {
			bankAccID = *pm.BankAccountID
		} else if acc, err := s.GetAccountByPaymentMethod(pm.Code); err == nil && acc != nil {
			bankAccID = acc.ID
		}
		if bankAccID == 0 {
			return s.RecordPaymentToAccount(tx, paymentMethodCode, amount, true, saleNumber, description, userID)
		}
		delta := amount
		if err := exec.Create(&database.TenantBankMovement{
			BankAccountID: bankAccID,
			Type:          "credit",
			Amount:        amount,
			Description:   description,
			Reference:     saleNumber,
			Date:          time.Now(),
			UserID:        userID,
			CreatedAt:     time.Now(),
		}).Error; err != nil {
			return err
		}
		return exec.Model(&database.TenantBankAccount{}).
			Where("id = ?", bankAccID).
			Update("balance", gorm.Expr("balance + ?", delta)).Error
	default:
		return nil
	}
}

// ListAllBankAccounts lista todas las cuentas (activas e inactivas) para administración.
func (s *CashBankService) ListAllBankAccounts() ([]database.TenantBankAccount, error) {
	var accounts []database.TenantBankAccount
	err := s.db.Order("type ASC, name ASC").Find(&accounts).Error
	return accounts, err
}

func (s *CashBankService) CreateBankAccount(name, bankName, accountNumber, currency, accountType, paymentMethod string, initialBalance float64) (*database.TenantBankAccount, error) {
	if name == "" {
		return nil, errors.New("nombre de cuenta requerido")
	}
	if currency == "" {
		currency = "PEN"
	}
	if accountType == "" {
		accountType = "bank"
	}
	acc := &database.TenantBankAccount{
		Name:          name,
		BankName:      bankName,
		AccountNumber: accountNumber,
		Currency:      currency,
		Balance:       initialBalance,
		Type:          accountType,
		Active:        true,
	}
	if paymentMethod != "" {
		acc.PaymentMethod = NormalizePaymentMethod(paymentMethod)
	}
	err := s.db.Create(acc).Error
	return acc, err
}

// UpdateBankAccount actualiza nombre, tipo, método de pago y estado. No modifica el saldo.
func (s *CashBankService) UpdateBankAccount(id uint, name, bankName, accountNumber, accountType, paymentMethod string, active bool) error {
	updates := map[string]interface{}{
		"name": name, "bank_name": bankName, "account_number": accountNumber,
		"type": accountType, "active": active,
	}
	if paymentMethod != "" {
		updates["payment_method"] = NormalizePaymentMethod(paymentMethod)
	} else {
		updates["payment_method"] = ""
	}
	return s.db.Model(&database.TenantBankAccount{}).Where("id = ?", id).Updates(updates).Error
}

// GetBankAccountByID devuelve una cuenta por ID.
func (s *CashBankService) GetBankAccountByID(id uint) (*database.TenantBankAccount, error) {
	var acc database.TenantBankAccount
	if err := s.db.First(&acc, id).Error; err != nil {
		return nil, err
	}
	return &acc, nil
}

func (s *CashBankService) AddBankMovement(accountID, userID uint, movType, description, reference string, amount float64, date time.Time) error {
	if amount <= 0 {
		return errors.New("monto inválido")
	}

	return s.db.Transaction(func(tx *gorm.DB) error {
		// Registrar movimiento
		if err := tx.Create(&database.TenantBankMovement{
			BankAccountID: accountID,
			Type:          movType,
			Amount:        amount,
			Description:   description,
			Reference:     reference,
			Date:          date,
			UserID:        userID,
			CreatedAt:     time.Now(),
		}).Error; err != nil {
			return err
		}

		// Actualizar saldo
		var delta float64
		if movType == "credit" {
			delta = amount
		} else {
			delta = -amount
		}
		return tx.Model(&database.TenantBankAccount{}).
			Where("id = ?", accountID).
			Update("balance", gorm.Expr("balance + ?", delta)).Error
	})
}

func (s *CashBankService) ListBankMovements(accountID uint) ([]database.TenantBankMovement, error) {
	var movements []database.TenantBankMovement
	err := s.db.Where("bank_account_id = ?", accountID).Order("date DESC, created_at DESC").Find(&movements).Error
	return movements, err
}
