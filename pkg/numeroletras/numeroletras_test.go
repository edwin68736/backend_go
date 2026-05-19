package numeroletras

import "testing"

func TestMontoEnLetras(t *testing.T) {
	tests := []struct {
		monto float64
		mon   string
		want  string
	}{
		{20, "PEN", "VEINTE CON 00/100 SOLES"},
		{28, "PEN", "VEINTIOCHO CON 00/100 SOLES"},
		{0, "PEN", "CERO CON 00/100 SOLES"},
	}
	for _, tc := range tests {
		got := MontoEnLetras(tc.monto, tc.mon)
		if got != tc.want {
			t.Errorf("MontoEnLetras(%v, %q) = %q, want %q", tc.monto, tc.mon, got, tc.want)
		}
	}
}
