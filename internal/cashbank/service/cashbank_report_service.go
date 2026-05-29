package service

import (
	"errors"
	"strings"
	"time"

	"tukifac/pkg/database"

	"gorm.io/gorm"
)

// SessionReport es el reporte de cierre/resumen de una sesión de caja.
type SessionReport struct {
	Session              SessionReportHeader   `json:"session"`
	IncomeDetail         []IncomeDetailRow      `json:"income_detail"`
	ExpenseDetail        []ExpenseDetailRow     `json:"expense_detail"`
	CancelledSalesDetail []CancelledSaleRow     `json:"cancelled_sales_detail"`
	TotalsByMethod       TotalsByMethodReport   `json:"totals_by_method"`
	Totals               SessionTotals          `json:"totals"`
}

type SessionReportHeader struct {
	ID               uint       `json:"id"`
	BranchID         uint       `json:"branch_id"`
	BranchName       string     `json:"branch_name"`
	OpenedByUserID   uint       `json:"opened_by_user_id"`
	OpenedByUserName string     `json:"opened_by_user_name"`
	OpenedAt         time.Time  `json:"opened_at"`
	ClosedAt         *time.Time `json:"closed_at"`
	OpeningBalance   float64    `json:"opening_balance"`
	ClosingBalance   *float64   `json:"closing_balance"`
	Status           string     `json:"status"`
	Notes            string     `json:"notes"` // apertura; si hubo notas de cierre, se concatenan al cerrar la sesión
}

type IncomeDetailRow struct {
	Date           time.Time `json:"date"`
	Type           string    `json:"type"`
	DocNumber      string    `json:"doc_number"`
	Reference      string    `json:"reference"`
	Amount         float64   `json:"amount"`
	PaymentMethod  string    `json:"payment_method"`
}

type ExpenseDetailRow struct {
	Date           time.Time `json:"date"`
	Type           string    `json:"type"`
	DocNumber      string    `json:"doc_number"`
	Reference      string    `json:"reference"`
	Amount         float64   `json:"amount"`
	PaymentMethod  string    `json:"payment_method"`
}

// CancelledSaleRow venta anulada vinculada a la sesión (reversión en caja).
type CancelledSaleRow struct {
	Date          time.Time `json:"date"`
	DocNumber     string    `json:"doc_number"`
	Amount        float64   `json:"amount"`
	PaymentMethod string    `json:"payment_method"`
	Reason        string    `json:"reason"`
}

type TotalsByMethodReport struct {
	Sales     []MethodTotal `json:"sales"`
	Purchases []MethodTotal `json:"purchases"`
	Movements []MethodTotal `json:"movements"`
}

type MethodTotal struct {
	Method string  `json:"method"`
	Total  float64 `json:"total"`
}

type SessionTotals struct {
	TotalIncome    float64 `json:"total_income"`
	TotalExpense   float64 `json:"total_expense"`
	TotalSales     float64 `json:"total_sales"`
	TotalPurchases float64 `json:"total_purchases"`
	FinalBalance   float64 `json:"final_balance"`
}

// MovementReportRow es una fila del reporte de movimientos.
type MovementReportRow struct {
	Date          time.Time `json:"date"`
	Type          string    `json:"type"`
	DocNumber     string    `json:"doc_number"`
	ContactName   string    `json:"contact_name"`
	UserName      string    `json:"user_name"`
	BranchName    string    `json:"branch_name"`
	PaymentMethod string    `json:"payment_method"`
	Amount        float64   `json:"amount"`
	MovementID    uint      `json:"movement_id"`
	CashSessionID uint      `json:"cash_session_id"`
	Category      string    `json:"category"`
	CashReference string    `json:"cash_reference"` // referencia del registro en caja (antes de derivar documento)
	NotesDetail   string    `json:"notes_detail"`    // notas del movimiento en caja
}

// MovementReportSummary totales sobre todos los movimientos que cumplen el filtro (no solo la página).
type MovementReportSummary struct {
	TotalRows    int64   `json:"total_rows"`
	SumIncome    float64 `json:"sum_income"`    // suma montos tipo income (positivos en BD)
	SumExpense   float64 `json:"sum_expense"`   // suma montos tipo expense (positivos en BD)
	NetMovement  float64 `json:"net_movement"`  // ingresos − egresos (impacto de caja)
}

// MovementReportFilters filtros para el reporte de movimientos.
type MovementReportFilters struct {
	BranchID      uint
	UserID        uint
	DateFrom      *time.Time
	DateTo        *time.Time
	SessionID     uint
	MovementType  string
	PaymentMethod string
	Limit         int
	Offset        int
}

func (s *CashBankService) movementReportFilteredDB(f MovementReportFilters) *gorm.DB {
	q := s.db.Model(&database.TenantCashMovement{}).
		Joins("LEFT JOIN tenant_cash_sessions ON tenant_cash_sessions.id = tenant_cash_movements.cash_session_id").
		Where("tenant_cash_movements.id > 0")

	if f.SessionID > 0 {
		q = q.Where("tenant_cash_movements.cash_session_id = ?", f.SessionID)
	}
	if f.BranchID > 0 {
		q = q.Where("tenant_cash_sessions.branch_id = ?", f.BranchID)
	}
	if f.UserID > 0 {
		q = q.Where("tenant_cash_movements.user_id = ?", f.UserID)
	}
	if f.MovementType != "" {
		q = q.Where("tenant_cash_movements.type = ?", f.MovementType)
	}
	if f.PaymentMethod != "" {
		method := NormalizePaymentMethod(f.PaymentMethod)
		q = q.Where(
			"LOWER(TRIM(tenant_cash_movements.payment_method)) IN (?, ?) OR tenant_cash_movements.payment_method = ?",
			method, strings.ToLower(strings.TrimSpace(f.PaymentMethod)), f.PaymentMethod,
		)
	}
	if f.DateFrom != nil {
		q = q.Where("tenant_cash_movements.created_at >= ?", f.DateFrom)
	}
	if f.DateTo != nil {
		q = q.Where("tenant_cash_movements.created_at <= ?", f.DateTo)
	}
	return q
}

func (s *CashBankService) sumMovementAmountByType(f MovementReportFilters, typ string) float64 {
	var sum float64
	q := s.movementReportFilteredDB(f).Where("tenant_cash_movements.type = ?", typ)
	_ = q.Select("COALESCE(SUM(tenant_cash_movements.amount),0)").Scan(&sum).Error
	return sum
}

// GetSessionReport genera el reporte de cierre para una sesión de caja.
func (s *CashBankService) GetSessionReport(sessionID uint) (*SessionReport, error) {
	var session database.TenantCashSession
	if err := s.db.First(&session, sessionID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("sesión de caja no encontrada")
		}
		return nil, err
	}

	report := &SessionReport{
		Session: SessionReportHeader{
			ID:             session.ID,
			BranchID:       session.BranchID,
			OpenedByUserID: session.OpenedBy,
			OpenedAt:       session.OpenedAt,
			ClosedAt:       session.ClosedAt,
			OpeningBalance: session.OpeningBalance,
			ClosingBalance: session.ClosingBalance,
			Status:         session.Status,
			Notes:          session.Notes,
		},
	}

	var branch database.TenantBranch
	if s.db.First(&branch, session.BranchID).Error == nil {
		report.Session.BranchName = branch.Name
	}
	var user database.TenantUser
	if s.db.First(&user, session.OpenedBy).Error == nil {
		report.Session.OpenedByUserName = user.Name
	}

	var movements []database.TenantCashMovement
	if err := s.db.Where("cash_session_id = ?", sessionID).Order("created_at ASC").Find(&movements).Error; err != nil {
		return nil, err
	}

	salesByMethod := make(map[string]float64)
	purchasesByMethod := make(map[string]float64)
	manualIncomeByMethod := make(map[string]float64)
	manualExpenseByMethod := make(map[string]float64)

	var saleIDs []uint
	for _, m := range movements {
		if m.SaleID != nil {
			saleIDs = append(saleIDs, *m.SaleID)
		}
	}

	if len(saleIDs) > 0 {
		var payments []database.TenantSalePayment
		s.db.Where("sale_id IN ?", saleIDs).Find(&payments)
		for _, p := range payments {
			salesByMethod[normalizeMethod(p.Method)] += p.Amount
		}
		var sales []database.TenantSale
		s.db.Where("id IN ?", saleIDs).Find(&sales)
		paidBySale := make(map[uint]float64)
		for _, p := range payments {
			paidBySale[p.SaleID] += p.Amount
		}
		for _, sale := range sales {
			if paidBySale[sale.ID] == 0 && sale.Total != 0 {
				meth := normalizeMethod(sale.PaymentMethod)
				if meth == "" {
					meth = "efectivo"
				}
				salesByMethod[meth] += sale.Total
			}
		}
	}

	for _, m := range movements {
		paymentMethod := normalizeMethod(m.PaymentMethod)
		if paymentMethod == "" {
			paymentMethod = "efectivo"
		}

		if m.Type == "income" {
			report.Totals.TotalIncome += m.Amount
			row := IncomeDetailRow{Date: m.CreatedAt, Amount: m.Amount, PaymentMethod: paymentMethod}
			if m.SaleID != nil {
				row.Type = "venta"
				report.Totals.TotalSales += m.Amount
				var sale database.TenantSale
				if s.db.First(&sale, *m.SaleID).Error == nil {
					row.DocNumber = sale.Number
				}
				report.IncomeDetail = append(report.IncomeDetail, row)
			} else {
				if m.Category == "ingreso_manual" || m.Category == "Ingreso manual" {
					row.Type = "ingreso_manual"
				} else {
					row.Type = "otro"
				}
				row.Reference = m.Reference
				manualIncomeByMethod[paymentMethod] += m.Amount
				report.IncomeDetail = append(report.IncomeDetail, row)
			}
		} else {
			report.Totals.TotalExpense += m.Amount
			row := ExpenseDetailRow{Date: m.CreatedAt, Amount: m.Amount, PaymentMethod: paymentMethod}
			if m.PurchaseID != nil {
				row.Type = "compra"
				report.Totals.TotalPurchases += m.Amount
				purchasesByMethod[paymentMethod] += m.Amount
				var pur database.TenantPurchase
				if s.db.First(&pur, *m.PurchaseID).Error == nil {
					row.DocNumber = pur.Series + "-" + pur.Number
				}
				report.ExpenseDetail = append(report.ExpenseDetail, row)
			} else {
				if m.Category == "gasto" || m.Category == "Gasto" {
					row.Type = "gasto"
				} else {
					row.Type = "egreso_manual"
				}
				row.Reference = m.Reference
				manualExpenseByMethod[paymentMethod] += m.Amount
				report.ExpenseDetail = append(report.ExpenseDetail, row)
			}
		}
	}

	report.Totals.FinalBalance = report.Session.OpeningBalance + report.Totals.TotalIncome - report.Totals.TotalExpense
	report.TotalsByMethod.Sales = mapToMethodTotals(salesByMethod)
	report.TotalsByMethod.Purchases = mapToMethodTotals(purchasesByMethod)
	for k, v := range manualIncomeByMethod {
		report.TotalsByMethod.Movements = append(report.TotalsByMethod.Movements, MethodTotal{Method: k, Total: v})
	}
	for k, v := range manualExpenseByMethod {
		report.TotalsByMethod.Movements = append(report.TotalsByMethod.Movements, MethodTotal{Method: k, Total: -v})
	}

	var voidMovements []database.TenantCashMovement
	s.db.Where("cash_session_id = ? AND type = ? AND category = ?", sessionID, "expense", "Anulación venta").
		Order("created_at ASC").
		Find(&voidMovements)
	for _, m := range voidMovements {
		row := CancelledSaleRow{
			Date:          m.CreatedAt,
			Amount:        m.Amount,
			PaymentMethod: normalizeMethod(m.PaymentMethod),
			Reason:        strings.TrimSpace(m.Notes),
		}
		if m.SaleID != nil {
			var sale database.TenantSale
			if s.db.First(&sale, *m.SaleID).Error == nil {
				row.DocNumber = sale.Number
				if row.Reason == "" && strings.Contains(sale.Notes, "ANULADA:") {
					row.Reason = sale.Notes
				}
			}
		}
		if row.DocNumber == "" {
			row.DocNumber = m.Reference
		}
		report.CancelledSalesDetail = append(report.CancelledSalesDetail, row)
	}

	return report, nil
}

func normalizeMethod(m string) string {
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

func mapToMethodTotals(m map[string]float64) []MethodTotal {
	out := make([]MethodTotal, 0, len(m))
	for k, v := range m {
		if v != 0 {
			out = append(out, MethodTotal{Method: k, Total: v})
		}
	}
	return out
}

// ListMovementsReport devuelve el listado de movimientos con filtros, total y resumen (ingresos/egresos en el rango filtrado).
func (s *CashBankService) ListMovementsReport(f MovementReportFilters) ([]MovementReportRow, int64, MovementReportSummary, error) {
	base := s.movementReportFilteredDB(f)
	var total int64
	if err := base.Count(&total).Error; err != nil {
		return nil, 0, MovementReportSummary{}, err
	}

	sumIn := s.sumMovementAmountByType(f, "income")
	sumEx := s.sumMovementAmountByType(f, "expense")
	summary := MovementReportSummary{
		TotalRows:   total,
		SumIncome:   sumIn,
		SumExpense:  sumEx,
		NetMovement: sumIn - sumEx,
	}

	q := s.movementReportFilteredDB(f).Select("tenant_cash_movements.*").Order("tenant_cash_movements.created_at DESC")
	if f.Limit > 0 {
		q = q.Offset(f.Offset).Limit(f.Limit)
	}

	var movements []database.TenantCashMovement
	if err := q.Find(&movements).Error; err != nil {
		return nil, 0, MovementReportSummary{}, err
	}

	sessionIDs := make(map[uint]struct{})
	userIDs := make(map[uint]struct{})
	saleIDs := make(map[uint]struct{})
	purchaseIDs := make(map[uint]struct{})
	for _, m := range movements {
		sessionIDs[m.CashSessionID] = struct{}{}
		userIDs[m.UserID] = struct{}{}
		if m.SaleID != nil {
			saleIDs[*m.SaleID] = struct{}{}
		}
		if m.PurchaseID != nil {
			purchaseIDs[*m.PurchaseID] = struct{}{}
		}
	}

	sessions := make(map[uint]database.TenantCashSession)
	if len(sessionIDs) > 0 {
		var list []database.TenantCashSession
		s.db.Where("id IN ?", keysUint(sessionIDs)).Find(&list)
		for _, se := range list {
			sessions[se.ID] = se
		}
	}

	branchIDs := make(map[uint]struct{})
	for _, ses := range sessions {
		branchIDs[ses.BranchID] = struct{}{}
	}
	branches := make(map[uint]string)
	if len(branchIDs) > 0 {
		var branchList []database.TenantBranch
		s.db.Where("id IN ?", keysUint(branchIDs)).Find(&branchList)
		for _, b := range branchList {
			branches[b.ID] = b.Name
		}
	}

	users := make(map[uint]string)
	if len(userIDs) > 0 {
		var userList []database.TenantUser
		s.db.Where("id IN ?", keysUint(userIDs)).Find(&userList)
		for _, u := range userList {
			users[u.ID] = u.Name
		}
	}

	sales := make(map[uint]database.TenantSale)
	if len(saleIDs) > 0 {
		var list []database.TenantSale
		s.db.Where("id IN ?", keysUint(saleIDs)).Find(&list)
		for _, sa := range list {
			sales[sa.ID] = sa
		}
	}
	contactsBySale := make(map[uint]string)
	for sid, sale := range sales {
		if sale.ContactID != nil {
			var c database.TenantContact
			if s.db.First(&c, *sale.ContactID).Error == nil {
				contactsBySale[sid] = contactDisplayName(c)
			}
		}
	}

	purchases := make(map[uint]database.TenantPurchase)
	if len(purchaseIDs) > 0 {
		var list []database.TenantPurchase
		s.db.Where("id IN ?", keysUint(purchaseIDs)).Find(&list)
		for _, p := range list {
			purchases[p.ID] = p
		}
	}
	contactsByPurchase := make(map[uint]string)
	for pid, pur := range purchases {
		if pur.ContactID != nil {
			var c database.TenantContact
			if s.db.First(&c, *pur.ContactID).Error == nil {
				contactsByPurchase[pid] = contactDisplayName(c)
			}
		}
	}

	salePayments := make(map[uint][]database.TenantSalePayment)
	if len(saleIDs) > 0 {
		var payList []database.TenantSalePayment
		s.db.Where("sale_id IN ?", keysUint(saleIDs)).Find(&payList)
		for _, p := range payList {
			salePayments[p.SaleID] = append(salePayments[p.SaleID], p)
		}
	}

	var rows []MovementReportRow
	for _, m := range movements {
		row := MovementReportRow{
			Date:          m.CreatedAt,
			Amount:        m.Amount,
			MovementID:    m.ID,
			CashSessionID: m.CashSessionID,
			Category:      m.Category,
			CashReference: m.Reference,
			NotesDetail:   m.Notes,
		}
		if m.Type == "expense" {
			row.Amount = -m.Amount
		}

		if ses, ok := sessions[m.CashSessionID]; ok {
			row.BranchName = branches[ses.BranchID]
		}
		row.UserName = users[m.UserID]
		row.PaymentMethod = normalizeMethod(m.PaymentMethod)
		if row.PaymentMethod == "" {
			row.PaymentMethod = "efectivo"
		}

		if m.SaleID != nil {
			if m.Type == "expense" {
				row.Type = "anulacion_venta"
			} else {
				row.Type = "venta"
			}
			if sale, ok := sales[*m.SaleID]; ok {
				row.DocNumber = sale.Number
				row.ContactName = contactsBySale[*m.SaleID]
				if pays := salePayments[*m.SaleID]; len(pays) > 0 {
					row.PaymentMethod = normalizeMethod(pays[0].Method)
				} else {
					row.PaymentMethod = normalizeMethod(sale.PaymentMethod)
				}
			}
		} else if m.PurchaseID != nil {
			row.Type = "compra"
			if pur, ok := purchases[*m.PurchaseID]; ok {
				row.DocNumber = pur.Series + "-" + pur.Number
				row.ContactName = contactsByPurchase[*m.PurchaseID]
			}
		} else if m.Type == "income" {
			row.Type = "ingreso"
			row.DocNumber = m.Reference
			row.ContactName = m.Notes
		} else {
			row.Type = "egreso"
			row.DocNumber = m.Reference
			row.ContactName = m.Notes
		}
		rows = append(rows, row)
	}
	return rows, total, summary, nil
}

// SessionProductSoldRow producto vendido agregado por sesión de caja.
type SessionProductSoldRow struct {
	ProductID   *uint   `json:"product_id"`
	Code        string  `json:"code"`
	Description string  `json:"description"`
	Quantity    float64 `json:"quantity"`
	Total       float64 `json:"total"`
}

// GetSessionProductsReport agrega ítems vendidos vinculados a una sesión de caja.
func (s *CashBankService) GetSessionProductsReport(sessionID uint) ([]SessionProductSoldRow, error) {
	var session database.TenantCashSession
	if err := s.db.First(&session, sessionID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("sesión de caja no encontrada")
		}
		return nil, err
	}
	var rows []SessionProductSoldRow
	err := s.db.Table("tenant_sale_items").
		Select(`tenant_sale_items.product_id, tenant_sale_items.code, tenant_sale_items.description,
			SUM(tenant_sale_items.quantity) AS quantity, SUM(tenant_sale_items.total) AS total`).
		Joins("JOIN tenant_sales ON tenant_sales.id = tenant_sale_items.sale_id").
		Where("tenant_sales.cash_session_id = ? AND tenant_sales.status NOT IN ?", sessionID, []string{"cancelled", "draft"}).
		Group("tenant_sale_items.product_id, tenant_sale_items.code, tenant_sale_items.description").
		Order("tenant_sale_items.description ASC").
		Scan(&rows).Error
	return rows, err
}

func keysUint(m map[uint]struct{}) []uint {
	keys := make([]uint, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func contactDisplayName(c database.TenantContact) string {
	if c.TradeName != "" {
		return c.TradeName
	}
	return c.BusinessName
}
