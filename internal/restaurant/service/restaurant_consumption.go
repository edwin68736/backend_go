package service

// F5a: insumos que consume un cobro de mesa, para llevar o delivery. Ver internal/recipes/service/consumption.go.

import (
	"strings"

	recipesvc "tukifac/internal/recipes/service"
	"tukifac/pkg/database"

	"gorm.io/gorm"
)

func uintValue(p *uint) uint {
	if p == nil {
		return 0
	}
	return *p
}

// planBillConsumption: una demanda por comanda, asociada a la línea de venta en la que se cobra. lineKeys va en el mismo orden que las
// líneas de la venta. Devuelve nil con el descuento apagado, sin consultar nada más.
func planBillConsumption(db *gorm.DB, comandas []database.TenantComanda, lineKeys []string) (*recipesvc.Plan, error) {
	active, block, err := recipesvc.ConsumptionPolicy(db)
	if err != nil || !active {
		return nil, err
	}
	position := make(map[string]int, len(lineKeys))
	for i, k := range lineKeys {
		position[k] = i
	}
	demands := make([]recipesvc.Demand, 0, len(comandas))
	for _, c := range comandas {
		if c.ProductID == nil {
			continue
		}
		key, component := comandaSaleLineKey(c), strings.TrimSpace(c.ComboParentKey) != ""
		if component {
			// Misma regla que comandasToBillLines: con combo_json ilegible la comanda se cobra como línea suelta.
			if payload, err := parseComboPayload(c.ComboJSON); err == nil && payload != nil {
				key = comboSaleLineKey(payload)
			}
		}
		line, ok := position[key]
		if !ok {
			continue
		}
		demands = append(demands, recipesvc.Demand{
			Line: line, ProductID: *c.ProductID, PresentationID: uintValue(c.PresentationID), Quantity: c.Quantity, Component: component,
		})
	}
	return recipesvc.PlanConsumption(db, block, demands)
}
