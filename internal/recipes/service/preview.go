package service

// F5b: lo que las pantallas necesitan saber del descuento ANTES de cobrar: para cuántas porciones alcanza cada plato, qué insumos no
// alcanzan para un pedido y qué revisar antes de encenderlo. Nada de esto escribe ni expone costos.

import (
	"math"
	"sort"

	"tukifac/pkg/database"

	"gorm.io/gorm"
)

// ── Porciones por plato ──

// DishStock: un plato (o presentación) que descuenta por receta. All: todas las presentaciones del plato tienen receta, así que su
// stock propio no se usa en ninguna.
type DishStock struct {
	ProductID      uint   `json:"product_id"`
	PresentationID uint   `json:"presentation_id"`
	Portions       *int   `json:"portions"` // vacío = no se puede saber (p. ej. combo incompleto)
	Limiting       string `json:"limiting,omitempty"`
	Combo          bool   `json:"combo,omitempty"`
	All            bool   `json:"all_presentations"`
}

// Availability: con el descuento apagado Active es false y no se informa ningún plato.
type Availability struct {
	Active      bool        `json:"active"`
	StockPolicy string      `json:"stock_policy"`
	Dishes      []DishStock `json:"dishes"`
}

func policyName(block bool) string {
	if block {
		return PolicyBlock
	}
	return PolicyAllow
}

// Availability: platos de la sucursal que hoy descuentan insumos y para cuántas porciones alcanza cada uno.
func (s *Service) Availability(branchID uint) (Availability, error) {
	out := Availability{StockPolicy: PolicyAllow, Dishes: []DishStock{}}
	active, block, err := ConsumptionPolicy(s.db)
	if err != nil || !active {
		return out, err
	}
	out.Active, out.StockPolicy = true, policyName(block)
	dishes, err := s.Dishes(Query{BranchID: branchID})
	if err != nil {
		return out, err
	}
	all := map[uint]bool{}
	for _, d := range dishes {
		if prev, seen := all[d.ProductID]; !seen || prev {
			all[d.ProductID] = d.HasRecipe
		}
	}
	for _, d := range dishes {
		if !d.HasRecipe {
			continue
		}
		out.Dishes = append(out.Dishes, DishStock{ProductID: d.ProductID, PresentationID: d.PresentationID, Portions: d.Available,
			Limiting: d.Limiting, Combo: d.IsCombo, All: all[d.ProductID]})
	}
	return out, nil
}

// ── Faltantes de un pedido ──

// Shortage: insumo que no alcanza para lo pedido.
type Shortage struct {
	SupplyID  uint     `json:"supply_id"`
	Name      string   `json:"name"`
	Unit      string   `json:"unit"`
	Required  float64  `json:"required"`
	Available float64  `json:"available"`
	Dishes    []string `json:"dishes"` // platos del pedido que lo usan
}

// Shortages compara lo que consumiría el plan con el stock de la sucursal. Es exactamente la cantidad que se descontaría al cobrar.
func (p *Plan) Shortages(db *gorm.DB, branchID uint) ([]Shortage, error) {
	out := []Shortage{}
	if p == nil {
		return out, nil
	}
	required := map[uint]float64{}
	dishes := map[uint]map[string]bool{}
	for _, k := range p.demands {
		recipe, _ := p.recipeFor(k.productID, k.presentationID)
		for _, l := range p.lines[recipe.ID] {
			qty := consumedQty(p.sold[k], l, recipe.YieldQty)
			if qty <= 0 {
				continue
			}
			required[l.ComponentProductID] = round3(required[l.ComponentProductID] + qty)
			if dishes[l.ComponentProductID] == nil {
				dishes[l.ComponentProductID] = map[string]bool{}
			}
			dishes[l.ComponentProductID][p.names[k.productID]] = true
		}
	}
	if len(required) == 0 {
		return out, nil
	}
	ids := make([]uint, 0, len(required))
	for id := range required {
		ids = append(ids, id)
	}
	var stocks []struct {
		ProductID uint
		Quantity  float64
	}
	if err := db.Table("tenant_product_stocks").Select("product_id, quantity").
		Where("product_id IN ? AND branch_id = ?", ids, branchID).Find(&stocks).Error; err != nil {
		return nil, err
	}
	available := map[uint]float64{}
	for _, st := range stocks {
		available[st.ProductID] += st.Quantity
	}
	var supplies []database.TenantProduct
	if err := db.Unscoped().Select("id", "name", "unit").Where("id IN ?", ids).Find(&supplies).Error; err != nil {
		return nil, err
	}
	for _, sp := range supplies {
		have := round3(available[sp.ID])
		if required[sp.ID] <= have+0.0005 {
			continue
		}
		names := make([]string, 0, len(dishes[sp.ID]))
		for n := range dishes[sp.ID] {
			names = append(names, n)
		}
		sort.Strings(names)
		out = append(out, Shortage{SupplyID: sp.ID, Name: sp.Name, Unit: sp.Unit, Required: required[sp.ID],
			Available: math.Max(have, 0), Dishes: names})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// ── Lista de verificación antes de encender ──

type ChecklistDish struct {
	ProductID      uint   `json:"product_id"`
	PresentationID uint   `json:"presentation_id"`
	Name           string `json:"name"`
}

type ChecklistSupply struct {
	SupplyID uint    `json:"supply_id"`
	Name     string  `json:"name"`
	Unit     string  `json:"unit"`
	Stock    float64 `json:"stock"`
}

// Checklist: qué pasará al encender el descuento en la sucursal.
type Checklist struct {
	DishesWithRecipe     int               `json:"dishes_with_recipe"`
	DishesWithoutRecipe  []ChecklistDish   `json:"dishes_without_recipe"`  // seguirán como hoy
	CombosIncomplete     []ChecklistDish   `json:"combos_incomplete"`      // algún componente sin receta: sin costo de la línea
	SuppliesWithoutStock []ChecklistSupply `json:"supplies_without_stock"` // se venderá en negativo o se bloqueará
	SuppliesWithoutCost  []ChecklistSupply `json:"supplies_without_cost"`  // el costo de la venta quedará vacío
	OwnStockReplaced     []ChecklistDish   `json:"own_stock_replaced"`     // dejarán de descontar su propio stock
}

func (s *Service) Checklist(branchID uint) (Checklist, error) {
	out := Checklist{DishesWithoutRecipe: []ChecklistDish{}, CombosIncomplete: []ChecklistDish{}, SuppliesWithoutStock: []ChecklistSupply{},
		SuppliesWithoutCost: []ChecklistSupply{}, OwnStockReplaced: []ChecklistDish{}}
	dishes, err := s.Dishes(Query{BranchID: branchID, WithCost: true, WithLines: true})
	if err != nil {
		return out, err
	}
	noStock, noCost := map[uint]ChecklistSupply{}, map[uint]ChecklistSupply{}
	for _, d := range dishes {
		item := ChecklistDish{ProductID: d.ProductID, PresentationID: d.PresentationID, Name: d.Name}
		if d.PresentationName != "" {
			item.Name += " (" + d.PresentationName + ")"
		}
		if !d.HasRecipe {
			if d.IsCombo {
				out.CombosIncomplete = append(out.CombosIncomplete, item)
			} else {
				out.DishesWithoutRecipe = append(out.DishesWithoutRecipe, item)
			}
			continue
		}
		out.DishesWithRecipe++
		if d.manageStock && !d.IsCombo {
			out.OwnStockReplaced = append(out.OwnStockReplaced, item)
		}
		for _, l := range d.Lines {
			sup := ChecklistSupply{SupplyID: l.SupplyID, Name: l.Name, Unit: l.BaseUnit, Stock: l.Stock}
			if l.Stock <= 0 {
				noStock[l.SupplyID] = sup
			}
			if l.CostMissing {
				noCost[l.SupplyID] = sup
			}
		}
	}
	list := func(m map[uint]ChecklistSupply) []ChecklistSupply {
		out := make([]ChecklistSupply, 0, len(m))
		for _, v := range m {
			out = append(out, v)
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
		return out
	}
	out.SuppliesWithoutStock, out.SuppliesWithoutCost = list(noStock), list(noCost)
	return out, nil
}
