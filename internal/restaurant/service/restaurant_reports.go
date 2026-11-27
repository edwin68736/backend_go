package service

// F6a: reportes de insumos y platos. Solo lectura y siempre de una sucursal. Las sumas se hacen en la base de datos; el costo
// desconocido nunca cuenta como cero.

import (
	"math"
	"sort"
	"strings"
	"time"

	"tukifac/pkg/dailyfx"
	"tukifac/pkg/salecurrency"
	"tukifac/pkg/salescope"
	"tukifac/pkg/sunatnote"

	"gorm.io/gorm"
)

// ReportPeriod: días completos, ambos incluidos.
type ReportPeriod struct{ From, To time.Time }

func reportRound(v float64, decimals int) float64 {
	p := math.Pow(10, float64(decimals))
	return math.Round(v*p) / p
}

func ptr(v float64) *float64 { return &v }

// ── R1 Existencias ──

type SupplyStockRow struct {
	SupplyID uint     `json:"supply_id"`
	Code     string   `json:"code"`
	Name     string   `json:"name"`
	Unit     string   `json:"unit"`
	Quantity float64  `json:"quantity"`
	MinStock float64  `json:"min_stock"`
	Low      bool     `json:"low"`
	Negative bool     `json:"negative"`
	AvgCost  *float64 `json:"avg_cost,omitempty"`
	Value    *float64 `json:"value,omitempty"`
	LastCost *float64 `json:"last_cost,omitempty"`
}

type SupplyStockSummary struct {
	Items         int      `json:"items"`
	LowCount      int      `json:"low_count"`
	NegativeCount int      `json:"negative_count"`
	TotalValue    *float64 `json:"total_value,omitempty"`
	// WithoutCost: insumos con stock cuyo costo no se conoce; no están en el total.
	WithoutCost int `json:"without_cost"`
}

// SupplyStockReport lista los insumos activos con su saldo en la sucursal. El stock negativo se muestra y no resta del total.
func (s *RestaurantService) SupplyStockReport(branchID uint, withCost bool) ([]SupplyStockRow, SupplyStockSummary, error) {
	var raw []struct {
		ID            uint
		Code          string
		Name          string
		Unit          string
		MinStock      float64
		PurchasePrice float64
		Quantity      *float64
		AvgUnitCost   *float64
	}
	err := s.db.Table("tenant_products AS p").
		Select("p.id, p.code, p.name, p.unit, p.min_stock, p.purchase_price, st.quantity, st.avg_unit_cost").
		Joins("LEFT JOIN tenant_product_stocks st ON st.product_id = p.id AND st.branch_id = ?", branchID).
		Where("p.is_supply = ? AND p.active = ? AND p.deleted_at IS NULL", true, true).
		Order("p.name, p.id").Scan(&raw).Error
	summary := SupplyStockSummary{}
	if err != nil {
		return nil, summary, err
	}
	rows := make([]SupplyStockRow, 0, len(raw))
	total := 0.0
	for _, r := range raw {
		row := SupplyStockRow{SupplyID: r.ID, Code: r.Code, Name: r.Name, Unit: r.Unit, MinStock: r.MinStock}
		if r.Quantity != nil {
			row.Quantity = *r.Quantity
		}
		row.Low = row.MinStock > 0 && row.Quantity < row.MinStock
		row.Negative = row.Quantity < 0
		if row.Low {
			summary.LowCount++
		}
		if row.Negative {
			summary.NegativeCount++
		}
		if r.AvgUnitCost == nil && row.Quantity > 0 {
			summary.WithoutCost++
		}
		if withCost {
			if r.PurchasePrice > 0 {
				row.LastCost = ptr(r.PurchasePrice)
			}
			if r.AvgUnitCost != nil {
				row.AvgCost = ptr(*r.AvgUnitCost)
				value := reportRound(math.Max(row.Quantity, 0)*(*r.AvgUnitCost), 2)
				row.Value = ptr(value)
				total += value
			}
		}
		rows = append(rows, row)
	}
	summary.Items = len(rows)
	if withCost {
		summary.TotalValue = ptr(reportRound(total, 2))
	}
	return rows, summary, nil
}

// ── R2 Compras ──

type SupplyPurchaseRow struct {
	PurchaseID uint     `json:"purchase_id"`
	IssueDate  string   `json:"issue_date"`
	Supplier   string   `json:"supplier"`
	Document   string   `json:"document"`
	SupplyID   uint     `json:"supply_id"`
	Supply     string   `json:"supply"`
	Unit       string   `json:"unit"`
	Quantity   float64  `json:"quantity"` // en la unidad base del insumo
	UnitCost   *float64 `json:"unit_cost,omitempty"`
	Amount     *float64 `json:"amount,omitempty"`
}

type SupplyPurchaseSummary struct {
	TotalAmount *float64 `json:"total_amount,omitempty"`
}

// purchaseEntries: los ingresos de kardex de compras vigentes. Salen del kardex para que cantidad y costo (sin IGV, en unidad base)
// sean los mismos que movieron el stock.
func (s *RestaurantService) purchaseEntries(branchID, supplyID uint, period ReportPeriod) *gorm.DB {
	q := s.db.Table("tenant_stock_movements AS m").
		Joins("JOIN tenant_purchase_items i ON i.id = m.purchase_item_id").
		Joins("JOIN tenant_purchases tp ON tp.id = i.purchase_id").
		Joins("JOIN tenant_products p ON p.id = m.product_id AND p.is_supply = ?", true).
		Where("m.type = ? AND m.branch_id = ? AND tp.status <> ?", "in", branchID, "cancelled").
		Where("tp.issue_date >= ? AND tp.issue_date <= ?", period.From, period.To)
	if supplyID > 0 {
		q = q.Where("m.product_id = ?", supplyID)
	}
	return q
}

func (s *RestaurantService) SupplyPurchasesReport(branchID, supplyID uint, period ReportPeriod, page, perPage int, withCost bool) (
	[]SupplyPurchaseRow, int64, SupplyPurchaseSummary, error) {
	summary := SupplyPurchaseSummary{}
	var total int64
	if err := s.purchaseEntries(branchID, supplyID, period).Count(&total).Error; err != nil {
		return nil, 0, summary, err
	}
	var raw []struct {
		PurchaseID  uint
		IssueDate   time.Time
		Supplier    string
		Series      string
		Number      string
		SupplyID    uint
		Supply      string
		Unit        string
		Quantity    float64
		UnitCost    float64
		CostUnknown bool
	}
	err := s.purchaseEntries(branchID, supplyID, period).
		Select("tp.id AS purchase_id, tp.issue_date, ct.business_name AS supplier, tp.series, tp.number, p.id AS supply_id, " +
			"p.name AS supply, p.unit, m.quantity, m.unit_cost, m.cost_unknown").
		Joins("LEFT JOIN tenant_contacts ct ON ct.id = tp.contact_id").
		Order("tp.issue_date DESC, m.id DESC").Limit(perPage).Offset((page - 1) * perPage).Scan(&raw).Error
	if err != nil {
		return nil, 0, summary, err
	}
	rows := make([]SupplyPurchaseRow, 0, len(raw))
	for _, r := range raw {
		row := SupplyPurchaseRow{PurchaseID: r.PurchaseID, IssueDate: r.IssueDate.Format("2006-01-02"), Supplier: r.Supplier,
			Document: r.Series + "-" + r.Number, SupplyID: r.SupplyID, Supply: r.Supply, Unit: r.Unit, Quantity: r.Quantity}
		if withCost && !r.CostUnknown {
			row.UnitCost = ptr(r.UnitCost)
			row.Amount = ptr(reportRound(r.Quantity*r.UnitCost, 2))
		}
		rows = append(rows, row)
	}
	if withCost {
		var amount float64
		err = s.purchaseEntries(branchID, supplyID, period).Where("m.cost_unknown = ?", false).
			Select("COALESCE(SUM(m.quantity * m.unit_cost), 0)").Scan(&amount).Error
		if err != nil {
			return nil, 0, summary, err
		}
		summary.TotalAmount = ptr(reportRound(amount, 2))
	}
	return rows, total, summary, nil
}

// ── R3 Consumo ──

type SupplyConsumptionRow struct {
	SupplyID uint     `json:"supply_id"`
	Supply   string   `json:"supply"`
	Unit     string   `json:"unit"`
	Consumed float64  `json:"consumed"`
	Restored float64  `json:"restored"`
	Net      float64  `json:"net"`
	Cost     *float64 `json:"cost,omitempty"` // de lo neto, a lo que costó al salir
	// WithoutCost: cantidad consumida sin costo conocido; no está en Cost.
	WithoutCost float64 `json:"without_cost"`
}

type SupplyConsumptionSummary struct {
	TotalCost *float64 `json:"total_cost,omitempty"`
}

// SupplyConsumptionReport suma lo que salió por recetas en el período y resta lo que se repuso en el período (anulaciones y notas de
// crédito), aunque la venta sea anterior: así cuadra con el kardex.
func (s *RestaurantService) SupplyConsumptionReport(branchID uint, period ReportPeriod, withCost bool) (
	[]SupplyConsumptionRow, SupplyConsumptionSummary, error) {
	summary := SupplyConsumptionSummary{}
	type sums struct {
		SupplyID    uint
		Qty         float64
		Cost        float64
		WithoutCost float64
	}
	var out, back []sums
	if err := s.db.Table("tenant_recipe_consumptions AS c").
		Select("c.supply_id, SUM(c.quantity) AS qty, SUM(COALESCE(c.quantity * c.unit_cost, 0)) AS cost, "+
			"SUM(CASE WHEN c.unit_cost IS NULL THEN c.quantity ELSE 0 END) AS without_cost").
		Where("c.branch_id = ? AND c.created_at >= ? AND c.created_at <= ?", branchID, period.From, period.To).
		Group("c.supply_id").Scan(&out).Error; err != nil {
		return nil, summary, err
	}
	if err := s.db.Table("tenant_recipe_consumption_reversals AS r").
		Select("c.supply_id, SUM(r.quantity) AS qty, SUM(COALESCE(r.quantity * c.unit_cost, 0)) AS cost, "+
			"SUM(CASE WHEN c.unit_cost IS NULL THEN r.quantity ELSE 0 END) AS without_cost").
		Joins("JOIN tenant_recipe_consumptions c ON c.id = r.consumption_id").
		Where("c.branch_id = ? AND r.created_at >= ? AND r.created_at <= ?", branchID, period.From, period.To).
		Group("c.supply_id").Scan(&back).Error; err != nil {
		return nil, summary, err
	}
	byID := map[uint]*SupplyConsumptionRow{}
	cost := map[uint]float64{}
	get := func(id uint) *SupplyConsumptionRow {
		if byID[id] == nil {
			byID[id] = &SupplyConsumptionRow{SupplyID: id}
		}
		return byID[id]
	}
	for _, r := range out {
		row := get(r.SupplyID)
		row.Consumed, row.WithoutCost = r.Qty, r.WithoutCost
		cost[r.SupplyID] += r.Cost
	}
	for _, r := range back {
		row := get(r.SupplyID)
		row.Restored = r.Qty
		row.WithoutCost -= r.WithoutCost
		cost[r.SupplyID] -= r.Cost
	}
	ids := make([]uint, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	names, err := s.productLabels(ids)
	if err != nil {
		return nil, summary, err
	}
	rows := make([]SupplyConsumptionRow, 0, len(byID))
	total := 0.0
	for id, row := range byID {
		row.Supply, row.Unit = names[id].Name, names[id].Unit
		row.Consumed, row.Restored = reportRound(row.Consumed, 3), reportRound(row.Restored, 3)
		row.Net = reportRound(row.Consumed-row.Restored, 3)
		row.WithoutCost = reportRound(row.WithoutCost, 3)
		if withCost {
			row.Cost = ptr(reportRound(cost[id], 2))
			total += cost[id]
		}
		rows = append(rows, *row)
	}
	// Lo que más se consume primero: por costo (comparable entre unidades) y, sin permiso de costos, por cantidad.
	sort.Slice(rows, func(i, j int) bool {
		if withCost && *rows[i].Cost != *rows[j].Cost {
			return *rows[i].Cost > *rows[j].Cost
		}
		if rows[i].Net != rows[j].Net {
			return rows[i].Net > rows[j].Net
		}
		return rows[i].Supply < rows[j].Supply
	})
	if withCost {
		summary.TotalCost = ptr(reportRound(total, 2))
	}
	return rows, summary, nil
}

type productLabel struct{ Name, Unit string }

// productLabels incluye productos eliminados: lo que ya se consumió sigue teniendo nombre.
func (s *RestaurantService) productLabels(ids []uint) (map[uint]productLabel, error) {
	out := map[uint]productLabel{}
	if len(ids) == 0 {
		return out, nil
	}
	var rows []struct {
		ID   uint
		Name string
		Unit string
	}
	if err := s.db.Table("tenant_products").Select("id, name, unit").Where("id IN ?", ids).Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.ID] = productLabel{Name: r.Name, Unit: r.Unit}
	}
	return out, nil
}

// ── R4 Mermas y desperdicios ──

type SupplyWasteRow struct {
	Date     time.Time `json:"date"`
	SupplyID uint      `json:"supply_id"`
	Supply   string    `json:"supply"`
	Unit     string    `json:"unit"`
	Kind     string    `json:"kind"` // merma | desperdicio
	Quantity float64   `json:"quantity"`
	UnitCost *float64  `json:"unit_cost,omitempty"`
	Value    *float64  `json:"value,omitempty"`
	User     string    `json:"user"`
	Notes    string    `json:"notes"`
}

type SupplyWasteSummary struct {
	TotalValue *float64 `json:"total_value,omitempty"`
}

var wasteOperations = map[string]string{"SHRINKAGE": "merma", "WASTE": "desperdicio"}

// wasteTypes: ids de los tipos de movimiento de merma y desmedro. Filtrar por id usa su índice en el kardex.
func (s *RestaurantService) wasteTypes() ([]uint, error) {
	var ids []uint
	err := s.db.Table("tenant_inventory_operation_types").Where("code IN ?", []string{"SHRINKAGE", "WASTE"}).Pluck("id", &ids).Error
	return ids, err
}

// wasteMovements: salidas de insumos por merma o desmedro; las de un documento anulado no cuentan.
func (s *RestaurantService) wasteMovements(types []uint, branchID, supplyID uint, period ReportPeriod) *gorm.DB {
	q := s.db.Table("tenant_stock_movements AS m").
		Joins("JOIN tenant_inventory_operation_types ot ON ot.id = m.operation_type_id").
		Joins("JOIN tenant_products p ON p.id = m.product_id AND p.is_supply = ?", true).
		Joins("LEFT JOIN tenant_inventory_documents d ON d.id = m.inventory_document_id").
		Where("m.operation_type_id IN ? AND m.type = ? AND m.branch_id = ?", types, "out", branchID).
		Where("d.id IS NULL OR d.status <> ?", "voided").
		Where("m.created_at >= ? AND m.created_at <= ?", period.From, period.To)
	if supplyID > 0 {
		q = q.Where("m.product_id = ?", supplyID)
	}
	return q
}

func (s *RestaurantService) SupplyWasteReport(branchID, supplyID uint, period ReportPeriod, page, perPage int, withCost bool) (
	[]SupplyWasteRow, int64, SupplyWasteSummary, error) {
	summary := SupplyWasteSummary{}
	var total int64
	types, err := s.wasteTypes()
	if err != nil {
		return nil, 0, summary, err
	}
	if err := s.wasteMovements(types, branchID, supplyID, period).Count(&total).Error; err != nil {
		return nil, 0, summary, err
	}
	var raw []struct {
		CreatedAt   time.Time
		SupplyID    uint
		Supply      string
		Unit        string
		Code        string
		Quantity    float64
		UnitCost    float64
		CostUnknown bool
		UserName    string
		Notes       string
	}
	err = s.wasteMovements(types, branchID, supplyID, period).
		Select("m.created_at, p.id AS supply_id, p.name AS supply, p.unit, ot.code, m.quantity, m.unit_cost, m.cost_unknown, " +
			"u.name AS user_name, m.notes").
		Joins("LEFT JOIN tenant_users u ON u.id = m.user_id").
		Order("m.created_at DESC, m.id DESC").Limit(perPage).Offset((page - 1) * perPage).Scan(&raw).Error
	if err != nil {
		return nil, 0, summary, err
	}
	rows := make([]SupplyWasteRow, 0, len(raw))
	for _, r := range raw {
		row := SupplyWasteRow{Date: r.CreatedAt, SupplyID: r.SupplyID, Supply: r.Supply, Unit: r.Unit, Kind: wasteOperations[r.Code],
			Quantity: r.Quantity, User: r.UserName, Notes: r.Notes}
		if withCost && !r.CostUnknown {
			row.UnitCost = ptr(r.UnitCost)
			row.Value = ptr(reportRound(r.Quantity*r.UnitCost, 2))
		}
		rows = append(rows, row)
	}
	if withCost {
		var value float64
		err = s.wasteMovements(types, branchID, supplyID, period).Where("m.cost_unknown = ?", false).
			Select("COALESCE(SUM(m.quantity * m.unit_cost), 0)").Scan(&value).Error
		if err != nil {
			return nil, 0, summary, err
		}
		summary.TotalValue = ptr(reportRound(value, 2))
	}
	return rows, total, summary, nil
}

// ── R6 Rentabilidad de platos vendidos ──

type DishProfitRow struct {
	ProductID      uint     `json:"product_id"`
	PresentationID uint     `json:"presentation_id"`
	Name           string   `json:"name"`
	Units          float64  `json:"units"`   // netas de devoluciones, con y sin costo
	Revenue        float64  `json:"revenue"` // sin IGV, en soles, netas de notas de crédito parciales
	Cost           float64  `json:"cost"`
	Margin         float64  `json:"margin"`        // solo sobre lo que tiene costo
	MarginPct      *float64 `json:"margin_pct"`    // vacío = nada con costo
	FoodCostPct    *float64 `json:"food_cost_pct"` // vacío = nada con costo
	// Lo vendido sin costo de receta: está en Units y Revenue pero no en el margen.
	UnitsWithoutCost   float64 `json:"units_without_cost"`
	RevenueWithoutCost float64 `json:"revenue_without_cost"`
}

type DishProfitSummary struct {
	Units              float64  `json:"units"`
	Revenue            float64  `json:"revenue"`
	Cost               float64  `json:"cost"`
	Margin             float64  `json:"margin"`
	MarginPct          *float64 `json:"margin_pct"`
	FoodCostPct        *float64 `json:"food_cost_pct"`
	UnitsWithoutCost   float64  `json:"units_without_cost"`
	RevenueWithoutCost float64  `json:"revenue_without_cost"`
	// UnconvertedUSD: venta en dólares sin tipo de cambio conocido; no está en ningún total.
	UnconvertedUSD float64 `json:"unconverted_usd"`
}

// dishSales: líneas de platos de las ventas vigentes de la sucursal emitidas en el período. Una nota de venta convertida cuenta una
// sola vez, como en los reportes de ventas.
func (s *RestaurantService) dishSales(branchID uint, period ReportPeriod, item string) *gorm.DB {
	return s.db.Table("tenant_sale_items AS "+item).
		Joins("JOIN tenant_sales AS s ON s.id = "+item+".sale_id").
		Joins("JOIN tenant_products p ON p.id = "+item+".product_id AND p.is_restaurant = ? AND p.is_supply = ?", true, false).
		Where("s.branch_id = ? AND s.status <> ? AND s.deleted_at IS NULL", branchID, "cancelled").
		Where("s.issue_date >= ? AND s.issue_date <= ?", period.From, period.To).
		Scopes(salescope.ScopeEmittedNoNotes("s"))
}

// fxDay: solo las ventas en dólares sin tipo de cambio guardado se separan por día para buscarlo.
const fxDay = "CASE WHEN s.currency = 'USD' AND (s.exchange_rate IS NULL OR s.exchange_rate = 0) " +
	"THEN SUBSTR(CAST(s.issue_date AS CHAR), 1, 10) ELSE '' END"

type dishKey struct{ product, presentation uint }

type dishGroup struct {
	ProductID      uint
	PresentationID uint
	Currency       string
	ExchangeRate   *float64
	FxDay          string
	HasCost        bool
	ReasonCode     string
	Units          float64
	Subtotal       float64
	Cost           float64
}

// toPEN devuelve el factor a soles; false = dólares sin tipo de cambio.
func toPEN(g dishGroup, rates map[string]float64) (float64, bool) {
	if strings.ToUpper(strings.TrimSpace(g.Currency)) != salecurrency.CurrencyUSD {
		return 1, true
	}
	if g.ExchangeRate != nil && *g.ExchangeRate > 0 {
		return *g.ExchangeRate, true
	}
	rate, seen := rates[g.FxDay]
	if !seen {
		if r, _, ok := dailyfx.SaleRate(g.FxDay); ok && r > 0 {
			rate = r
		}
		rates[g.FxDay] = rate
	}
	return rate, rate > 0
}

func (s *RestaurantService) DishProfitReport(branchID uint, period ReportPeriod) ([]DishProfitRow, DishProfitSummary, error) {
	summary := DishProfitSummary{}
	var sold []dishGroup
	if err := s.dishSales(branchID, period, "si").
		Select("si.product_id, COALESCE(si.presentation_id, 0) AS presentation_id, s.currency, s.exchange_rate, " + fxDay + " AS fx_day, " +
			"CASE WHEN si.recipe_cost_total IS NULL THEN 0 ELSE 1 END AS has_cost, SUM(si.quantity) AS units, " +
			"SUM(si.subtotal) AS subtotal, SUM(COALESCE(si.recipe_cost_total, 0)) AS cost").
		Group("si.product_id, COALESCE(si.presentation_id, 0), s.currency, s.exchange_rate, " + fxDay + ", " +
			"CASE WHEN si.recipe_cost_total IS NULL THEN 0 ELSE 1 END").
		Scan(&sold).Error; err != nil {
		return nil, summary, err
	}
	// Notas de crédito parciales ACEPTADAS sobre esas líneas (hasta entonces tampoco reponen stock).
	var notes []dishGroup
	if err := s.dishSales(branchID, period, "oi").
		Select("oi.product_id, COALESCE(oi.presentation_id, 0) AS presentation_id, s.currency, s.exchange_rate, "+fxDay+" AS fx_day, "+
			"CASE WHEN oi.recipe_cost_total IS NULL THEN 0 ELSE 1 END AS has_cost, n.note_reason_code AS reason_code, "+
			"SUM(ni.quantity) AS units, SUM(ni.subtotal) AS subtotal").
		Joins("JOIN tenant_sale_items ni ON ni.original_sale_item_id = oi.id").
		Joins("JOIN tenant_sales n ON n.id = ni.sale_id AND n.doc_type = ? AND n.billing_status = ? AND n.status <> ? AND n.deleted_at IS NULL",
			salescope.DocTypeCreditNote, "accepted", "cancelled").
		Group("oi.product_id, COALESCE(oi.presentation_id, 0), s.currency, s.exchange_rate, " + fxDay + ", " +
			"CASE WHEN oi.recipe_cost_total IS NULL THEN 0 ELSE 1 END, n.note_reason_code").
		Scan(&notes).Error; err != nil {
		return nil, summary, err
	}
	// Ajustes por importe: no apuntan a la línea original (no devuelven cantidades), así que se atribuyen al plato. Se agrupa por
	// venta para saber si ese plato tuvo costo en ella.
	var adjustments []dishGroup
	if err := s.db.Table("tenant_sales AS n").
		Select("ni.product_id, COALESCE(ni.presentation_id, 0) AS presentation_id, s.currency, s.exchange_rate, "+fxDay+" AS fx_day, "+
			"MAX(CASE WHEN EXISTS (SELECT 1 FROM tenant_sale_items x WHERE x.sale_id = s.id AND x.product_id = ni.product_id "+
			"AND x.recipe_cost_total IS NOT NULL) THEN 1 ELSE 0 END) AS has_cost, n.note_reason_code AS reason_code, "+
			"SUM(ni.subtotal) AS subtotal").
		Joins("JOIN tenant_sale_items ni ON ni.sale_id = n.id AND ni.original_sale_item_id IS NULL").
		Joins("JOIN tenant_sales s ON s.id = n.original_sale_id").
		Joins("JOIN tenant_products p ON p.id = ni.product_id AND p.is_restaurant = ? AND p.is_supply = ?", true, false).
		Where("n.doc_type = ? AND n.billing_status = ? AND n.status <> ? AND n.deleted_at IS NULL",
			salescope.DocTypeCreditNote, "accepted", "cancelled").
		Where("s.branch_id = ? AND s.status <> ? AND s.deleted_at IS NULL", branchID, "cancelled").
		Where("s.issue_date >= ? AND s.issue_date <= ?", period.From, period.To).
		Scopes(salescope.ScopeEmittedNoNotes("s")).
		Group("s.id, ni.product_id, COALESCE(ni.presentation_id, 0), s.currency, s.exchange_rate, " + fxDay + ", n.note_reason_code").
		Scan(&adjustments).Error; err != nil {
		return nil, summary, err
	}
	// Costo de los insumos que volvieron por esas devoluciones. Se parte de las reposiciones, que son pocas.
	var returned []dishGroup
	if err := s.db.Table("tenant_recipe_consumption_reversals AS r").
		Select("si.product_id, COALESCE(si.presentation_id, 0) AS presentation_id, SUM(r.quantity * c.unit_cost) AS cost").
		Joins("JOIN tenant_recipe_consumptions c ON c.id = r.consumption_id AND c.unit_cost IS NOT NULL").
		Joins("JOIN tenant_sale_items si ON si.id = c.sale_item_id AND si.recipe_cost_total IS NOT NULL").
		Joins("JOIN tenant_sales s ON s.id = si.sale_id").
		Joins("JOIN tenant_products p ON p.id = si.product_id AND p.is_restaurant = ? AND p.is_supply = ?", true, false).
		Where("c.branch_id = ? AND s.branch_id = ? AND s.status <> ? AND s.deleted_at IS NULL", branchID, branchID, "cancelled").
		Where("s.issue_date >= ? AND s.issue_date <= ?", period.From, period.To).
		Scopes(salescope.ScopeEmittedNoNotes("s")).
		Group("si.product_id, COALESCE(si.presentation_id, 0)").
		Scan(&returned).Error; err != nil {
		return nil, summary, err
	}

	byDish := map[dishKey]*DishProfitRow{}
	rates := map[string]float64{}
	row := func(g dishGroup) *DishProfitRow {
		k := dishKey{g.ProductID, g.PresentationID}
		if byDish[k] == nil {
			byDish[k] = &DishProfitRow{ProductID: g.ProductID, PresentationID: g.PresentationID}
		}
		return byDish[k]
	}
	// add suma (o resta, con signo negativo) unidades y venta en el grupo con o sin costo.
	add := func(g dishGroup, units, revenue float64) {
		factor, ok := toPEN(g, rates)
		if !ok {
			summary.UnconvertedUSD += revenue
			return
		}
		r := row(g)
		r.Units += units
		r.Revenue += revenue * factor
		if !g.HasCost {
			r.UnitsWithoutCost += units
			r.RevenueWithoutCost += revenue * factor
		}
	}
	for _, g := range sold {
		add(g, g.Units, g.Subtotal)
		if _, ok := toPEN(g, rates); ok && g.HasCost {
			row(g).Cost += g.Cost
		}
	}
	for _, g := range notes {
		if !sunatnote.IsPartialCreditReason(g.ReasonCode) {
			continue // la anulación total deja la venta anulada; una corrección de datos no mueve importes
		}
		units := 0.0
		if sunatnote.CreditReasonReturnsStock(g.ReasonCode) {
			units = g.Units
		}
		add(g, -units, -g.Subtotal)
	}
	for _, g := range adjustments {
		if sunatnote.IsPartialCreditReason(g.ReasonCode) {
			add(g, 0, -g.Subtotal)
		}
	}
	for _, g := range returned {
		if r := byDish[dishKey{g.ProductID, g.PresentationID}]; r != nil {
			r.Cost -= g.Cost
		}
	}

	if err := s.nameDishes(byDish); err != nil {
		return nil, summary, err
	}
	rows := make([]DishProfitRow, 0, len(byDish))
	costedRevenue := 0.0
	for _, r := range byDish {
		withCost := r.Revenue - r.RevenueWithoutCost
		r.Margin, r.MarginPct, r.FoodCostPct = marginOf(withCost, r.Cost)
		summary.Units += r.Units
		summary.Revenue += r.Revenue
		summary.Cost += r.Cost
		summary.UnitsWithoutCost += r.UnitsWithoutCost
		summary.RevenueWithoutCost += r.RevenueWithoutCost
		costedRevenue += withCost
		r.Units, r.UnitsWithoutCost = reportRound(r.Units, 3), reportRound(r.UnitsWithoutCost, 3)
		r.Revenue, r.RevenueWithoutCost = reportRound(r.Revenue, 2), reportRound(r.RevenueWithoutCost, 2)
		r.Cost = reportRound(r.Cost, 2)
		rows = append(rows, *r)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Margin != rows[j].Margin {
			return rows[i].Margin > rows[j].Margin
		}
		if rows[i].Revenue != rows[j].Revenue {
			return rows[i].Revenue > rows[j].Revenue
		}
		return rows[i].Name < rows[j].Name
	})
	summary.Margin, summary.MarginPct, summary.FoodCostPct = marginOf(costedRevenue, summary.Cost)
	summary.Units, summary.UnitsWithoutCost = reportRound(summary.Units, 3), reportRound(summary.UnitsWithoutCost, 3)
	summary.Revenue, summary.RevenueWithoutCost = reportRound(summary.Revenue, 2), reportRound(summary.RevenueWithoutCost, 2)
	summary.Cost, summary.UnconvertedUSD = reportRound(summary.Cost, 2), reportRound(summary.UnconvertedUSD, 2)
	return rows, summary, nil
}

func marginOf(revenue, cost float64) (float64, *float64, *float64) {
	if revenue < 0.005 {
		return reportRound(revenue-cost, 2), nil, nil
	}
	return reportRound(revenue-cost, 2), ptr(reportRound((revenue-cost)/revenue*100, 2)), ptr(reportRound(cost/revenue*100, 2))
}

// nameDishes pone «Plato» o «Plato - Presentación».
func (s *RestaurantService) nameDishes(dishes map[dishKey]*DishProfitRow) error {
	var productIDs, presentationIDs []uint
	for k := range dishes {
		productIDs = append(productIDs, k.product)
		if k.presentation > 0 {
			presentationIDs = append(presentationIDs, k.presentation)
		}
	}
	names, err := s.productLabels(productIDs)
	if err != nil {
		return err
	}
	presentations := map[uint]string{}
	if len(presentationIDs) > 0 {
		var rows []struct {
			ID   uint
			Name string
		}
		if err := s.db.Table("tenant_product_presentations").Select("id, name").Where("id IN ?", presentationIDs).Scan(&rows).Error; err != nil {
			return err
		}
		for _, r := range rows {
			presentations[r.ID] = r.Name
		}
	}
	for k, r := range dishes {
		r.Name = names[k.product].Name
		if p := presentations[k.presentation]; p != "" {
			r.Name += " - " + p
		}
	}
	return nil
}
