package sunat

import "testing"

func TestNormalizeUnit(t *testing.T) {
	cases := []struct {
		unit, typ, want string
	}{
		{"", "product", "NIU"},
		{"UND", "product", "NIU"},
		{"und", "product", "NIU"},
		{"NIU", "product", "NIU"},
		{"KGM", "product", "KGM"},
		{"", "service", "ZZ"},
		{"UND", "service", "ZZ"},
		{"NIU", "service", "ZZ"},
		{"ZZ", "", "ZZ"},
		{"UND", "", "NIU"},
	}
	for _, c := range cases {
		if got := NormalizeUnit(c.unit, c.typ); got != c.want {
			t.Fatalf("NormalizeUnit(%q, %q) = %q, want %q", c.unit, c.typ, got, c.want)
		}
	}
}
