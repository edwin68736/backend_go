package cron

import (
	"testing"
	"time"

	"tukifac/pkg/saas"
)

func TestNextFiscalSnapshotRun_HorariosFijosDeLima(t *testing.T) {
	loc := saas.LimaLocation()
	at := func(day, h, m int) time.Time { return time.Date(2026, 11, day, h, m, 0, 0, loc) }
	cases := []struct{ from, want time.Time }{
		{at(26, 0, 0), at(26, 0, 35)},
		{at(26, 0, 35), at(26, 6, 35)}, // justo en el horario: pasa al siguiente
		{at(26, 12, 0), at(26, 12, 35)},
		{at(26, 15, 45), at(26, 18, 35)},
		{at(26, 18, 36), at(27, 0, 35)},
		{at(26, 23, 59), at(27, 0, 35)},
	}
	for _, c := range cases {
		if got := nextFiscalSnapshotRun(c.from.UTC()); !got.Equal(c.want) {
			t.Errorf("desde %s: %s, want %s", c.from.Format("02 15:04"), got.In(loc).Format("02 15:04"), c.want.Format("02 15:04"))
		}
	}
	// El horario nocturno cae dentro de la franja de la pasada profunda diaria.
	run := nextFiscalSnapshotRun(at(26, 0, 0))
	if run.Hour() != fiscalSnapshotDeepHour || run.Minute() < fiscalSnapshotDeepMinute {
		t.Errorf("la pasada de las %s no activaría la profunda", run.Format("15:04"))
	}
}
