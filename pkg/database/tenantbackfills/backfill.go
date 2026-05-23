package tenantbackfills

import "gorm.io/gorm"

// TenantBackfill migración de datos run-once (no DDL masivo en Up).
type TenantBackfill interface {
	Version() int
	Name() string
	Run(db *gorm.DB) error
}
