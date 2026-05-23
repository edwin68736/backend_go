package engine

import (
	"fmt"
	"log/slog"
	"time"

	"tukifac/pkg/database"
	"tukifac/pkg/database/tenantmigrations"
	"tukifac/pkg/logger"
	"tukifac/pkg/metrics"

	"gorm.io/gorm"
)

// RunTenantSchemaMigrations ejecuta migraciones incrementales fromV+1 .. toV en la BD tenant.
func RunTenantSchemaMigrations(db *gorm.DB, slug string, fromV, toV int) error {
	if toV <= fromV {
		return nil
	}
	byVer := tenantmigrations.ByVersion()
	if err := ensureTenantMigrationHistoryTable(db); err != nil {
		return err
	}

	for v := fromV + 1; v <= toV; v++ {
		mig, ok := byVer[v]
		if !ok {
			return fmt.Errorf("migración V%d no registrada en tenantmigrations", v)
		}
		if applied, _ := isHistoryApplied(db, v, database.MigrationHistoryTypeSchema); applied {
			logger.L.Info("tenant_migration_skip",
				slog.String("tenant", slug),
				slog.Int("version", v),
				slog.String("reason", "already_applied"),
			)
			continue
		}

		logger.L.Info("tenant_migration_start",
			slog.String("tenant", slug),
			slog.Int("from", v-1),
			slog.Int("to", v),
			slog.String("name", mig.Name()),
		)
		start := time.Now()
		err := mig.Up(db)
		dur := time.Since(start)
		if err != nil {
			_ = recordHistory(db, v, mig.Name(), database.MigrationHistoryTypeSchema, dur, false, err.Error())
			metrics.MigrationFailedTotal.Add(1)
			logger.L.Error("tenant_migration_failed",
				slog.String("tenant", slug),
				slog.Int("from_version", v-1),
				slog.Int("to_version", v),
				slog.Int64("duration_ms", dur.Milliseconds()),
				slog.String("status", "failed"),
				slog.Any("error", err),
			)
			return fmt.Errorf("V%d %s: %w", v, mig.Name(), err)
		}
		_ = recordHistory(db, v, mig.Name(), database.MigrationHistoryTypeSchema, dur, true, "")
		metrics.MigrationDurationMsTotal.Add(dur.Milliseconds())
		metrics.MigrationSuccessTotal.Add(1)
		logger.L.Info("tenant_migration_success",
			slog.String("tenant", slug),
			slog.Int("from_version", v-1),
			slog.Int("to_version", v),
			slog.Int64("duration_ms", dur.Milliseconds()),
			slog.String("status", "success"),
		)
	}
	return nil
}

func ensureTenantMigrationHistoryTable(db *gorm.DB) error {
	return db.AutoMigrate(&database.TenantMigrationHistory{})
}

func isHistoryApplied(db *gorm.DB, version int, typ string) (bool, error) {
	if !db.Migrator().HasTable(&database.TenantMigrationHistory{}) {
		return false, nil
	}
	var n int64
	err := db.Model(&database.TenantMigrationHistory{}).
		Where("version = ? AND type = ? AND success = ?", version, typ, true).
		Count(&n).Error
	return n > 0, err
}

func recordHistory(db *gorm.DB, version int, name, typ string, dur time.Duration, success bool, errMsg string) error {
	if !db.Migrator().HasTable(&database.TenantMigrationHistory{}) {
		if err := ensureTenantMigrationHistoryTable(db); err != nil {
			return err
		}
	}
	row := database.TenantMigrationHistory{
		Version:    version,
		Name:       name,
		Type:       typ,
		AppliedAt:  time.Now(),
		DurationMs: dur.Milliseconds(),
		Success:    success,
	}
	if errMsg != "" {
		row.Error = &errMsg
	}
	return db.Create(&row).Error
}

// IsBackfillApplied consulta history run-once para backfill.
func IsBackfillApplied(db *gorm.DB, version int) (bool, error) {
	return isHistoryApplied(db, version, database.MigrationHistoryTypeBackfill)
}

// RecordBackfillHistory registra backfill en tenant_migration_history.
func RecordBackfillHistory(db *gorm.DB, version int, name string, dur time.Duration, success bool, errMsg string) error {
	return recordHistory(db, version, name, database.MigrationHistoryTypeBackfill, dur, success, errMsg)
}
