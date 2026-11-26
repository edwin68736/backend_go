package cron

import (
	"log/slog"
	"time"

	"tukifac/internal/fiscal/summary"
	"tukifac/pkg/cronlock"
	"tukifac/pkg/database"
	"tukifac/pkg/logger"
	"tukifac/pkg/saas"
)

const (
	// Cada 6 horas y a horas fijas de Lima (00:35, 06:35, 12:35 y 18:35): el barrido abre las bases de todos los tenants.
	// Para un tenant puntual está la actualización manual desde el panel.
	fiscalSnapshotInterval  = 6 * time.Hour
	fiscalSnapshotRunMinute = 35
	fiscalSnapshotWorkers   = 4
	// Pasada profunda (historial completo + abiertos antiguos): una vez al día a partir de las 00:30 Lima.
	fiscalSnapshotDeepHour   = 0
	fiscalSnapshotDeepMinute = 30
)

// StartFiscalTenantSnapshotWorker mantiene el resumen fiscal por tenant en la BD central
// (tenant_fiscal_daily / tenant_fiscal_health). Solo LEE las bases de los tenants.
// Ver docs/PLAN-RESUMEN-FISCAL-POR-TENANT.md.
func StartFiscalTenantSnapshotWorker() {
	go func() {
		waitForCentralSchema()
		runFiscalSnapshotLoop()
	}()
}

func runFiscalSnapshotLoop() {
	logger.L.Info("cron_started",
		slog.String("job", "fiscal_tenant_snapshot"),
		slog.Duration("interval", fiscalSnapshotInterval),
		slog.Int("workers", fiscalSnapshotWorkers),
	)

	// Al arrancar (despliegue o reinicio) se hace una pasada y luego se siguen las horas fijas.
	runFiscalSnapshotOnce()

	next := nextFiscalSnapshotRun(time.Now())
	for {
		time.Sleep(time.Until(next))
		runFiscalSnapshotOnce()
		// Desde la hora programada o desde ahora, lo que sea posterior: nunca repite el mismo horario ni acumula atrasos.
		if now := time.Now(); now.After(next) {
			next = nextFiscalSnapshotRun(now)
		} else {
			next = nextFiscalSnapshotRun(next)
		}
	}
}

// nextFiscalSnapshotRun primer horario fijo estrictamente posterior a from.
func nextFiscalSnapshotRun(from time.Time) time.Time {
	lt := from.In(saas.LimaLocation())
	run := time.Date(lt.Year(), lt.Month(), lt.Day(), 0, fiscalSnapshotRunMinute, 0, 0, lt.Location())
	for !run.After(lt) {
		run = run.Add(fiscalSnapshotInterval)
	}
	return run
}

func runFiscalSnapshotOnce() {
	if !database.IsCentralSchemaReady() || !fiscalSnapshotTablesReady() {
		return
	}
	// TTL corto: si una instancia muere con el candado tomado, caduca pronto y otra puede continuar.
	release, acquired := cronlock.TryAcquire("fiscal:tenant_snapshot", 14*time.Minute)
	if !acquired {
		logger.L.Debug("fiscal_snapshot_skipped_lock")
		return
	}
	defer release()

	deep := fiscalSnapshotNeedsDeep()
	if _, err := summary.NewScanner().ScanAll(deep, fiscalSnapshotWorkers); err != nil {
		logger.L.Warn("fiscal_snapshot_failed", slog.Any("error", err))
	}
}

// Las tablas las crea migrate-central en el deploy; si el job arranca antes, no hace nada.
func fiscalSnapshotTablesReady() bool {
	m := database.CentralDB.Migrator()
	return m.HasTable(&database.TenantFiscalDaily{}) && m.HasTable(&database.TenantFiscalHealth{})
}

// La primera vez (tabla de salud vacía) y cada noche pasada la hora configurada se hace la pasada
// profunda; el resto de los ciclos solo recalcula la ventana reciente.
func fiscalSnapshotNeedsDeep() bool {
	var n int64
	if err := database.CentralDB.Model(&database.TenantFiscalHealth{}).Count(&n).Error; err == nil && n == 0 {
		return true
	}
	now := saas.NowLima()
	if now.Hour() != fiscalSnapshotDeepHour || now.Minute() < fiscalSnapshotDeepMinute {
		return false
	}
	_, ok := cronlock.TryAcquireDaily("fiscal:tenant_snapshot_deep", now.Format("2006-01-02"), 20*time.Hour)
	return ok
}
