// Package service: recetas, costo, precio sugerido y disponibilidad (F4a). Solo lee costos y stock: no toca la venta.
package service

import (
	"math"
	"strings"
)

// Settings configuración del precio sugerido.
type Settings struct {
	TargetFoodCostPct float64 `json:"target_food_cost_pct"`
	PriceRounding     string  `json:"price_rounding"`     // none | 0.10 | 0.50 | 1.00 | x.90
	RoundingDirection string  `json:"rounding_direction"` // up | nearest
	CostBasis         string  `json:"recipe_cost_basis"`  // avg | last
}

func DefaultSettings() Settings {
	return Settings{TargetFoodCostPct: 30, PriceRounding: "0.10", RoundingDirection: "up", CostBasis: "avg"}
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }
func round6(v float64) float64 { return math.Round(v*1e6) / 1e6 }

// roundCommercial redondea el precio publicado. Trabaja en céntimos enteros para que 16.40 no «suba» a 16.50 por decimales.
func roundCommercial(price float64, mode, direction string) float64 {
	if price <= 0 {
		return 0
	}
	cents := math.Ceil(math.Round(price*1e6)/1e4 - 1e-9) // céntimos, hacia arriba
	if direction == "nearest" {
		cents = math.Round(price * 100)
	}
	step := map[string]float64{"0.10": 10, "0.50": 50, "1.00": 100}[mode]
	switch {
	case step > 0 && direction == "nearest":
		return math.Round(cents/step) * step / 100
	case step > 0:
		return math.Ceil(cents/step) * step / 100
	case mode == "x.90":
		base := math.Floor(cents/100)*100 + 90
		if direction == "nearest" {
			if cents-(base-100) < base-cents {
				base -= 100
			}
		} else if base < cents {
			base += 100
		}
		return base / 100
	}
	return cents / 100
}

// priceTax describe cómo se relaciona el precio publicado de un plato con su precio sin IGV.
type priceTax struct {
	rate     float64 // 0.18; 0 si es exonerado/inafecto o la empresa no recupera IGV (Nuevo RUS)
	includes bool    // el precio publicado ya trae el IGV
}

func (t priceTax) net(published float64) float64 {
	if t.includes && t.rate > 0 {
		return published / (1 + t.rate)
	}
	return published
}

func (t priceTax) published(net float64) float64 {
	if t.includes && t.rate > 0 {
		return net * (1 + t.rate)
	}
	return net
}

// Costing resultado económico de un plato. Los porcentajes van de 0 a 100.
type Costing struct {
	PortionCost         float64 `json:"portion_cost"`
	NetPrice            float64 `json:"net_price"`
	NetFactor           float64 `json:"net_factor"` // sol neto por cada sol publicado (para mostrar el food cost de otro precio)
	FoodCostPct         float64 `json:"food_cost_pct"`
	Margin              float64 `json:"margin"`
	MarginPct           float64 `json:"margin_pct"`
	SuggestedPrice      float64 `json:"suggested_price"`
	SuggestedNet        float64 `json:"suggested_net"`
	SuggestedFoodCost   float64 `json:"suggested_food_cost_pct"` // el real tras redondear
	TargetFoodCostPct   float64 `json:"target_food_cost_pct"`
	OverTarget          bool    `json:"over_target"`
	CurrentPriceMissing bool    `json:"current_price_missing,omitempty"`
}

// cost calcula food cost, margen y precio sugerido. targetPct es el food cost objetivo (0-100).
func costing(portionCost, price float64, tax priceTax, targetPct float64, s Settings) Costing {
	c := Costing{PortionCost: round6(portionCost), TargetFoodCostPct: targetPct, NetPrice: round2(tax.net(price)), NetFactor: round6(tax.net(1))}
	if net := tax.net(price); net > 0 {
		c.FoodCostPct = round2(portionCost / net * 100)
		c.Margin = round2(net - portionCost)
		c.MarginPct = round2((net - portionCost) / net * 100)
		c.OverTarget = c.FoodCostPct > targetPct+0.005
	} else {
		c.CurrentPriceMissing = true
	}
	if portionCost > 0 && targetPct > 0 {
		c.SuggestedNet = round2(portionCost / (targetPct / 100))
		c.SuggestedPrice = roundCommercial(tax.published(portionCost/(targetPct/100)), strings.TrimSpace(s.PriceRounding), s.RoundingDirection)
		if net := tax.net(c.SuggestedPrice); net > 0 {
			c.SuggestedFoodCost = round2(portionCost / net * 100)
		}
	}
	return c
}

// unitFactor: cuántas unidades base del insumo es una unidad escrita en la receta. Vacío = la misma unidad base.
func unitFactor(base, written string) (float64, bool) {
	base, written = strings.ToUpper(strings.TrimSpace(base)), strings.ToUpper(strings.TrimSpace(written))
	if written == "" || written == base {
		return 1, true
	}
	switch {
	case base == "KGM" && written == "GRM", base == "LTR" && written == "MLT":
		return 0.001, true
	case base == "GRM" && written == "KGM", base == "MLT" && written == "LTR":
		return 1000, true
	}
	return 0, false
}
