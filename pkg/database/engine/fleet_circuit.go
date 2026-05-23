package engine

import (
	"fmt"
	"sync/atomic"

	"tukifac/config"
	"tukifac/pkg/database"
	"tukifac/pkg/migrationalert"
)

// FleetCircuitThreshold fallos consecutivos antes de abrir circuit breaker.
func FleetCircuitThreshold() int {
	n := 10
	if config.AppConfig != nil && config.AppConfig.FleetCircuitBreakerThreshold > 0 {
		n = config.AppConfig.FleetCircuitBreakerThreshold
	}
	return n
}

type fleetCircuitTracker struct {
	consecutive atomic.Int32
	tripped     atomic.Bool
}

func (t *fleetCircuitTracker) onSuccess() {
	t.consecutive.Store(0)
}

func (t *fleetCircuitTracker) onFailure(tenantSlug string, err error) bool {
	if t.tripped.Load() {
		return true
	}
	n := t.consecutive.Add(1)
	threshold := int32(FleetCircuitThreshold())
	if n < threshold {
		return false
	}
	reason := fmt.Sprintf("%d fallos consecutivos en fleet (último: %s: %v)", threshold, tenantSlug, err)
	_ = database.TripFleetCircuitBreaker(reason)
	t.tripped.Store(true)
	migrationalert.NotifyCircuitBreakerOpen(reason, int(threshold))
	return true
}

func (t *fleetCircuitTracker) isTripped() bool {
	return t.tripped.Load()
}
