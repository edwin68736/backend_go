package engine

import (
	"fmt"
	"log/slog"
	"time"

	"tukifac/pkg/database"
	"tukifac/pkg/logger"
)

// CodeTargetVersion versión objetivo del binario (tenantmigrations registry).
func CodeTargetVersion() int {
	return database.TenantSchemaTargetVersion()
}

// InitTenantSchemaVersions delega a database (bootstrap V30).
func InitTenantSchemaVersions() (created, skipped int, err error) {
	return database.InitTenantSchemaVersions()
}

// BumpFleetTargetVersion sube target en central.
func BumpFleetTargetVersion(target int) (int64, error) {
	return database.BumpFleetTargetVersion(target)
}

// MigrateSingleTenant migraciones incrementales V(current+1)..V(target).
func MigrateSingleTenant(slug, dbName string, tenantID uint, current, target int) error {
	db, err := database.OpenTenantDBForMigration(dbName)
	if err != nil {
		return err
	}
	defer database.CloseTenantDB(db)

	if err := RunTenantSchemaMigrations(db, slug, current, target); err != nil {
		return err
	}
	return database.MarkTenantSchemaCompleted(tenantID, target)
}

// PendingTenantRow alias.
type PendingTenantRow = database.PendingTenantRow

// ListPendingTenants lista pendientes.
func ListPendingTenants(limit int, activeOnly bool) ([]PendingTenantRow, error) {
	return database.ListPendingSchemaTenants(limit, activeOnly)
}

// TryAcquireMigrationLock lease por tenant.
func TryAcquireMigrationLock(tenantID uint, workerID string, lease time.Duration) (bool, error) {
	return database.TryAcquireSchemaMigrationLock(tenantID, workerID, lease)
}

// ReleaseMigrationLock libera lock.
func ReleaseMigrationLock(tenantID uint) error {
	return database.ReleaseSchemaMigrationLock(tenantID)
}

// MarkTenantFailed registra fallo.
func MarkTenantFailed(tenantID uint, err error) error {
	return database.MarkTenantSchemaFailed(tenantID, err)
}

// UpsertTenantSchemaVersion para alta de tenant.
func UpsertTenantSchemaVersion(tenantID uint, current, target int, status string) error {
	return database.UpsertTenantSchemaVersion(tenantID, current, target, status)
}

// MigrateFleetOne procesa un tenant (usado por fleet).
func MigrateFleetOne(row PendingTenantRow, workerID string, lease time.Duration) error {
	ok, err := TryAcquireMigrationLock(row.TenantID, workerID, lease)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("tenant bloqueado por otro worker")
	}
	defer func() { _ = ReleaseMigrationLock(row.TenantID) }()

	logger.L.Info("fleet_tenant_start",
		slog.String("tenant", row.Slug),
		slog.Int("from", row.CurrentVersion),
		slog.Int("to", row.TargetVersion),
	)
	start := time.Now()
	err = MigrateSingleTenant(row.Slug, row.DBName, row.TenantID, row.CurrentVersion, row.TargetVersion)
	if err != nil {
		return err
	}
	logger.L.Info("fleet_tenant_success",
		slog.String("tenant", row.Slug),
		slog.Duration("duration", time.Since(start)),
	)
	return nil
}
