// Package metrics expone contadores atómicos para Prometheus (/metrics).
package metrics

import "sync/atomic"

var (
	TenantPoolActive   atomic.Int64
	TenantPoolEvicted  atomic.Int64
	TenantPoolOpened   atomic.Int64

	TenantCacheHits    atomic.Int64
	TenantCacheMisses  atomic.Int64
	TenantCacheNegHits atomic.Int64

	BillingQueueEnqueued atomic.Int64
	BillingQueueProcessed atomic.Int64
	BillingQueueFailed    atomic.Int64
	BillingQueueDeadLetter atomic.Int64

	FiscalQueueEnqueued atomic.Int64
	FiscalQueueProcessed atomic.Int64

	RedisOpsOK   atomic.Int64
	RedisOpsFail atomic.Int64

	MigrationDurationMsTotal atomic.Int64
	MigrationFailedTotal     atomic.Int64
	MigrationSuccessTotal    atomic.Int64
	SchemaLagTenants         atomic.Int64
	FleetPending             atomic.Int64
	FleetFailed              atomic.Int64
)
