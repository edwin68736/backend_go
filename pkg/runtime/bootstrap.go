package runtime

import (
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"tukifac/config"
	billworker "tukifac/internal/billing/worker"
	"tukifac/pkg/billingqueue"
	"tukifac/pkg/database"
	"tukifac/pkg/logger"
	"tukifac/pkg/tenantcache"
)

// Init infraestructura de producción (Redis, tenant cache, DB manager, billing workers).
func Init(cfg *config.Config) error {
	if err := database.ConnectCentral(); err != nil {
		return err
	}
	if err := database.EnsureCentralFleetSchema(); err != nil {
		return err
	}
	database.InitTenantDBManager(cfg)

	rdb := tenantcache.InitRedis(cfg)
	tenantcache.Init(cfg, rdb)

	billingqueue.Start(cfg, rdb, billworker.ProcessJob)

	logger.L.Info("runtime_initialized",
		slog.Bool("redis", rdb != nil),
		slog.Bool("billing_async", billingqueue.Enabled()),
		slog.Int("tenant_pool_max", cfg.TenantPoolMaxActive),
	)
	return nil
}

// Shutdown graceful de pools y Redis.
func Shutdown() {
	billingqueue.Stop()
	database.ShutdownTenantDBManager()
	_ = tenantcache.Close()
}

// ListenShutdown registra SIGINT/SIGTERM para cerrar pools.
func ListenShutdown() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-ch
		logger.L.Info("shutdown_signal_received")
		Shutdown()
		os.Exit(0)
	}()
}
