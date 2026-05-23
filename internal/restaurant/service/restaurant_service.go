package service

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"tukifac/internal/restaurant/staff"
	"tukifac/pkg/database"
	"tukifac/pkg/tax"
	cashbanksvc "tukifac/internal/cashbank/service"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type RestaurantService struct {
	db *gorm.DB
}

func New(db *gorm.DB) *RestaurantService {
	return &RestaurantService{db: db}
}

// restaurantLinePayableTotal es el importe a cobrar por una línea (cantidad × precio unitario)
// según tipo de afectación SUNAT y si el precio del catálogo incluye o no IGV.
func restaurantLinePayableTotal(db *gorm.DB, taxCfg tax.Config, productID *uint, unitPrice, quantity float64) float64 {
	affType := "10"
	priceIncludes := true
	if productID != nil {
		var p database.TenantProduct
		if db.First(&p, *productID).Error == nil {
			if p.IgvAffectationType != "" {
				affType = p.IgvAffectationType
			}
			priceIncludes = p.PriceIncludesIgv
		}
	}
	_, _, total := tax.CalcItem(unitPrice, quantity, 0, affType, priceIncludes, taxCfg)
	return total
}

// ============================= PISOS / SALAS =============================

func (s *RestaurantService) GetRestaurantSettings() (*database.TenantRestaurantSetting, error) {
	var cfg database.TenantRestaurantSetting
	if err := s.db.First(&cfg).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return &database.TenantRestaurantSetting{}, nil
		}
		return nil, err
	}
	return &cfg, nil
}

func isDeletionPinConfigured(stored string) bool {
	stored = strings.TrimSpace(stored)
	if stored == "" {
		return false
	}
	if strings.HasPrefix(stored, "$2a$") || strings.HasPrefix(stored, "$2b$") {
		return len(stored) > 20
	}
	return len(stored) >= 4
}

func (s *RestaurantService) HasDeletionPin() (bool, error) {
	cfg, err := s.GetRestaurantSettings()
	if err != nil {
		return false, err
	}
	return isDeletionPinConfigured(cfg.DeletionPin), nil
}

func (s *RestaurantService) SaveRestaurantSettings(deletionPin string) error {
	deletionPin = strings.TrimSpace(deletionPin)
	if len(deletionPin) < 4 {
		return errors.New("el PIN debe tener al menos 4 dígitos")
	}
	if len(deletionPin) > 6 {
		return errors.New("el PIN no puede tener más de 6 dígitos")
	}
	for _, r := range deletionPin {
		if r < '0' || r > '9' {
			return errors.New("el PIN solo puede contener dígitos")
		}
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(deletionPin), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	stored := string(hash)

	var cfg database.TenantRestaurantSetting
	if err := s.db.First(&cfg).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			cfg.DeletionPin = stored
			return s.db.Create(&cfg).Error
		}
		return err
	}
	return s.db.Model(&cfg).Update("deletion_pin", stored).Error
}

// VerifyDeletionPin verifica que el PIN coincida con el configurado. Si no hay PIN configurado, retorna error pidiendo configurarlo.
func (s *RestaurantService) VerifyDeletionPin(pin string) error {
	pin = strings.TrimSpace(pin)
	if pin == "" {
		return errors.New("se requiere el PIN de seguridad")
	}
	cfg, err := s.GetRestaurantSettings()
	if err != nil {
		return err
	}
	stored := strings.TrimSpace(cfg.DeletionPin)
	if !isDeletionPinConfigured(stored) {
		return errors.New("configure el PIN de seguridad en Ajustes del Restaurante (panel tenant) antes de anular pedidos o comandas")
	}
	if strings.HasPrefix(stored, "$2a$") || strings.HasPrefix(stored, "$2b$") {
		if bcrypt.CompareHashAndPassword([]byte(stored), []byte(pin)) != nil {
			return errors.New("PIN incorrecto")
		}
		return nil
	}
	// Compatibilidad con PIN en texto plano guardado antes del hash.
	if stored != pin {
		return errors.New("PIN incorrecto")
	}
	return nil
}

// ============================= PISOS / SALAS =============================

func (s *RestaurantService) ListFloors(branchID uint) ([]database.TenantRestaurantFloor, error) {
	var floors []database.TenantRestaurantFloor
	q := s.db.Where("active = ?", true)
	if branchID > 0 {
		q = q.Where("branch_id = ?", branchID)
	}
	err := q.Order("sort_order ASC, name ASC").Find(&floors).Error
	return floors, err
}

func (s *RestaurantService) CreateFloor(branchID uint, name string, order int) (*database.TenantRestaurantFloor, error) {
	if name == "" {
		return nil, errors.New("el nombre del piso es requerido")
	}
	if branchID == 0 {
		return nil, errors.New("sucursal requerida")
	}
	f := &database.TenantRestaurantFloor{BranchID: branchID, Name: name, SortOrder: order, Active: true}
	err := s.db.Create(f).Error
	return f, err
}

func (s *RestaurantService) UpdateFloor(id uint, name string, order int, active bool) error {
	return s.db.Model(&database.TenantRestaurantFloor{}).Where("id = ?", id).Updates(map[string]interface{}{
		"name": name, "sort_order": order, "active": active,
	}).Error
}

func (s *RestaurantService) DeleteFloor(id uint) error {
	var count int64
	s.db.Model(&database.TenantRestaurantTable{}).Where("floor_id = ? AND active = ?", id, true).Count(&count)
	if count > 0 {
		return errors.New("no se puede eliminar un piso con mesas activas")
	}
	return s.db.Delete(&database.TenantRestaurantFloor{}, id).Error
}

// ============================= MESAS =============================

type TableWithSession struct {
	database.TenantRestaurantTable
	FloorName   string  `json:"floor_name"`
	SessionID   *uint   `json:"session_id"`
	TotalAmount float64 `json:"total_amount"`
	WaiterName  string  `json:"waiter_name"`
}

func (s *RestaurantService) ListTables(branchID, floorID uint) ([]TableWithSession, error) {
	type raw struct {
		database.TenantRestaurantTable
		FloorName   string  `gorm:"column:floor_name"`
		SessionID   *uint   `gorm:"column:session_id"`
		TotalAmount float64 `gorm:"column:total_amount"`
		WaiterName  string  `gorm:"column:waiter_name"`
	}
	var rows []raw
	q := s.db.Table("tenant_restaurant_tables t").
		Select("t.*, f.name AS floor_name, ts.id AS session_id, COALESCE(ts.total_amount,0) AS total_amount, COALESCE(NULLIF(st.display_name,''), u.name, '') AS waiter_name").
		Joins("JOIN tenant_restaurant_floors f ON f.id = t.floor_id").
		Joins("LEFT JOIN tenant_table_sessions ts ON ts.table_id = t.id AND ts.status = 'open'").
		Joins("LEFT JOIN tenant_restaurant_staff st ON st.id = ts.staff_id").
		Joins("LEFT JOIN tenant_users u ON u.id = st.user_id").
		Where("t.active = ?", true)
	if branchID > 0 {
		q = q.Where("t.branch_id = ?", branchID)
	}
	if floorID > 0 {
		q = q.Where("t.floor_id = ?", floorID)
	}
	q.Order("f.sort_order ASC, t.name ASC").Scan(&rows)

	result := make([]TableWithSession, len(rows))
	for i, r := range rows {
		result[i] = TableWithSession{
			TenantRestaurantTable: r.TenantRestaurantTable,
			FloorName:             r.FloorName,
			SessionID:             r.SessionID,
			TotalAmount:           r.TotalAmount,
			WaiterName:            r.WaiterName,
		}
	}
	return result, nil
}

func (s *RestaurantService) CreateTable(branchID, floorID uint, name string, capacity int) (*database.TenantRestaurantTable, error) {
	if branchID == 0 || floorID == 0 || name == "" {
		return nil, errors.New("sucursal, piso y nombre son requeridos")
	}
	var floor database.TenantRestaurantFloor
	if err := s.db.Where("id = ? AND branch_id = ?", floorID, branchID).First(&floor).Error; err != nil {
		return nil, errors.New("piso no pertenece a esta sucursal")
	}
	t := &database.TenantRestaurantTable{BranchID: branchID, FloorID: floorID, Name: name, Capacity: capacity, Status: "libre", Active: true}
	err := s.db.Create(t).Error
	return t, err
}

func (s *RestaurantService) UpdateTable(id uint, name string, capacity int, active bool) error {
	return s.db.Model(&database.TenantRestaurantTable{}).Where("id = ?", id).Updates(map[string]interface{}{
		"name": name, "capacity": capacity, "active": active,
	}).Error
}

func (s *RestaurantService) DeleteTable(id uint) error {
	var sess database.TenantTableSession
	if s.db.Where("table_id = ? AND status = 'open'", id).First(&sess).Error == nil {
		return errors.New("la mesa tiene una sesión activa, no se puede eliminar")
	}
	return s.db.Delete(&database.TenantRestaurantTable{}, id).Error
}

// ============================= SESIONES DE MESA =============================

type SessionDetail struct {
	database.TenantTableSession
	TableName   string        `json:"table_name"`
	FloorName   string        `json:"floor_name"`
	WaiterName  string        `json:"waiter_name"`
	DriverName  string        `json:"driver_name"`
	ContactName string        `json:"contact_name"`
	Orders      []OrderDetail `json:"orders"`
}

type OrderDetail struct {
	database.TenantTableOrder
	Comandas []database.TenantComanda `json:"comandas"`
}

func (s *RestaurantService) staffDisplayName(staffID *uint) string {
	if staffID == nil || *staffID == 0 {
		return ""
	}
	st, err := staff.New(s.db).GetStaffByID(*staffID)
	if err != nil {
		return ""
	}
	return staff.New(s.db).StaffDisplayName(st)
}

func (s *RestaurantService) OpenTable(tableID *uint, staffID *uint, branchID, userID uint, guests int, notes string) (*database.TenantTableSession, error) {
	return s.OpenTableExtended(OpenSessionInput{
		TableID: tableID, StaffID: staffID, BranchID: branchID, UserID: userID,
		Guests: guests, Notes: notes,
	})
}

func (s *RestaurantService) GetSessionDetail(sessionID uint) (*SessionDetail, error) {
	var sess database.TenantTableSession
	if err := s.db.First(&sess, sessionID).Error; err != nil {
		return nil, errors.New("sesión no encontrada")
	}

	detail := &SessionDetail{TenantTableSession: sess}

	// Datos de mesa y mozo
	if sess.TableID != nil {
		var table database.TenantRestaurantTable
		if s.db.First(&table, *sess.TableID).Error == nil {
			detail.TableName = table.Name
			// piso
			var floor database.TenantRestaurantFloor
			if s.db.First(&floor, table.FloorID).Error == nil {
				detail.FloorName = floor.Name
			}
		}
	}
	if sess.StaffID != nil {
		detail.WaiterName = s.staffDisplayName(sess.StaffID)
	}
	if sess.DeliveryDriverID != nil {
		var d database.TenantDeliveryDriver
		if s.db.First(&d, *sess.DeliveryDriverID).Error == nil {
			detail.DriverName = d.Name
		}
	}
	if sess.ContactID != nil {
		var c database.TenantContact
		if s.db.First(&c, *sess.ContactID).Error == nil {
			detail.ContactName = c.BusinessName
			if detail.CustomerName == "" {
				detail.CustomerName = c.BusinessName
			}
		}
	}

	// Pedidos con comandas
	var orders []database.TenantTableOrder
	s.db.Where("session_id = ? AND status = 'active'", sessionID).Order("order_number ASC").Find(&orders)
	detail.Orders = make([]OrderDetail, 0, len(orders))
	for _, o := range orders {
		var comandas []database.TenantComanda
		s.db.Where("order_id = ?", o.ID).Order("created_at ASC").Find(&comandas)
		detail.Orders = append(detail.Orders, OrderDetail{TenantTableOrder: o, Comandas: comandas})
	}

	return detail, nil
}

func (s *RestaurantService) GetActiveSessionByTable(tableID uint) (*database.TenantTableSession, error) {
	var sess database.TenantTableSession
	err := s.db.Where("table_id = ? AND status = 'open'", tableID).First(&sess).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &sess, err
}

// ============================= PEDIDOS Y COMANDAS =============================

type NewOrderItem struct {
	ProductID   *uint   `json:"product_id"`
	ProductCode string  `json:"product_code"`
	ProductName string  `json:"product_name"`
	Quantity    float64 `json:"quantity"`
	UnitPrice   float64 `json:"unit_price"`
	Notes       string  `json:"notes"`
}

func (s *RestaurantService) AddOrder(sessionID uint, staffID *uint, userID uint, items []NewOrderItem, notes string) (*OrderDetail, error) {
	if len(items) == 0 {
		return nil, errors.New("el pedido debe tener al menos un ítem")
	}
	var sess database.TenantTableSession
	if err := s.db.First(&sess, sessionID).Error; err != nil {
		return nil, errors.New("sesión no encontrada")
	}
	if sess.Status != "open" {
		return nil, errors.New("la sesión ya está cerrada")
	}

	// Siguiente número de pedido
	var lastOrder database.TenantTableOrder
	var nextNum int = 1
	if s.db.Where("session_id = ?", sessionID).Order("order_number DESC").First(&lastOrder).Error == nil {
		nextNum = lastOrder.OrderNumber + 1
	}

	if staffID == nil && sess.StaffID != nil {
		staffID = sess.StaffID
	}

	order := &database.TenantTableOrder{
		SessionID:   sessionID,
		StaffID:     staffID,
		UserID:      userID,
		OrderNumber: nextNum,
		Notes:       notes,
		Status:      "active",
	}

	var comandas []database.TenantComanda
	var sessionTotal float64

	err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(order).Error; err != nil {
			return err
		}
		taxCfg := tax.LoadFromDB(tx)
		for _, item := range items {
			// Cada ítem es una comanda independiente (incluso si el producto es el mismo)
			c := database.TenantComanda{
				OrderID:     order.ID,
				SessionID:   sessionID,
				ProductID:   item.ProductID,
				ProductCode: item.ProductCode,
				ProductName: item.ProductName,
				Quantity:    item.Quantity,
				UnitPrice:   item.UnitPrice,
				Notes:       item.Notes,
				Status:      "pendiente",
			}
			if err := tx.Create(&c).Error; err != nil {
				return err
			}
			comandas = append(comandas, c)
			sessionTotal += restaurantLinePayableTotal(tx, taxCfg, item.ProductID, item.UnitPrice, item.Quantity)
		}

		// Actualizar total acumulado de la sesión
		tx.Model(&database.TenantTableSession{}).Where("id = ?", sessionID).
			UpdateColumn("total_amount", gorm.Expr("total_amount + ?", sessionTotal))

		now := time.Now()
		sessUpdates := map[string]interface{}{
			"order_status": OrderStatusSentToKitchen,
		}
		if sess.SentToKitchenAt == nil {
			sessUpdates["sent_to_kitchen_at"] = now
		}
		if sess.OrderStatus == OrderStatusDraft || sess.OrderStatus == OrderStatusPending || sess.OrderStatus == "" {
			sessUpdates["order_status"] = OrderStatusSentToKitchen
		}
		tx.Model(&database.TenantTableSession{}).Where("id = ?", sessionID).Updates(sessUpdates)
		return s.syncSessionOrderStatus(tx, sessionID)
	})

	if err != nil {
		return nil, err
	}

	return &OrderDetail{TenantTableOrder: *order, Comandas: comandas}, nil
}

// UpdateComandaStatus cambia el estado de una comanda.
func (s *RestaurantService) UpdateComandaStatus(id uint, status string, userID uint) error {
	validStatuses := map[string]bool{
		"pendiente": true, "preparacion": true, "lista": true, "entregada": true,
	}
	if !validStatuses[status] {
		return errors.New("estado inválido: usa pendiente, preparacion, lista o entregada")
	}
	err := s.db.Model(&database.TenantComanda{}).Where("id = ?", id).Update("status", status).Error
	if err != nil {
		return err
	}
	var c database.TenantComanda
	if s.db.First(&c, id).Error == nil {
		return s.syncSessionOrderStatus(s.db, c.SessionID)
	}
	return nil
}

// CancelComanda anula una comanda (solo admin).
func (s *RestaurantService) CancelComanda(id uint, reason string, cancelledByID uint) error {
	var c database.TenantComanda
	if err := s.db.First(&c, id).Error; err != nil {
		return errors.New("comanda no encontrada")
	}
	if c.CancelledAt != nil {
		return errors.New("la comanda ya fue anulada")
	}
	if c.Status == "entregada" {
		return errors.New("no se puede anular una comanda ya entregada")
	}
	now := time.Now()
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&c).Updates(map[string]interface{}{
			"cancelled_at":    now,
			"cancelled_by_id": cancelledByID,
			"cancel_reason":   reason,
			"status":          "entregada", // marca como procesada para que no aparezca en cocina
		}).Error; err != nil {
			return err
		}
		// Restar del total de la sesión (mismo criterio tributario que al agregar la comanda)
		taxCfg := tax.LoadFromDB(tx)
		deduct := restaurantLinePayableTotal(tx, taxCfg, c.ProductID, c.UnitPrice, c.Quantity)
		tx.Model(&database.TenantTableSession{}).Where("id = ?", c.SessionID).
			UpdateColumn("total_amount", gorm.Expr("GREATEST(0, total_amount - ?)", deduct))
		return nil
	})
}

// MarkComandaPrinted marca una comanda como impresa.
func (s *RestaurantService) MarkComandaPrinted(id uint) error {
	now := time.Now()
	return s.db.Model(&database.TenantComanda{}).Where("id = ?", id).Updates(map[string]interface{}{
		"printed": true, "printed_at": now,
	}).Error
}

// GetKitchenComandas retorna comandas para la vista de cocina/comandas.
// Solo incluye comandas de sesiones ABIERTAS (mesas aún no cerradas), en los 4 estados.
// Así no aparecen "fantasmas" de mesas ya cerradas o cobradas.
func (s *RestaurantService) GetKitchenComandas(branchID uint) ([]database.TenantComanda, error) {
	var comandas []database.TenantComanda
	err := s.db.Joins("JOIN tenant_table_sessions ts ON ts.id = tenant_comandas.session_id").
		Where("ts.branch_id = ? AND ts.status = ? AND tenant_comandas.status IN ('pendiente','preparacion','lista','entregada') AND tenant_comandas.cancelled_at IS NULL", branchID, "open").
		Order("tenant_comandas.created_at ASC").
		Find(&comandas).Error
	return comandas, err
}

// ============================= COBRO Y CIERRE =============================

type BillInput struct {
	SessionID     uint
	UserID        uint
	EmployeeType  string // staff restaurante: bloquea efectivo a waiter
	SeriesID      uint
	DocType       string
	IssueDate     time.Time
	Currency      string
	ContactID     *uint
	Payments      []PaymentInput
	CashSessionID *uint
	CloseSession  bool // si es false, genera la venta pero no cierra la mesa (cliente puede seguir consumiendo)
}

type PaymentInput struct {
	Method    string  `json:"method"`
	Amount    float64 `json:"amount"`
	Reference string  `json:"reference"`
	Notes     string  `json:"notes"`
}

// BillTable cierra la sesión, genera una venta formal y registra los pagos.
func (s *RestaurantService) BillTable(input BillInput, taxCfg tax.Config) (*database.TenantSale, error) {
	var sess database.TenantTableSession
	if err := s.db.First(&sess, input.SessionID).Error; err != nil {
		return nil, errors.New("sesión no encontrada")
	}
	if sess.Status != "open" {
		return nil, errors.New("la sesión ya está cerrada o facturada")
	}

	// Comandas a facturar.
	// Cierre total (POS / mesa): incluye todos los estados de cocina; "entregada" = servido, no facturado aún.
	// Cobro parcial (mesa sigue abierta): solo ítems aún no facturados en un cobro anterior.
	q := s.db.Where("session_id = ? AND cancelled_at IS NULL", input.SessionID)
	if !input.CloseSession {
		q = q.Where("status != ?", "entregada")
	}
	var comandas []database.TenantComanda
	if err := q.Find(&comandas).Error; err != nil {
		return nil, err
	}
	if len(comandas) == 0 {
		return nil, errors.New("no hay ítems para facturar en esta sesión")
	}

	resolvedCash, err := s.resolveCashSessionForPayments(sess.BranchID, input.UserID, input.EmployeeType, input.CashSessionID, input.Payments)
	if err != nil {
		return nil, err
	}
	input.CashSessionID = resolvedCash

	// Obtener la serie
	var series database.TenantDocumentSeries
	if err := s.db.First(&series, input.SeriesID).Error; err != nil {
		return nil, errors.New("serie no encontrada")
	}

	// Validar total de pagos
	var totalPaid float64
	for _, p := range input.Payments {
		totalPaid += p.Amount
	}
	roundedSession := roundFloat(sess.TotalAmount)
	roundedPaid := roundFloat(totalPaid)
	if roundedPaid < roundedSession {
		return nil, fmt.Errorf("monto pagado (%.2f) es menor al total de la sesión (%.2f)", totalPaid, sess.TotalAmount)
	}

	// Construir ítems de venta desde las comandas (con tipo de afectación IGV para Lycet)
	type saleItemData struct {
		ProductID          *uint
		Code               string
		Description        string
		Unit               string
		Quantity           float64
		UnitPrice          float64
		TaxRate            float64
		IgvAffectationType string
		PriceIncludesIgv   bool
	}
	itemMap := make(map[string]*saleItemData)
	for _, c := range comandas {
		key := fmt.Sprintf("%d_%s", func() uint {
			if c.ProductID != nil {
				return *c.ProductID
			}
			return 0
		}(), c.ProductName)
		if existing, ok := itemMap[key]; ok {
			existing.Quantity += c.Quantity
		} else {
			affType := "10"
			priceIncludesIgv := true
			if c.ProductID != nil {
				var p database.TenantProduct
				if s.db.First(&p, *c.ProductID).Error == nil {
					if p.IgvAffectationType != "" {
						affType = p.IgvAffectationType
					}
					priceIncludesIgv = p.PriceIncludesIgv
				}
			}
			itemMap[key] = &saleItemData{
				ProductID:          c.ProductID,
				Code:               c.ProductCode,
				Description:        c.ProductName,
				Unit:               "NIU",
				Quantity:           c.Quantity,
				UnitPrice:          c.UnitPrice,
				TaxRate:            taxCfg.EffectiveRate(affType),
				IgvAffectationType: affType,
				PriceIncludesIgv:   priceIncludesIgv,
			}
		}
	}

	// Calcular totales
	var subtotal, taxAmount, total float64
	var saleItems []database.TenantSaleItem
	for _, item := range itemMap {
		iSub, iTax, iTotal := tax.CalcItem(item.UnitPrice, item.Quantity, 0, item.IgvAffectationType, item.PriceIncludesIgv, taxCfg)
		subtotal += iSub; taxAmount += iTax; total += iTotal
		saleItems = append(saleItems, database.TenantSaleItem{
			ProductID:          item.ProductID,
			Code:               item.Code,
			Description:        item.Description,
			Unit:               item.Unit,
			Quantity:           item.Quantity,
			UnitPrice:          item.UnitPrice,
			TaxRate:            item.TaxRate,
			IgvAffectationType: item.IgvAffectationType,
			Subtotal:           iSub,
			TaxAmount:          iTax,
			Total:              iTotal,
		})
	}

	// Generar número de venta
	correlative := series.Correlative
	saleNumber := fmt.Sprintf("%s-%08d", series.Series, correlative)
	currency := input.Currency
	if currency == "" {
		currency = "PEN"
	}

	sale := &database.TenantSale{
		BranchID:            sess.BranchID,
		UserID:              input.UserID,
		ContactID:           input.ContactID,
		RestaurantSessionID: &input.SessionID,
		CashSessionID:       input.CashSessionID,
		SeriesID:      input.SeriesID,
		DocType:       input.DocType,
		Series:        series.Series,
		Correlative:   correlative,
		Number:        saleNumber,
		IssueDate:     input.IssueDate,
		Subtotal:      subtotal,
		TaxAmount:     taxAmount,
		Total:         total,
		Currency:      currency,
		PaymentMethod: input.Payments[0].Method, // método principal
		Status:        "paid",
		BillingStatus: "pending",
	}

	now := time.Now()
	return sale, s.db.Transaction(func(tx *gorm.DB) error {
		// Incrementar correlativo
		tx.Model(&series).Update("correlative", series.Correlative+1)

		if err := tx.Create(sale).Error; err != nil {
			return err
		}
		for i := range saleItems {
			saleItems[i].SaleID = sale.ID
		}
		if err := tx.Create(&saleItems).Error; err != nil {
			return err
		}

		// Descontar stock y registrar kardex para productos con control de stock
		for _, item := range saleItems {
			if item.ProductID == nil {
				continue
			}
			var product database.TenantProduct
			if tx.First(&product, *item.ProductID).Error != nil {
				continue
			}
			if !product.ManageStock {
				continue
			}
			var stock database.TenantProductStock
			tx.Where("product_id = ? AND branch_id = ?", *item.ProductID, sess.BranchID).First(&stock)
			newQty := stock.Quantity - item.Quantity
			if stock.ID == 0 {
				tx.Create(&database.TenantProductStock{
					ProductID: *item.ProductID,
					BranchID:  sess.BranchID,
					Quantity:  newQty,
				})
			} else {
				tx.Model(&stock).Update("quantity", newQty)
			}
			tx.Create(&database.TenantStockMovement{
				ProductID: *item.ProductID,
				BranchID:  sess.BranchID,
				Type:      "out",
				Quantity:  item.Quantity,
				Balance:   newQty,
				Reference: "VENTA/" + sale.Number,
				UserID:    input.UserID,
				CreatedAt: now,
			})
		}

		// Registrar pagos múltiples: distribuir a caja o cuenta bancaria según método
		cbSvc := cashbanksvc.NewCashBankService(s.db)
		for _, p := range input.Payments {
			tx.Create(&database.TenantSalePayment{
				SaleID:    sale.ID,
				Method:    p.Method,
				Amount:    p.Amount,
				Reference: p.Reference,
				Notes:     p.Notes,
			})
			desc := "Venta " + sale.Number
			_ = cbSvc.RecordPayment(tx, p.Method, p.Amount, input.CashSessionID, sale.Number, desc, &sale.ID, input.UserID)
		}

		if input.CloseSession {
			// Cerrar sesión y liberar mesa
			tx.Model(&sess).Updates(map[string]interface{}{
				"status": "billed", "closed_at": now, "sale_id": sale.ID,
				"order_status": OrderStatusPaid, "paid_at": now,
			})
			if sess.TableID != nil {
				tx.Model(&database.TenantRestaurantTable{}).Where("id = ?", *sess.TableID).
					Update("status", "libre")
			}
			// Eliminar comandas de la sesión cerrada (ya facturadas en la venta; no aparecen en cocina)
			tx.Where("session_id = ?", input.SessionID).Delete(&database.TenantComanda{})
		} else {
			// Generar venta pero mantener mesa abierta: descontar lo facturado del total de la sesión
			tx.Model(&sess).UpdateColumn("total_amount", gorm.Expr("GREATEST(0, total_amount - ?)", total))
			// Marcar solo las comandas facturadas en este cobro (evita doble facturación en cobros parciales)
			billedIDs := make([]uint, 0, len(comandas))
			for _, c := range comandas {
				billedIDs = append(billedIDs, c.ID)
			}
			if len(billedIDs) > 0 {
				tx.Model(&database.TenantComanda{}).Where("id IN ?", billedIDs).
					Update("status", "entregada")
			}
		}

		return nil
	})
}

// CancelSession anula un pedido abierto: exige PIN, sin venta asociada, elimina registros (hard delete).
func (s *RestaurantService) CancelSession(sessionID uint, pin, reason string, userID uint) error {
	_ = userID
	if err := s.VerifyDeletionPin(pin); err != nil {
		return err
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return errors.New("indique el motivo de anulación")
	}

	var sess database.TenantTableSession
	if err := s.db.First(&sess, sessionID).Error; err != nil {
		return errors.New("pedido no encontrado")
	}
	if sess.Status != "open" {
		return errors.New("solo se pueden anular pedidos abiertos")
	}
	if sess.SaleID != nil {
		return errors.New("no se puede anular: el pedido ya fue facturado")
	}

	return s.db.Transaction(func(tx *gorm.DB) error {
		var orderIDs []uint
		if err := tx.Model(&database.TenantTableOrder{}).
			Where("session_id = ?", sessionID).
			Pluck("id", &orderIDs).Error; err != nil {
			return err
		}

		comandaQ := tx.Where("session_id = ?", sessionID)
		if len(orderIDs) > 0 {
			comandaQ = tx.Where("session_id = ? OR order_id IN ?", sessionID, orderIDs)
		}
		if err := comandaQ.Delete(&database.TenantComanda{}).Error; err != nil {
			return err
		}
		if err := tx.Where("session_id = ?", sessionID).Delete(&database.TenantTableOrder{}).Error; err != nil {
			return err
		}
		if err := tx.Delete(&database.TenantTableSession{}, sessionID).Error; err != nil {
			return err
		}
		if sess.TableID != nil {
			tx.Model(&database.TenantRestaurantTable{}).Where("id = ?", *sess.TableID).
				Update("status", "libre")
		}
		return nil
	})
}

// CloseSessionOnly cierra la sesión y libera la mesa sin generar venta (ej. mesa ya pagada, solo liberar).
func (s *RestaurantService) CloseSessionOnly(sessionID uint) error {
	var sess database.TenantTableSession
	if err := s.db.First(&sess, sessionID).Error; err != nil {
		return errors.New("sesión no encontrada")
	}
	if sess.Status != "open" {
		return errors.New("la sesión no está abierta")
	}
	now := time.Now()
	return s.db.Transaction(func(tx *gorm.DB) error {
		tx.Model(&sess).Updates(map[string]interface{}{
			"status": "closed", "closed_at": now,
		})
		if sess.TableID != nil {
			tx.Model(&database.TenantRestaurantTable{}).Where("id = ?", *sess.TableID).
				Update("status", "libre")
		}
		return nil
	})
}

// ============================= PAGOS MÚLTIPLES (ventas generales) =============================

// RegisterPayments registra uno o más pagos para una venta existente.
func (s *RestaurantService) RegisterPayments(saleID uint, payments []PaymentInput, userID uint) error {
	var sale database.TenantSale
	if err := s.db.First(&sale, saleID).Error; err != nil {
		return errors.New("venta no encontrada")
	}

	var totalPaid float64
	// Sumar pagos ya existentes
	var existing []database.TenantSalePayment
	s.db.Where("sale_id = ?", saleID).Find(&existing)
	for _, p := range existing {
		totalPaid += p.Amount
	}
	for _, p := range payments {
		totalPaid += p.Amount
	}

	if roundFloat(totalPaid) < roundFloat(sale.Total) {
		return fmt.Errorf("el total pagado (%.2f) es menor al total de la venta (%.2f)", totalPaid, sale.Total)
	}

	cbSvc := cashbanksvc.NewCashBankService(s.db)
	return s.db.Transaction(func(tx *gorm.DB) error {
		for _, p := range payments {
			tx.Create(&database.TenantSalePayment{
				SaleID:    saleID,
				Method:    p.Method,
				Amount:    p.Amount,
				Reference: p.Reference,
				Notes:     p.Notes,
			})
			desc := "Venta " + sale.Number
			_ = cbSvc.RecordPayment(tx, p.Method, p.Amount, sale.CashSessionID, sale.Number, desc, &sale.ID, userID)
		}
		// Actualizar método de pago principal en la venta
		if len(payments) > 0 {
			tx.Model(&sale).Update("payment_method", payments[0].Method)
		}
		return nil
	})
}

func (s *RestaurantService) GetSalePayments(saleID uint) ([]database.TenantSalePayment, error) {
	var payments []database.TenantSalePayment
	err := s.db.Where("sale_id = ?", saleID).Order("created_at ASC").Find(&payments).Error
	return payments, err
}

// ============================= HELPERS =============================

func (s *RestaurantService) resolveCashSessionForPayments(
	branchID, userID uint,
	employeeType string,
	cashSessionID *uint,
	payments []PaymentInput,
) (*uint, error) {
	cbSvc := cashbanksvc.NewCashBankService(s.db)
	needsCash := false
	for _, p := range payments {
		if p.Amount <= 0 {
			continue
		}
		pm, err := cbSvc.GetPaymentMethodByCode(p.Method)
		if err == nil && pm != nil && pm.DestinationType == "cash" {
			needsCash = true
			break
		}
		if strings.EqualFold(strings.TrimSpace(p.Method), "cash") || strings.EqualFold(strings.TrimSpace(p.Method), "efectivo") {
			needsCash = true
			break
		}
	}
	if !needsCash {
		return cashSessionID, nil
	}
	et := strings.ToLower(strings.TrimSpace(employeeType))
	if et == "waiter" || et == "mozo" {
		return nil, errors.New("los mozos no pueden cobrar en efectivo; use otro método de pago o un cajero")
	}
	var sid uint
	if cashSessionID != nil && *cashSessionID > 0 {
		sid = *cashSessionID
	} else {
		sess, err := cbSvc.GetOpenSession(branchID, userID)
		if err != nil {
			return nil, err
		}
		if sess == nil {
			return nil, errors.New("debe abrir su caja para cobrar en efectivo")
		}
		sid = sess.ID
	}
	if _, err := cbSvc.ValidateCashSessionForUser(sid, userID, branchID); err != nil {
		return nil, err
	}
	return &sid, nil
}

func roundFloat(f float64) float64 {
	return float64(int(f*100+0.5)) / 100
}
