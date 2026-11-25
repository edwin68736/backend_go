package service

import (
	"math"
	"testing"
)

func TestRoundCommercial(t *testing.T) {
	cases := []struct {
		price     float64
		mode, dir string
		want      float64
	}{
		{16.3233, "none", "up", 16.33}, {16.3233, "0.10", "up", 16.40}, {16.3233, "0.50", "up", 16.50},
		{16.3233, "1.00", "up", 17}, {16.3233, "x.90", "up", 16.90}, {16.95, "x.90", "up", 17.90},
		{16.3233, "none", "nearest", 16.32}, {16.3233, "0.10", "nearest", 16.30}, {16.3233, "0.50", "nearest", 16.50},
		{16.3233, "1.00", "nearest", 16}, {16.3233, "x.90", "nearest", 15.90}, {16.70, "x.90", "nearest", 16.90},
		// Un precio que ya cae justo en el paso no sube por decimales de coma flotante.
		{16.40, "0.10", "up", 16.40}, {13.8 * 1.18 / 1.18, "0.10", "up", 13.80}, {0.1 + 0.2, "0.10", "up", 0.30},
		{16.90, "x.90", "up", 16.90}, {0, "0.10", "up", 0},
	}
	for _, c := range cases {
		if got := roundCommercial(c.price, c.mode, c.dir); math.Abs(got-c.want) > 1e-9 {
			t.Errorf("%v %s %s = %v, quería %v", c.price, c.mode, c.dir, got, c.want)
		}
	}
}

func TestCosting(t *testing.T) {
	s := DefaultSettings()
	// Ejemplo aprobado: costo 4.15, objetivo 30 % → 13.83 neto → 16.32 con IGV → 16.40 (29.86 %).
	c := costing(4.15, 15, priceTax{rate: 0.18, includes: true}, 30, s)
	if c.SuggestedNet != 13.83 || c.SuggestedPrice != 16.40 || c.SuggestedFoodCost != 29.86 {
		t.Fatalf("sugerido: %+v", c)
	}
	if c.NetPrice != 12.71 || c.FoodCostPct != 32.65 || !c.OverTarget || c.Margin != 8.56 {
		t.Fatalf("actual: %+v", c)
	}
	// Exonerado o Nuevo RUS: el precio es el neto.
	c = costing(4.15, 15, priceTax{}, 30, s)
	if c.NetPrice != 15 || c.FoodCostPct != 27.67 || c.OverTarget || c.SuggestedPrice != 13.90 {
		t.Fatalf("sin IGV: %+v", c)
	}
	// Precio publicado que NO incluye IGV: no se le suma ni se le resta.
	c = costing(4.15, 15, priceTax{rate: 0.18}, 30, s)
	if c.NetPrice != 15 || c.SuggestedPrice != 13.90 {
		t.Fatalf("precio sin IGV incluido: %+v", c)
	}
	// Sin precio actual: no se inventa food cost.
	c = costing(4.15, 0, priceTax{rate: 0.18, includes: true}, 30, s)
	if !c.CurrentPriceMissing || c.FoodCostPct != 0 || c.SuggestedPrice != 16.40 {
		t.Fatalf("sin precio: %+v", c)
	}
}

func TestUnitFactor(t *testing.T) {
	for _, c := range []struct {
		base, written string
		want          float64
		ok            bool
	}{
		{"KGM", "GRM", 0.001, true}, {"LTR", "MLT", 0.001, true}, {"GRM", "KGM", 1000, true}, {"MLT", "LTR", 1000, true},
		{"KGM", "", 1, true}, {"NIU", "NIU", 1, true}, {"NIU", "GRM", 0, false}, {"KGM", "MLT", 0, false},
	} {
		got, ok := unitFactor(c.base, c.written)
		if got != c.want || ok != c.ok {
			t.Errorf("%s/%s = %v %v", c.base, c.written, got, ok)
		}
	}
}
