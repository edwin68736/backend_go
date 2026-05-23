package database

import "time"

// Delays de reintento tras fallo (intento 1..4+).
var fleetBackoffDelays = []time.Duration{
	1 * time.Minute,
	5 * time.Minute,
	15 * time.Minute,
	time.Hour,
}

// FleetBackoffDelay espera antes del siguiente intento según attempts (1-based tras incrementar).
func FleetBackoffDelay(attempts int) time.Duration {
	if attempts <= 0 {
		return fleetBackoffDelays[0]
	}
	idx := attempts - 1
	if idx >= len(fleetBackoffDelays) {
		return fleetBackoffDelays[len(fleetBackoffDelays)-1]
	}
	return fleetBackoffDelays[idx]
}

// FleetBackoffReady indica si el tenant puede reintentarse.
func FleetBackoffReady(nextRetryAt *time.Time, now time.Time) bool {
	if nextRetryAt == nil {
		return true
	}
	return !nextRetryAt.After(now)
}
