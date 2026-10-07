package service

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"tukifac/pkg/database"

	"gorm.io/gorm"
)

// OrderItemView línea del pedido con datos del catálogo y la marca de precio distinto al de lista.
type OrderItemView struct {
	database.EquipOrderItem
	ProductName    string      `json:"product_name"`
	ReferencePrice float64     `json:"reference_price"`
	PriceDeviation float64     `json:"price_deviation"` // fracción respecto al precio de lista (0.35 = 35 % distinto); 0 si no aplica
	Components     []comboSnap `json:"components,omitempty"`
}

// PaymentLine cobro aplicado a un pedido.
type PaymentLine struct {
	PaymentID uint      `json:"payment_id"`
	PaidAt    time.Time `json:"paid_at"`
	Method    string    `json:"method"`
	Moment    string    `json:"moment"`
	Reference string    `json:"reference"`
	Invoice   string    `json:"invoice"`
	Amount    float64   `json:"amount"` // lo aplicado a ESTE pedido
	Total     float64   `json:"payment_total"`
	Status    string    `json:"status"`
	Notes     string    `json:"notes"`
}

// TimelineEvent hito del pedido (cada fecha es un dato distinto: ver §5.5 de la propuesta).
type TimelineEvent struct {
	At     time.Time `json:"at"`
	Kind   string    `json:"kind"`
	Label  string    `json:"label"`
	Future bool      `json:"future,omitempty"`
}

// OrderView pedido completo para la pantalla.
type OrderView struct {
	database.EquipOrder
	Items    []OrderItemView         `json:"items"`
	Customer *database.EquipCustomer `json:"customer"`
	Payments []PaymentLine           `json:"payments"`
	Shipment *ShipmentView           `json:"shipment"`
	Packing  []PackingLine           `json:"packing"`
	Timeline []TimelineEvent         `json:"timeline"`
}

func invoiceText(p database.EquipPayment) string {
	if p.InvoiceSeries == "" && p.InvoiceNumber == "" {
		return ""
	}
	return strings.TrimSpace(p.InvoiceSeries + "-" + p.InvoiceNumber)
}

func (s *Service) GetOrder(id uint) (*OrderView, error) {
	var o database.EquipOrder
	if found, err := findOne(s.db, &o, "id = ?", id); err != nil {
		return nil, err
	} else if !found {
		return nil, invalid("pedido no encontrado")
	}
	v := &OrderView{EquipOrder: o, Payments: []PaymentLine{}}

	var items []database.EquipOrderItem
	if err := s.db.Where("order_id = ?", id).Order("line_no ASC, id ASC").Find(&items).Error; err != nil {
		return nil, err
	}
	prodIDs := []uint{}
	for _, it := range items {
		if it.ProductID != nil {
			prodIDs = append(prodIDs, *it.ProductID)
		}
	}
	prods := map[uint]database.EquipProduct{}
	if len(prodIDs) > 0 {
		var ps []database.EquipProduct
		if err := s.db.Where("id IN ?", prodIDs).Find(&ps).Error; err != nil {
			return nil, err
		}
		for _, p := range ps {
			prods[p.ID] = p
		}
	}
	for _, it := range items {
		iv := OrderItemView{EquipOrderItem: it}
		if it.ProductID != nil {
			p := prods[*it.ProductID]
			iv.ProductName, iv.ReferencePrice = p.Name, p.ReferencePrice
			if p.ReferencePrice > 0 && !it.IsCourtesy {
				if dev := math.Abs(it.UnitPrice-p.ReferencePrice) / p.ReferencePrice; dev > 0.3 {
					iv.PriceDeviation = math.Round(dev*100) / 100
				}
			}
		}
		if it.LineType == "combo" {
			_ = json.Unmarshal([]byte(it.ComboSnapshotJSON), &iv.Components)
		}
		v.Items = append(v.Items, iv)
	}
	v.Packing = s.packingFor(s.db, items)

	if o.CustomerID != nil {
		var c database.EquipCustomer
		if found, _ := findOne(s.db, &c, "id = ?", *o.CustomerID); found {
			v.Customer = &c
		}
	}

	type allocRow struct {
		PaymentID uint
		Amount    float64
	}
	var allocs []allocRow
	if err := s.db.Table("equip_payment_allocations").Select("payment_id, amount").Where("order_id = ?", id).Scan(&allocs).Error; err != nil {
		return nil, err
	}
	if len(allocs) > 0 {
		pids := make([]uint, 0, len(allocs))
		amt := map[uint]float64{}
		for _, a := range allocs {
			pids = append(pids, a.PaymentID)
			amt[a.PaymentID] += a.Amount
		}
		var pays []database.EquipPayment
		if err := s.db.Where("id IN ?", pids).Order("paid_at ASC, id ASC").Find(&pays).Error; err != nil {
			return nil, err
		}
		for _, p := range pays {
			v.Payments = append(v.Payments, PaymentLine{PaymentID: p.ID, PaidAt: p.PaidAt, Method: p.Method, Moment: p.Moment, Reference: p.Reference,
				Invoice: invoiceText(p), Amount: amt[p.ID], Total: p.Amount, Status: p.Status, Notes: p.Notes})
		}
	}

	st, err := s.GetSettings()
	if err != nil {
		return nil, err
	}
	var sh database.EquipShipment
	if found, err := findOne(s.db, &sh, "order_id = ? AND is_current = ?", id, true); err != nil {
		return nil, err
	} else if found {
		sv := s.shipmentView(s.db, sh, st)
		v.Shipment = &sv
	}
	v.Timeline = buildTimeline(v)
	return v, nil
}

func buildTimeline(v *OrderView) []TimelineEvent {
	var ev []TimelineEvent
	add := func(at *time.Time, kind, label string) {
		if at != nil && !at.IsZero() {
			ev = append(ev, TimelineEvent{At: *at, Kind: kind, Label: label, Future: at.After(time.Now().Add(24 * time.Hour))})
		}
	}
	od := v.OrderDate
	add(&od, "pedido", "Fecha del pedido")
	reg := v.RegisteredAt
	add(&reg, "registro", "Registrado en el sistema")
	add(v.ConfirmedAt, "confirmacion", "Confirmado (stock descontado)")
	if v.ValidationStatus == "validado" {
		add(v.ValidatedAt, "validacion", "Validado")
	}
	for _, p := range v.Payments {
		if p.Status == "vigente" {
			pa := p.PaidAt
			add(&pa, "cobro", fmt.Sprintf("Cobro S/ %.2f (%s)", p.Amount, methodLabel(p.Method)))
		}
	}
	if sh := v.Shipment; sh != nil {
		add(sh.ScheduledDispatchDate, "despacho_programado", "Despacho programado")
		add(sh.DispatchedAt, "despacho", "Despachado")
		add(sh.ArrivedAt, "llegada", "Llegó a la agencia")
		if sh.Status == "en_agencia" {
			add(sh.PickupDeadline, "vencimiento", "Vence el plazo de recojo")
		}
		add(sh.PickedUpAt, "recojo", "Recogido por el cliente")
	}
	sort.SliceStable(ev, func(i, j int) bool { return ev[i].At.Before(ev[j].At) })
	return ev
}

var paymentMethodLabels = map[string]string{
	"yape": "Yape", "plin": "Plin", "efectivo": "Efectivo", "transferencia": "Transferencia", "deposito": "Depósito", "otro": "Otro",
}

func methodLabel(m string) string {
	if l, ok := paymentMethodLabels[m]; ok {
		return l
	}
	return m
}

// ── Listado ────────────────────────────────────────────────────────────────

type OrderFilter struct {
	Q                string
	From, To         string // AAAA-MM-DD sobre la fecha del pedido
	SaleType         string
	PaymentStatus    string
	ValidationStatus string
	Status           string
	ShipmentStatus   string
	CarrierID        uint
	Department       string
	Page, PerPage    int
}

type OrderRow struct {
	ID               uint      `json:"id"`
	OrderNumber      int       `json:"order_number"`
	SaleType         string    `json:"sale_type"`
	OrderDate        time.Time `json:"order_date"`
	CustomerName     string    `json:"customer_name"`
	CustomerDocType  string    `json:"customer_doc_type"`
	CustomerDoc      string    `json:"customer_doc_number"`
	TotalAmount      float64   `json:"total_amount"`
	PaidAmount       float64   `json:"paid_amount"`
	BalanceAmount    float64   `json:"balance_amount"`
	PaymentStatus    string    `json:"payment_status"`
	ValidationStatus string    `json:"validation_status"`
	Status           string    `json:"status"`
	IsGift           bool      `json:"is_gift"`
	ShipmentStatus   string    `json:"shipment_status"`
	CarrierName      string    `json:"carrier_name"`
	GuideNumber      string    `json:"guide_number"`
	Department       string    `json:"department"`
	Summary          string    `json:"summary"`
}

type OrderListResult struct {
	Rows       []OrderRow `json:"rows"`
	Total      int64      `json:"total"`
	Page       int        `json:"page"`
	PerPage    int        `json:"per_page"`
	SumTotal   float64    `json:"sum_total"`
	SumBalance float64    `json:"sum_balance"`
}

func (s *Service) ListOrders(f OrderFilter) (*OrderListResult, error) {
	if f.Page < 1 {
		f.Page = 1
	}
	if f.PerPage < 1 || f.PerPage > 200 {
		f.PerPage = 25
	}
	base := s.db.Table("equip_orders o").
		Joins("LEFT JOIN equip_shipments sh ON sh.order_id = o.id AND sh.is_current = ?", true).
		Joins("LEFT JOIN equip_carriers c ON c.id = sh.carrier_id")
	if q := strings.TrimSpace(f.Q); q != "" {
		like := "%" + strings.ToLower(q) + "%"
		base = base.Where("LOWER(o.customer_name) LIKE ? OR o.customer_doc_number LIKE ? OR o.customer_phone LIKE ? OR sh.guide_number LIKE ? OR CAST(o.order_number AS CHAR) = ?",
			like, "%"+q+"%", "%"+q+"%", "%"+q+"%", q)
	}
	if d, err := parseDateOnly(f.From); err != nil {
		return nil, err
	} else if d != nil {
		base = base.Where("o.order_date >= ?", d.Format("2006-01-02"))
	}
	if d, err := parseDateOnly(f.To); err != nil {
		return nil, err
	} else if d != nil {
		base = base.Where("o.order_date <= ?", d.Format("2006-01-02"))
	}
	for col, val := range map[string]string{"o.sale_type": f.SaleType, "o.payment_status": f.PaymentStatus, "o.validation_status": f.ValidationStatus, "o.status": f.Status, "sh.status": f.ShipmentStatus} {
		if strings.TrimSpace(val) != "" {
			base = base.Where(col+" = ?", strings.TrimSpace(val))
		}
	}
	if f.CarrierID > 0 {
		base = base.Where("sh.carrier_id = ?", f.CarrierID)
	}
	if dep := strings.TrimSpace(f.Department); dep != "" {
		base = base.Where("LOWER(sh.destination_department) = ?", strings.ToLower(dep))
	}

	res := &OrderListResult{Page: f.Page, PerPage: f.PerPage}
	if err := base.Session(&gorm.Session{}).Count(&res.Total).Error; err != nil {
		return nil, err
	}
	type sums struct{ T, B float64 }
	var sm sums
	if err := base.Session(&gorm.Session{}).Select("COALESCE(SUM(CASE WHEN o.status <> 'anulado' THEN o.total_amount ELSE 0 END), 0) AS t, COALESCE(SUM(CASE WHEN o.status <> 'anulado' THEN o.balance_amount ELSE 0 END), 0) AS b").Scan(&sm).Error; err != nil {
		return nil, err
	}
	res.SumTotal, res.SumBalance = round2(sm.T), round2(sm.B)

	if err := base.Session(&gorm.Session{}).
		Select("o.id, o.order_number, o.sale_type, o.order_date, o.customer_name, o.customer_doc_type, o.customer_doc_number AS customer_doc, o.total_amount, o.paid_amount, o.balance_amount, o.payment_status, o.validation_status, o.status, o.is_gift, COALESCE(sh.status, '') AS shipment_status, COALESCE(c.name, '') AS carrier_name, COALESCE(sh.guide_number, '') AS guide_number, COALESCE(sh.destination_department, '') AS department").
		Order("o.order_number DESC").Limit(f.PerPage).Offset((f.Page - 1) * f.PerPage).Scan(&res.Rows).Error; err != nil {
		return nil, err
	}
	if len(res.Rows) > 0 {
		ids := make([]uint, len(res.Rows))
		for i, r := range res.Rows {
			ids[i] = r.ID
		}
		var items []database.EquipOrderItem
		if err := s.db.Where("order_id IN ?", ids).Order("line_no").Find(&items).Error; err != nil {
			return nil, err
		}
		parts := map[uint][]string{}
		for _, it := range items {
			parts[it.OrderID] = append(parts[it.OrderID], fmt.Sprintf("%gx %s", it.Quantity, it.Description))
		}
		for i := range res.Rows {
			res.Rows[i].Summary = strings.Join(parts[res.Rows[i].ID], " | ")
		}
	}
	return res, nil
}
