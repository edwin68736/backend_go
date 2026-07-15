package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	invsvc "tukifac/internal/inventory/service"
	"tukifac/pkg/database"
	"tukifac/pkg/gormutil"
	"tukifac/pkg/money"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// comboSelectionInput lo que el cliente eligió en cada grupo del combo (viene del POS).
// Los grupos fijos no necesitan enviarse: se resuelven solos desde el catálogo.
type comboSelectionInput struct {
	GroupID uint                      `json:"group_id"`
	Items   []comboSelectionItemInput `json:"items"`
}

type comboSelectionItemInput struct {
	ProductID uint    `json:"product_id"`
	Quantity  float64 `json:"quantity"`
}

// comboComponentPayload datos del componente que representa una comanda concreta.
type comboComponentPayload struct {
	GroupID       uint    `json:"group_id"`
	GroupName     string  `json:"group_name"`
	SelectionType string  `json:"selection_type"`
	ProductID     uint    `json:"product_id"`
	ProductName   string  `json:"product_name"`
	Quantity      float64 `json:"quantity"` // por combo, sin multiplicar por los combos pedidos
	ExtraPrice    float64 `json:"extra_price"`
}

// comboPayload snapshot histórico guardado en TenantComanda.ComboJSON. La parte del combo es
// idéntica en las N comandas del mismo combo_parent_key; Component cambia en cada una.
// No depende del catálogo vivo: si mañana cambia el combo, lo ya pedido no se altera.
type comboPayload struct {
	ComboID            uint                  `json:"combo_id"`
	ComboCode          string                `json:"combo_code"`
	ComboName          string                `json:"combo_name"`
	ComboPrice         float64               `json:"combo_price"`    // unitario, ya con sobreprecios
	ComboQuantity      float64               `json:"combo_quantity"` // combos pedidos en esta línea
	Selection          string                `json:"selection"`      // firma: agrupa líneas idénticas al facturar
	IgvAffectationType string                `json:"igv_affectation_type"`
	PriceIncludesIgv   bool                  `json:"price_includes_igv"`
	Component          comboComponentPayload `json:"component"`
	Snapshot           bool                  `json:"snapshot"`
}

// comboComponentDraft una comanda a crear por cada componente del combo.
type comboComponentDraft struct {
	ProductID         uint
	ProductCode       string
	ProductName       string
	Quantity          float64 // por combo
	PreparationArea   string
	PreparationAreaID *uint
	Payload           comboPayload
}

func parseComboSelections(raw string) ([]comboSelectionInput, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	var out []comboSelectionInput
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, errors.New("combo_json inválido")
	}
	return out, nil
}

func parseComboPayload(raw string) (*comboPayload, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	var p comboPayload
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return nil, errors.New("combo_json de la comanda inválido")
	}
	if p.ComboID == 0 {
		return nil, nil
	}
	return &p, nil
}

// resolveComboOrderItem valida la elección del cliente contra el combo del catálogo y devuelve
// un draft por componente. Devuelve nil si el producto no es un combo.
//
// Precio: precio fijo del combo + Σ (sobreprecio × cantidad) de las opciones elegidas.
// Igual que en el resto del flujo, un unit_price > 0 del cliente (precio acordado en caja) manda.
// El producto ya viene resuelto por resolveRestaurantOrderItem: releerlo aquí era una
// consulta por ítem que no aportaba nada.
func resolveComboOrderItem(
	tx *gorm.DB,
	item *NewOrderItem,
	resolved *database.TenantProduct,
) ([]comboComponentDraft, error) {
	if item.ProductID == nil || *item.ProductID == 0 || resolved == nil {
		return nil, nil
	}
	combo := *resolved
	if !combo.HasCombo {
		return nil, nil
	}

	groups, itemsByGroup, err := loadComboCatalog(tx, combo.ID)
	if err != nil {
		return nil, err
	}
	if len(groups) == 0 {
		return nil, fmt.Errorf("el combo «%s» no tiene grupos configurados", combo.Name)
	}

	selections, err := parseComboSelections(item.ComboJSON)
	if err != nil {
		return nil, err
	}
	selByGroup := make(map[uint][]comboSelectionItemInput, len(selections))
	for _, s := range selections {
		selByGroup[s.GroupID] = append(selByGroup[s.GroupID], s.Items...)
	}

	chosen := make([]comboChosenComponent, 0, len(groups))
	for _, g := range groups {
		picks, err := resolveComboGroupSelection(g, itemsByGroup[g.ID], selByGroup[g.ID])
		if err != nil {
			return nil, err
		}
		chosen = append(chosen, picks...)
	}
	if len(chosen) == 0 {
		return nil, fmt.Errorf("el combo «%s» no tiene componentes seleccionados", combo.Name)
	}

	extras := 0.0
	for _, c := range chosen {
		extras += c.ExtraPrice * c.Quantity
	}
	comboUnit := money.RoundDisplay(money.RoundDisplay(combo.SalePrice) + extras)
	if money.RoundDisplay(item.UnitPrice) <= 0 {
		item.UnitPrice = comboUnit
	}
	if strings.TrimSpace(item.ProductName) == "" {
		item.ProductName = combo.Name
	}
	if strings.TrimSpace(item.ProductCode) == "" {
		item.ProductCode = combo.Code
	}
	affType := strings.TrimSpace(item.IgvAffectationType)
	if affType == "" {
		affType = strings.TrimSpace(combo.IgvAffectationType)
	}
	if affType == "" {
		affType = "10"
	}
	item.IgvAffectationType = affType
	item.PriceIncludesIgv = combo.PriceIncludesIgv

	base := comboPayload{
		ComboID:            combo.ID,
		ComboCode:          combo.Code,
		ComboName:          combo.Name,
		ComboPrice:         money.RoundDisplay(item.UnitPrice),
		ComboQuantity:      item.Quantity,
		Selection:          comboSelectionSignature(chosen),
		IgvAffectationType: affType,
		PriceIncludesIgv:   combo.PriceIncludesIgv,
		Snapshot:           true,
	}

	drafts := make([]comboComponentDraft, 0, len(chosen))
	for _, c := range chosen {
		payload := base
		payload.Component = comboComponentPayload{
			GroupID:       c.GroupID,
			GroupName:     c.GroupName,
			SelectionType: c.SelectionType,
			ProductID:     c.Product.ID,
			ProductName:   c.Product.Name,
			Quantity:      c.Quantity,
			ExtraPrice:    money.RoundDisplay(c.ExtraPrice),
		}
		area, areaID := resolveComboComponentArea(tx, c)
		drafts = append(drafts, comboComponentDraft{
			ProductID:         c.Product.ID,
			ProductCode:       c.Product.Code,
			ProductName:       c.Product.Name,
			Quantity:          c.Quantity,
			PreparationArea:   area,
			PreparationAreaID: areaID,
			Payload:           payload,
		})
	}
	return drafts, nil
}

// createComboComandas crea una comanda por componente, cada una ruteada a su área de preparación
// y todas atadas por un mismo combo_parent_key. Van a unit_price 0: el dinero del combo vive en
// su línea de venta (ver comandasToBillLines), no repartido entre los componentes.
func createComboComandas(
	tx *gorm.DB,
	orderID, sessionID uint,
	item *NewOrderItem,
	drafts []comboComponentDraft,
) ([]database.TenantComanda, error) {
	parentKey := uuid.NewString()
	out := make([]database.TenantComanda, 0, len(drafts))
	for _, d := range drafts {
		payload := d.Payload
		payload.ComboQuantity = item.Quantity
		raw, err := json.Marshal(payload)
		if err != nil {
			return nil, errors.New("no se pudo serializar combo_json")
		}
		productID := d.ProductID
		c := database.TenantComanda{
			OrderID:            orderID,
			SessionID:          sessionID,
			ProductID:          &productID,
			ProductCode:        d.ProductCode,
			ProductName:        d.ProductName,
			PreparationArea:    d.PreparationArea,
			PreparationAreaID:  d.PreparationAreaID,
			Quantity:           item.Quantity * d.Quantity,
			UnitPrice:          0,
			Notes:              item.Notes,
			IgvAffectationType: payload.IgvAffectationType,
			PriceIncludesIgv:   payload.PriceIncludesIgv,
			ComboParentKey:     parentKey,
			ComboJSON:          string(raw),
			Status:             "pendiente",
		}
		if err := tx.Create(&c).Error; err != nil {
			return nil, err
		}
		if err := gormutil.PersistBoolWithDefault(tx, &c, "price_includes_igv", payload.PriceIncludesIgv); err != nil {
			return nil, err
		}
		c.PriceIncludesIgv = payload.PriceIncludesIgv
		out = append(out, c)
	}
	return out, nil
}

// comboStockContext datos de la venta que originan el movimiento de kardex.
type comboStockContext struct {
	BranchID  uint
	Reference string
	UserID    uint
}

// recordComboComponentStock descuenta el stock de los componentes de los combos vendidos.
//
// El combo se factura como una línea con su product_id, pero el que sale del almacén es cada
// componente: si el pollo lleva control de stock y el agua no, solo se mueve el pollo. Las
// comandas de componentes ya traen la cantidad multiplicada por los combos pedidos.
func recordComboComponentStock(
	tx *gorm.DB,
	inv comboStockRecorder,
	comandas []database.TenantComanda,
	ctx comboStockContext,
) error {
	// Un mismo componente puede venir en varias comandas (varios combos): se acumula para
	// dejar un solo asiento por producto, como hace el flujo de líneas normales.
	totals := map[uint]float64{}
	order := make([]uint, 0, len(comandas))
	for _, c := range comandas {
		if strings.TrimSpace(c.ComboParentKey) == "" || c.ProductID == nil {
			continue
		}
		if _, seen := totals[*c.ProductID]; !seen {
			order = append(order, *c.ProductID)
		}
		totals[*c.ProductID] += c.Quantity
	}

	for _, productID := range order {
		var product database.TenantProduct
		if tx.First(&product, productID).Error != nil {
			continue
		}
		if !product.ManageStock {
			continue
		}
		if err := inv.RecordMovementTx(tx, invsvc.MovementInput{
			ProductID:     productID,
			BranchID:      ctx.BranchID,
			Type:          "out",
			Quantity:      totals[productID],
			Reference:     ctx.Reference,
			UserID:        ctx.UserID,
			OperationCode: "SALE",
		}); err != nil {
			return err
		}
	}
	return nil
}

// comboStockRecorder lo que necesitamos del servicio de inventario (facilita el test).
type comboStockRecorder interface {
	RecordMovementTx(tx *gorm.DB, in invsvc.MovementInput) error
}

// comboChosenComponent componente ya resuelto contra el catálogo.
type comboChosenComponent struct {
	GroupID       uint
	GroupName     string
	SelectionType string
	Product       database.TenantProduct
	Quantity      float64
	ExtraPrice    float64
	AreaIDSnap    *uint // área snapshotada al armar el combo
}

// resolveComboGroupSelection aplica las reglas del grupo a lo que eligió el cliente.
func resolveComboGroupSelection(
	g database.TenantComboGroup,
	catalog []comboCatalogItem,
	picks []comboSelectionItemInput,
) ([]comboChosenComponent, error) {
	byProduct := make(map[uint]comboCatalogItem, len(catalog))
	for _, it := range catalog {
		byProduct[it.Item.ProductID] = it
	}

	switch g.SelectionType {
	case database.ComboSelectionFixed:
		// Sin elección: el componente va siempre, en su cantidad por defecto.
		if len(catalog) == 0 {
			return nil, fmt.Errorf("el grupo «%s» no tiene componente configurado", g.Name)
		}
		it := catalog[0]
		return []comboChosenComponent{newComboChosen(g, it, it.Item.DefaultQuantity)}, nil

	case database.ComboSelectionSingle:
		if len(picks) != 1 {
			return nil, fmt.Errorf("debe elegir una opción en «%s»", g.Name)
		}
		it, ok := byProduct[picks[0].ProductID]
		if !ok {
			return nil, fmt.Errorf("la opción elegida no pertenece a «%s»", g.Name)
		}
		return []comboChosenComponent{newComboChosen(g, it, it.Item.DefaultQuantity)}, nil

	case database.ComboSelectionMultiple:
		seen := make(map[uint]struct{}, len(picks))
		out := make([]comboChosenComponent, 0, len(picks))
		for _, p := range picks {
			it, ok := byProduct[p.ProductID]
			if !ok {
				return nil, fmt.Errorf("una de las opciones elegidas no pertenece a «%s»", g.Name)
			}
			if _, dup := seen[p.ProductID]; dup {
				return nil, fmt.Errorf("opción duplicada en «%s»", g.Name)
			}
			seen[p.ProductID] = struct{}{}

			qty := p.Quantity
			if !g.AllowQuantity || qty <= 0 {
				qty = it.Item.DefaultQuantity
			}
			if qty <= 0 {
				qty = 1
			}
			if g.AllowQuantity && it.Item.MaxQuantity > 0 && qty > it.Item.MaxQuantity {
				return nil, fmt.Errorf("«%s»: la cantidad máxima de «%s» es %g", g.Name, it.Product.Name, it.Item.MaxQuantity)
			}
			out = append(out, newComboChosen(g, it, qty))
		}
		if len(out) < g.MinSelect {
			return nil, fmt.Errorf("debe elegir al menos %d opción(es) en «%s»", g.MinSelect, g.Name)
		}
		if g.MaxSelect > 0 && len(out) > g.MaxSelect {
			return nil, fmt.Errorf("solo puede elegir hasta %d opción(es) en «%s»", g.MaxSelect, g.Name)
		}
		return out, nil

	default:
		return nil, fmt.Errorf("el grupo «%s» tiene un tipo de selección inválido", g.Name)
	}
}

func newComboChosen(g database.TenantComboGroup, it comboCatalogItem, qty float64) comboChosenComponent {
	if qty <= 0 {
		qty = 1
	}
	return comboChosenComponent{
		GroupID:       g.ID,
		GroupName:     g.Name,
		SelectionType: g.SelectionType,
		Product:       it.Product,
		Quantity:      qty,
		ExtraPrice:    it.Item.ExtraPrice,
		AreaIDSnap:    it.Item.PreparationAreaID,
	}
}

// comboCatalogItem item del combo junto al producto componente.
type comboCatalogItem struct {
	Item    database.TenantComboGroupItem
	Product database.TenantProduct
}

// loadComboCatalog carga grupos e items activos del combo (3 queries, sin N+1).
func loadComboCatalog(tx *gorm.DB, comboID uint) ([]database.TenantComboGroup, map[uint][]comboCatalogItem, error) {
	var groups []database.TenantComboGroup
	if err := tx.Where("product_id = ? AND active = ?", comboID, true).
		Order("sort_order ASC, id ASC").Find(&groups).Error; err != nil {
		return nil, nil, err
	}
	if len(groups) == 0 {
		return nil, map[uint][]comboCatalogItem{}, nil
	}
	groupIDs := make([]uint, 0, len(groups))
	for _, g := range groups {
		groupIDs = append(groupIDs, g.ID)
	}
	var items []database.TenantComboGroupItem
	if err := tx.Where("group_id IN ? AND active = ?", groupIDs, true).
		Order("sort_order ASC, id ASC").Find(&items).Error; err != nil {
		return nil, nil, err
	}
	productIDs := make([]uint, 0, len(items))
	for _, it := range items {
		productIDs = append(productIDs, it.ProductID)
	}
	products := map[uint]database.TenantProduct{}
	if len(productIDs) > 0 {
		var rows []database.TenantProduct
		if err := tx.Where("id IN ?", productIDs).Find(&rows).Error; err != nil {
			return nil, nil, err
		}
		for _, p := range rows {
			products[p.ID] = p
		}
	}
	byGroup := make(map[uint][]comboCatalogItem, len(groups))
	for _, it := range items {
		p, ok := products[it.ProductID]
		if !ok || !p.Active {
			// Componente borrado o desactivado tras armar el combo: no se puede servir.
			continue
		}
		byGroup[it.GroupID] = append(byGroup[it.GroupID], comboCatalogItem{Item: it, Product: p})
	}
	return groups, byGroup, nil
}

// resolveComboComponentArea rutea el componente a su área de preparación. Prioriza el id
// snapshotado en el item del combo y cae al área viva del producto.
func resolveComboComponentArea(tx *gorm.DB, c comboChosenComponent) (string, *uint) {
	areaID := c.AreaIDSnap
	if areaID == nil || *areaID == 0 {
		areaID = c.Product.PreparationAreaID
	}
	if areaID != nil && *areaID > 0 {
		var area database.TenantPreparationArea
		if err := tx.Select("slug").First(&area, *areaID).Error; err == nil {
			if slug := strings.TrimSpace(strings.ToLower(area.Slug)); slug != "" {
				return slug, areaID
			}
		}
	}
	if slug := strings.TrimSpace(strings.ToLower(c.Product.PreparationArea)); slug != "" {
		return slug, areaID
	}
	return "cocina", areaID
}

// comboSelectionSignature firma determinista de la elección: dos combos con la misma
// selección comparten línea de venta; con distinta, no.
func comboSelectionSignature(chosen []comboChosenComponent) string {
	parts := make([]string, 0, len(chosen))
	for _, c := range chosen {
		parts = append(parts, fmt.Sprintf("%d:%d:%g:%.2f", c.GroupID, c.Product.ID, c.Quantity, c.ExtraPrice))
	}
	sort.Strings(parts)
	return strings.Join(parts, ",")
}

// comboSaleLineKey clave de agrupación en la venta. No usa combo_parent_key: dos combos
// idénticos pedidos por separado deben fundirse en una sola línea, como el resto del flujo.
func comboSaleLineKey(p *comboPayload) string {
	return fmt.Sprintf("combo|%d|%s|%.4f", p.ComboID, p.Selection, p.ComboPrice)
}

// comboLineModifiersSnapshot describe los componentes del combo para la línea de venta,
// reutilizando la forma de modifiers_json que el frontend ya sabe pintar.
func comboLineModifiersSnapshot(comandas []database.TenantComanda) string {
	components := make([]comboComponentPayload, 0, len(comandas))
	for _, c := range comandas {
		p, err := parseComboPayload(c.ComboJSON)
		if err != nil || p == nil {
			continue
		}
		components = append(components, p.Component)
	}
	return comboComponentsSnapshot(components)
}

// comboDraftsSnapshot igual que comboLineModifiersSnapshot pero desde los drafts, para la
// venta directa (que no crea comandas). Ambos caminos deben producir el mismo detalle: el
// ticket de una venta rápida y el de una mesa tienen que verse igual.
func comboDraftsSnapshot(drafts []comboComponentDraft) string {
	components := make([]comboComponentPayload, 0, len(drafts))
	for _, d := range drafts {
		components = append(components, d.Payload.Component)
	}
	return comboComponentsSnapshot(components)
}

func comboComponentsSnapshot(components []comboComponentPayload) string {
	entries := make([]modifierPayloadEntry, 0, len(components))
	for _, c := range components {
		entries = append(entries, modifierPayloadEntry{
			GroupID:    c.GroupID,
			GroupName:  c.GroupName,
			Type:       "combo",
			GroupType:  "combo",
			OptionID:   c.ProductID,
			OptionName: comboComponentLabel(c),
			ExtraPrice: c.ExtraPrice,
			Snapshot:   true,
		})
	}
	if len(entries) == 0 {
		return ""
	}
	b, err := json.Marshal(entries)
	if err != nil {
		return ""
	}
	return string(b)
}

// comboComponentLabel "2 x Papas fritas" o "Agua mineral".
func comboComponentLabel(c comboComponentPayload) string {
	if c.Quantity > 1 {
		return fmt.Sprintf("%g x %s", c.Quantity, c.ProductName)
	}
	return c.ProductName
}

// billLine línea cobrable derivada de las comandas. Un combo son N comandas (una por área de
// preparación) pero una sola línea de dinero: la de su precio fijo.
type billLine struct {
	Key                string
	ProductID          *uint
	Code               string
	Name               string
	Quantity           float64
	UnitPrice          float64
	Notes              string
	ModifiersJSON      string
	IgvAffectationType string
	PriceIncludesIgv   bool
	IsCombo            bool
	// Comanda origen (solo líneas sueltas): el IGV se resuelve con comandaIgvForCalc.
	Comanda *database.TenantComanda
}

// comandasToBillLines colapsa las comandas de cada combo en una línea y deja el resto igual.
// El combo aparece en la posición de su primer componente, así que el orden se conserva.
// Fuente única de verdad para precuenta y facturación: si divergieran, el cliente vería un
// precio y pagaría otro.
func comandasToBillLines(comandas []database.TenantComanda) []billLine {
	byParent := make(map[string][]database.TenantComanda)
	for _, c := range comandas {
		if pk := strings.TrimSpace(c.ComboParentKey); pk != "" {
			byParent[pk] = append(byParent[pk], c)
		}
	}

	out := make([]billLine, 0, len(comandas))
	seen := make(map[string]struct{}, len(byParent))
	for i := range comandas {
		c := comandas[i]
		pk := strings.TrimSpace(c.ComboParentKey)
		if pk == "" {
			out = append(out, billLine{
				Key:           comandaSaleLineKey(c),
				ProductID:     c.ProductID,
				Code:          c.ProductCode,
				Name:          c.ProductName,
				Quantity:      c.Quantity,
				UnitPrice:     c.UnitPrice,
				Notes:         c.Notes,
				ModifiersJSON: strings.TrimSpace(c.ModifiersJSON),
				Comanda:       &comandas[i],
			})
			continue
		}
		if _, ok := seen[pk]; ok {
			continue // ya contabilizado por el primer componente del combo
		}
		seen[pk] = struct{}{}

		group := byParent[pk]
		payload, err := parseComboPayload(c.ComboJSON)
		if err != nil || payload == nil {
			// combo_json ilegible: se degrada a línea suelta antes que perder el ítem.
			out = append(out, billLine{
				Key:           comandaSaleLineKey(c),
				ProductID:     c.ProductID,
				Code:          c.ProductCode,
				Name:          c.ProductName,
				Quantity:      c.Quantity,
				UnitPrice:     c.UnitPrice,
				Notes:         c.Notes,
				ModifiersJSON: strings.TrimSpace(c.ModifiersJSON),
				Comanda:       &comandas[i],
			})
			continue
		}
		comboID := payload.ComboID
		out = append(out, billLine{
			Key:                comboSaleLineKey(payload),
			ProductID:          &comboID,
			Code:               payload.ComboCode,
			Name:               payload.ComboName,
			Quantity:           payload.ComboQuantity,
			UnitPrice:          payload.ComboPrice,
			Notes:              c.Notes,
			ModifiersJSON:      comboLineModifiersSnapshot(group),
			IgvAffectationType: payload.IgvAffectationType,
			PriceIncludesIgv:   payload.PriceIncludesIgv,
			IsCombo:            true,
		})
	}
	return out
}
