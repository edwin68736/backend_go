package health

import (
	"fmt"
	"runtime"
	"time"

	"tukifac/pkg/billingqueue"
	"tukifac/pkg/database"
	"tukifac/pkg/metrics"

	"github.com/gofiber/fiber/v3"
)

var startedAt = time.Now()

// Metrics expone métricas estilo Prometheus.
func Metrics(c fiber.Ctx) error {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	uptime := time.Since(startedAt).Seconds()
	body := fmt.Sprintf(`# HELP tukifac_up Backend process is running.
# TYPE tukifac_up gauge
tukifac_up 1
# HELP tukifac_uptime_seconds Uptime since process start.
# TYPE tukifac_uptime_seconds gauge
tukifac_uptime_seconds %.0f
# HELP go_goroutines Number of goroutines.
# TYPE go_goroutines gauge
go_goroutines %d
# HELP go_memstats_alloc_bytes Bytes allocated and in use.
# TYPE go_memstats_alloc_bytes gauge
go_memstats_alloc_bytes %d
# HELP go_memstats_sys_bytes Bytes obtained from system.
# TYPE go_memstats_sys_bytes gauge
go_memstats_sys_bytes %d
# HELP tukifac_tenant_pools_active Active tenant DB pools in memory.
# TYPE tukifac_tenant_pools_active gauge
tukifac_tenant_pools_active %d
# HELP tukifac_tenant_pools_opened_total Total tenant pools opened since start.
# TYPE tukifac_tenant_pools_opened_total counter
tukifac_tenant_pools_opened_total %d
# HELP tukifac_tenant_pools_evicted_total Total tenant pools evicted since start.
# TYPE tukifac_tenant_pools_evicted_total counter
tukifac_tenant_pools_evicted_total %d
# HELP tukifac_tenant_cache_hits_total Tenant metadata cache hits.
# TYPE tukifac_tenant_cache_hits_total counter
tukifac_tenant_cache_hits_total %d
# HELP tukifac_tenant_cache_misses_total Tenant metadata cache misses.
# TYPE tukifac_tenant_cache_misses_total counter
tukifac_tenant_cache_misses_total %d
# HELP tukifac_billing_queue_depth Jobs waiting in billing queue.
# TYPE tukifac_billing_queue_depth gauge
tukifac_billing_queue_depth %d
# HELP tukifac_billing_enqueued_total Billing jobs enqueued.
# TYPE tukifac_billing_enqueued_total counter
tukifac_billing_enqueued_total %d
# HELP tukifac_billing_processed_total Billing jobs processed OK.
# TYPE tukifac_billing_processed_total counter
tukifac_billing_processed_total %d
# HELP tukifac_billing_failed_total Billing jobs failed/retry.
# TYPE tukifac_billing_failed_total counter
tukifac_billing_failed_total %d
# HELP tukifac_redis_ops_ok_total Redis operations OK.
# TYPE tukifac_redis_ops_ok_total counter
tukifac_redis_ops_ok_total %d
# HELP tukifac_redis_ops_fail_total Redis operations failed.
# TYPE tukifac_redis_ops_fail_total counter
tukifac_redis_ops_fail_total %d
# HELP tukifac_migration_success_total Tenant schema migrations succeeded.
# TYPE tukifac_migration_success_total counter
tukifac_migration_success_total %d
# HELP tukifac_migration_failed_total Tenant schema migrations failed.
# TYPE tukifac_migration_failed_total counter
tukifac_migration_failed_total %d
# HELP tukifac_migration_duration_ms_total Cumulative migration duration ms.
# TYPE tukifac_migration_duration_ms_total counter
tukifac_migration_duration_ms_total %d
# HELP tukifac_fleet_pending Tenants pending schema migration.
# TYPE tukifac_fleet_pending gauge
tukifac_fleet_pending %d
# HELP tukifac_fleet_failed Tenants failed schema migration.
# TYPE tukifac_fleet_failed gauge
tukifac_fleet_failed %d
`,
		uptime,
		runtime.NumGoroutine(),
		m.Alloc,
		m.Sys,
		database.ActivePoolCount(),
		metrics.TenantPoolOpened.Load(),
		metrics.TenantPoolEvicted.Load(),
		metrics.TenantCacheHits.Load(),
		metrics.TenantCacheMisses.Load(),
		billingqueue.QueueDepth(),
		metrics.BillingQueueEnqueued.Load(),
		metrics.BillingQueueProcessed.Load(),
		metrics.BillingQueueFailed.Load(),
		metrics.RedisOpsOK.Load(),
		metrics.RedisOpsFail.Load(),
		metrics.MigrationSuccessTotal.Load(),
		metrics.MigrationFailedTotal.Load(),
		metrics.MigrationDurationMsTotal.Load(),
		metrics.FleetPending.Load(),
		metrics.FleetFailed.Load(),
	)

	c.Set(fiber.HeaderContentType, "text/plain; version=0.0.4; charset=utf-8")
	return c.SendString(body)
}
