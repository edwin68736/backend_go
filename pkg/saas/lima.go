package saas

import "time"

const LimaTimezone = "America/Lima"

// MaxProvisionalHours límite duro de reactivación provisional.
const MaxProvisionalHours = 12

var limaLoc *time.Location

// LimaLocation zona horaria Perú (exportada para parsers HTTP).
func LimaLocation() *time.Location {
	return lima()
}

func lima() *time.Location {
	if limaLoc != nil {
		return limaLoc
	}
	loc, err := time.LoadLocation(LimaTimezone)
	if err != nil {
		limaLoc = time.FixedZone("America/Lima", -5*3600)
	} else {
		limaLoc = loc
	}
	return limaLoc
}

// NowLima hora actual en Perú.
func NowLima() time.Time {
	return time.Now().In(lima())
}

// CalendarDateLima trunca a medianoche America/Lima (solo fecha calendario).
func CalendarDateLima(t time.Time) time.Time {
	lt := t.In(lima())
	return time.Date(lt.Year(), lt.Month(), lt.Day(), 0, 0, 0, 0, lima())
}

// EndOfDayLima 23:59:59 del día calendario en Lima.
func EndOfDayLima(t time.Time) time.Time {
	d := CalendarDateLima(t)
	return d.Add(23*time.Hour + 59*time.Minute + 59*time.Second)
}

// CalendarDaysAfterEnd días calendario transcurridos desde endDate (0 = vence hoy o futuro).
func CalendarDaysAfterEnd(endDate, now time.Time) int {
	end := CalendarDateLima(endDate)
	today := CalendarDateLima(now)
	return int(today.Sub(end).Hours() / 24)
}

// CalendarDaysUntilEnd días hasta fin inclusive (0 = vence hoy).
func CalendarDaysUntilEnd(endDate, now time.Time) int {
	d := CalendarDaysAfterEnd(endDate, now)
	if d <= 0 {
		return -d
	}
	return 0
}

// NextLimaDailyRun retorna próxima ejecución 00:05 America/Lima.
func NextLimaDailyRun(from time.Time) time.Time {
	loc := lima()
	lt := from.In(loc)
	run := time.Date(lt.Year(), lt.Month(), lt.Day(), 0, 5, 0, 0, loc)
	if !lt.Before(run) {
		run = run.AddDate(0, 0, 1)
	}
	return run
}

// EffectiveProvisionalHours aplica tope 12h.
func EffectiveProvisionalHours(configured int) time.Duration {
	if configured <= 0 {
		configured = MaxProvisionalHours
	}
	if configured > MaxProvisionalHours {
		configured = MaxProvisionalHours
	}
	return time.Duration(configured) * time.Hour
}
