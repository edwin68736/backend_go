package service

import (
	"strings"
	"time"
)

const (
	CycleWeekly     = "weekly"
	CycleBiweekly   = "biweekly"
	CycleMonthly    = "monthly"
	CycleQuarterly  = "quarterly"
	CycleYearly     = "yearly"
	CycleCustom     = "custom"
)

// NormalizeBillingCycle devuelve un ciclo conocido o monthly por defecto.
func NormalizeBillingCycle(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case CycleWeekly, CycleBiweekly, CycleMonthly, CycleQuarterly, CycleYearly, CycleCustom:
		return strings.ToLower(strings.TrimSpace(s))
	default:
		return CycleMonthly
	}
}

// NextBillingFrom calcula la siguiente fecha de cierre de periodo a partir de una fecha ancla.
func NextBillingFrom(from time.Time, cycle string, intervalDays int) time.Time {
	c := NormalizeBillingCycle(cycle)
	switch c {
	case CycleWeekly:
		return from.AddDate(0, 0, 7)
	case CycleBiweekly:
		return from.AddDate(0, 0, 14)
	case CycleMonthly:
		return from.AddDate(0, 1, 0)
	case CycleQuarterly:
		return from.AddDate(0, 3, 0)
	case CycleYearly:
		return from.AddDate(1, 0, 0)
	case CycleCustom:
		d := intervalDays
		if d <= 0 {
			d = 30
		}
		return from.AddDate(0, 0, d)
	default:
		return from.AddDate(0, 1, 0)
	}
}

// PrevBillingFrom retrocede un periodo desde una fecha ancla (inverso de NextBillingFrom).
func PrevBillingFrom(anchor time.Time, cycle string, intervalDays int) time.Time {
	c := NormalizeBillingCycle(cycle)
	switch c {
	case CycleWeekly:
		return anchor.AddDate(0, 0, -7)
	case CycleBiweekly:
		return anchor.AddDate(0, 0, -14)
	case CycleMonthly:
		return anchor.AddDate(0, -1, 0)
	case CycleQuarterly:
		return anchor.AddDate(0, -3, 0)
	case CycleYearly:
		return anchor.AddDate(-1, 0, 0)
	case CycleCustom:
		d := intervalDays
		if d <= 0 {
			d = 30
		}
		return anchor.AddDate(0, 0, -d)
	default:
		return anchor.AddDate(0, -1, 0)
	}
}
