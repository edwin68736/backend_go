package database

import (
	"fmt"
	"time"

	"tukifac/config"

	"gorm.io/gorm"
)

// MigrateSummary resultado de migrar múltiples tenants.
type MigrateSummary struct {
	Success []string
	Failed  []TenantMigrateFailure
}

type TenantMigrateFailure struct {
	Slug   string
	DBName string
	Err    error
}

// RunCentralMigration crea (si falta) y migra la BD central + seeds idempotentes.
func RunCentralMigration() error {
	if err := EnsureCentralDB(); err != nil {
		return fmt.Errorf("ensure central database: %w", err)
	}
	if CentralDB == nil {
		if err := ConnectCentral(); err != nil {
			return err
		}
	}
	if err := MigrateCentral(); err != nil {
		return fmt.Errorf("migrate central: %w", err)
	}
	if err := MigrateModuleKeySaasToMemberships(); err != nil {
		return fmt.Errorf("migrate module key saas→memberships: %w", err)
	}
	if err := SeedCentral(); err != nil {
		return fmt.Errorf("seed central: %w", err)
	}
	return nil
}

// MigrateTenantSchema aplica AutoMigrate + seeds de catálogo del tenant (sin pool persistente).
// Usar en CLI, alta de tenant y endpoints admin de migración.
func MigrateTenantSchema(dbName string) error {
	db, err := openTenantDB(dbName)
	if err != nil {
		return err
	}
	defer closeDB(db)

	if err := applyTenantSchema(db, dbName); err != nil {
		return err
	}
	// Si ya estaba en pool, invalidar para que la app reabra con esquema actualizado
	RemoveTenantFromPool(dbName)
	return nil
}

func applyTenantSchema(db *gorm.DB, dbName string) error {
	if err := MigrateTenant(db); err != nil {
		return fmt.Errorf("auto migrate: %w", err)
	}
	mig := db.Migrator()
	if !mig.HasTable(&TenantRole{}) {
		return fmt.Errorf("no se creó tenant_roles (permisos CREATE en MySQL?)")
	}
	if err := SeedUbigeoRegionesProvincias(db); err != nil {
		return fmt.Errorf("seed ubigeo regiones/provincias: %w", err)
	}
	if err := SeedUbigeoDistritos(db, UbigeoDistritosCSVPath()); err != nil {
		return fmt.Errorf("seed ubigeo distritos: %w", err)
	}
	if err := SeedPaymentMethodsIfEmpty(db); err != nil {
		return fmt.Errorf("seed payment methods: %w", err)
	}
	return nil
}

// MigrateTenantBySlug migra un tenant por slug (cualquier status).
func MigrateTenantBySlug(slug string) error {
	if CentralDB == nil {
		return fmt.Errorf("BD central no conectada")
	}
	var tenant Tenant
	if err := CentralDB.Where("slug = ?", slug).First(&tenant).Error; err != nil {
		return fmt.Errorf("tenant no encontrado: %w", err)
	}
	return MigrateTenantSchema(tenant.DBName)
}

// ListTenantsForMigration devuelve tenants a migrar en lote.
func ListTenantsForMigration(activeOnly bool) ([]Tenant, error) {
	if CentralDB == nil {
		return nil, fmt.Errorf("BD central no conectada")
	}
	var tenants []Tenant
	q := CentralDB.Order("slug ASC")
	if activeOnly {
		q = q.Where("status = ?", "active")
	}
	if err := q.Find(&tenants).Error; err != nil {
		return nil, err
	}
	return tenants, nil
}

// MigrateProgress callback opcional por tenant (slug, err).
type MigrateProgress func(slug string, err error)

// MigrateTenantsBatch migra tenants en lotes con pausa configurable (no detiene en error).
func MigrateTenantsBatch(activeOnly bool, onProgress MigrateProgress) MigrateSummary {
	cfg := config.AppConfig
	batchSize := cfg.MigrationBatchSize
	if batchSize <= 0 {
		batchSize = 50
	}
	pause := cfg.MigrationBatchPause

	tenants, err := ListTenantsForMigration(activeOnly)
	summary := MigrateSummary{}
	if err != nil {
		summary.Failed = append(summary.Failed, TenantMigrateFailure{
			Slug: "(list)",
			Err:  err,
		})
		if onProgress != nil {
			onProgress("(list)", err)
		}
		return summary
	}

	for i, t := range tenants {
		migErr := MigrateTenantSchema(t.DBName)
		if onProgress != nil {
			onProgress(t.Slug, migErr)
		}
		if migErr != nil {
			summary.Failed = append(summary.Failed, TenantMigrateFailure{
				Slug:   t.Slug,
				DBName: t.DBName,
				Err:    migErr,
			})
		} else {
			summary.Success = append(summary.Success, t.Slug)
		}

		if pause > 0 && batchSize > 0 && (i+1)%batchSize == 0 && i+1 < len(tenants) {
			time.Sleep(pause)
		}
	}
	return summary
}

func closeDB(db *gorm.DB) {
	if db == nil {
		return
	}
	if sqlDB, err := db.DB(); err == nil {
		_ = sqlDB.Close()
	}
}
