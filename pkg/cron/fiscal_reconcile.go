package cron

import (
	"log/slog"
	"time"

	billworker "tukifac/internal/billing/worker"
	"tukifac/pkg/cronlock"
	"tukifac/pkg/database"
	"tukifac/pkg/logger"
)

const fiscalReconcileInterval = 7 * time.Minute

// StartFiscalReconcileWorker fallback cuando falla webhook facturador → ERP.
func StartFiscalReconcileWorker() {
	go func() {
		waitForCentralSchema()
		runFiscalReconcileLoop()
	}()
}

func runFiscalReconcileLoop() {
	logger.L.Info("cron_started",
		slog.String("job", "fiscal_reconcile"),
		slog.Duration("interval", fiscalReconcileInterval),
	)

	runFiscalReconcileOnce(true)

	ticker := time.NewTicker(fiscalReconcileInterval)
	defer ticker.Stop()
	for range ticker.C {
		runFiscalReconcileOnce(false)
	}
}

func runFiscalReconcileOnce(initial bool) {
	if !database.IsCentralSchemaReady() {
		return
	}
	release, acquired := cronlock.TryAcquire("fiscal:billing_reconcile", 6*time.Minute)
	if !acquired {
		logger.L.Debug("fiscal_reconcile_skipped_lock", slog.Bool("initial", initial))
		return
	}
	defer release()

	billworker.ReconcileAllTenants()
}
