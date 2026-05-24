package facturador

import (
	"testing"
	"time"
)

func TestFormatFiscalDateTime(t *testing.T) {
	loc := limaLocation()
	in := time.Date(2026, 5, 24, 12, 0, 0, 0, loc)
	got := FormatFiscalDateTime(in)
	want := "2026-05-24T12:00:00-05:00"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}
