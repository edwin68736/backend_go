package service

import (
	"testing"
	"time"

	"tukifac/pkg/datespe"
)

func TestResolveElectronicIssueDate(t *testing.T) {
	loc := datespe.Location()
	now := time.Date(2026, 10, 22, 15, 0, 0, 0, loc)
	old := time.Date(2026, 10, 10, 12, 0, 0, 0, loc)
	cases := []struct {
		name, raw string
		nota      time.Time
		wantErr   bool
		wantDay   string
	}{
		{"hoy", "2026-10-22", old, false, "2026-10-22"},
		{"hace 3 días", "2026-10-19", old, false, "2026-10-19"},
		{"hace 4 días", "2026-10-18", old, true, ""},
		{"futura", "2026-10-23", old, true, ""},
		{"inválida", "22/10/2026", old, true, ""},
		{"vacía con nota vieja usa hoy", "", old, false, "2026-10-22"},
		{"vacía con nota reciente conserva su fecha", "", time.Date(2026, 10, 21, 9, 0, 0, 0, loc), false, "2026-10-21"},
	}
	for _, c := range cases {
		got, err := resolveElectronicIssueDate(c.raw, c.nota, now)
		if (err != nil) != c.wantErr {
			t.Errorf("%s: err = %v, wantErr %v", c.name, err, c.wantErr)
			continue
		}
		if err == nil && got.In(loc).Format("2006-01-02") != c.wantDay {
			t.Errorf("%s: día = %s, want %s", c.name, got.In(loc).Format("2006-01-02"), c.wantDay)
		}
	}
}
