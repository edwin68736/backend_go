package service

import (
	"strings"

	"tukifac/pkg/database"
	"tukifac/pkg/saas/pricing"
)

// PlanOption una de las 12 opciones del ítem «Plan Tukifac» (plan × periodo).
type PlanOption struct {
	ID     uint    `json:"id"`     // plan de Tukifac (saas_plans) al que corresponde
	Name   string  `json:"name"`   // «Microempresa plan mensual»
	Months int     `json:"months"` // 1, 6 o 12
	Price  float64 `json:"price"`  // importe del periodo completo, con el descuento del ciclo si lo tiene
}

// Los 4 planes que se venden desde el panel y los 3 periodos. El resto de planes de saas_plans (pruebas, gratis,
// ilimitado…) no se ofrecen aquí; los pedidos antiguos con otros nombres conservan su descripción.
var (
	planTiers   = []struct{ Match, Label string }{{"microempresa", "Microempresa"}, {"emprendedor", "Emprendedor"}, {"premium", "Premium"}, {"corporativo", "Corporativo"}}
	planPeriods = []struct {
		Months int
		Label  string
	}{{1, "mensual"}, {6, "semestral"}, {12, "anual"}}
)

// ListPlans devuelve las opciones fijas «<Plan> plan mensual/semestral/anual». El precio sale de saas_plans (precio mensual
// × meses) con el descuento configurado para ese ciclo en el panel central.
func (s *Service) ListPlans() ([]PlanOption, error) {
	var plans []database.SaasPlan
	if err := s.db.Where("active = ?", true).Find(&plans).Error; err != nil {
		return nil, err
	}
	var cycles []database.SaasPlanCycle
	if err := s.db.Find(&cycles).Error; err != nil {
		return nil, err
	}
	disc := map[[2]int]pricing.Discount{}
	for _, c := range cycles {
		disc[[2]int{int(c.PlanID), c.Months}] = pricing.Discount{Type: c.DiscountType, Value: c.DiscountValue}
	}
	out := []PlanOption{}
	for _, t := range planTiers {
		for _, p := range plans {
			if strings.ToLower(strings.TrimSpace(p.Name)) != t.Match {
				continue
			}
			for _, per := range planPeriods {
				amt := pricing.ComputeCycleAmounts(p.Price, per.Months, disc[[2]int{int(p.ID), per.Months}])
				out = append(out, PlanOption{ID: p.ID, Name: t.Label + " plan " + per.Label, Months: per.Months, Price: amt.Net})
			}
			break
		}
	}
	return out, nil
}
