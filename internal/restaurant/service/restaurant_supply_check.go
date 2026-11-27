package service

// F5b: avisar al pedir qué insumos no alcanzan. Calcula lo mismo que se descontará al cobrar (recetas, presentaciones y combos) y
// suma lo ya pedido en la mesa y aún sin cobrar, igual que la validación de stock al agregar el pedido. No reserva ni escribe nada.

import (
	"strings"

	recipesvc "tukifac/internal/recipes/service"
	"tukifac/pkg/database"
)

type SupplyCheck struct {
	Active      bool                 `json:"active"`
	StockPolicy string               `json:"stock_policy"`
	Shortages   []recipesvc.Shortage `json:"shortages"`
}

// CheckSupplies: items es lo que está en el carrito, con la misma forma que se envía al pedir. Una línea que no se pueda resolver se
// ignora: el error lo mostrará el pedido, no el aviso.
func (s *RestaurantService) CheckSupplies(branchID, sessionID uint, items []NewOrderItem) (SupplyCheck, error) {
	out := SupplyCheck{StockPolicy: recipesvc.PolicyAllow, Shortages: []recipesvc.Shortage{}}
	active, block, err := recipesvc.ConsumptionPolicy(s.db)
	if err != nil || !active {
		return out, err
	}
	out.Active = true
	if block {
		out.StockPolicy = recipesvc.PolicyBlock
	}

	var demands []recipesvc.Demand
	line := 0
	for _, item := range items { // copia: resolver la línea la modifica
		if item.ProductID == nil || *item.ProductID == 0 || item.Quantity <= 0 {
			continue
		}
		product, err := resolveRestaurantOrderItem(s.db, &item)
		if err != nil || product == nil {
			continue
		}
		if product.HasCombo {
			drafts, err := resolveComboOrderItem(s.db, &item, product)
			if err != nil {
				continue
			}
			for _, d := range drafts {
				demands = append(demands, recipesvc.Demand{Line: line, ProductID: d.ProductID, Quantity: item.Quantity * d.Quantity, Component: true})
			}
		} else {
			demands = append(demands, recipesvc.Demand{Line: line, ProductID: product.ID, PresentationID: uintValue(item.PresentationID),
				Quantity: item.Quantity})
		}
		line++
	}
	// Sin nada nuevo en el carrito no se avisa por lo que la mesa ya pidió: eso ya se avisó al pedirlo.
	if len(demands) == 0 {
		return out, nil
	}
	if sessionID > 0 {
		var comandas []database.TenantComanda
		if err := s.db.Joins("JOIN tenant_table_sessions ts ON ts.id = tenant_comandas.session_id AND ts.branch_id = ?", branchID).
			Where("tenant_comandas.session_id = ? AND tenant_comandas.cancelled_at IS NULL AND tenant_comandas.billed_at IS NULL", sessionID).
			Find(&comandas).Error; err != nil {
			return out, err
		}
		for _, c := range comandas {
			if c.ProductID == nil {
				continue
			}
			demands = append(demands, recipesvc.Demand{Line: line, ProductID: *c.ProductID, PresentationID: uintValue(c.PresentationID),
				Quantity: c.Quantity, Component: strings.TrimSpace(c.ComboParentKey) != ""})
			line++
		}
	}
	plan, err := recipesvc.PlanConsumption(s.db, block, demands)
	if err != nil {
		return out, err
	}
	shortages, err := plan.Shortages(s.db, branchID)
	if err != nil {
		return out, err
	}
	out.Shortages = shortages
	return out, nil
}
