package tenantmigrations

import "gorm.io/gorm"

// TenantMigration migración incremental de esquema tenant (solo DDL / cambios estructurales).
type TenantMigration interface {
	Version() int
	Name() string
	Up(db *gorm.DB) error
}
