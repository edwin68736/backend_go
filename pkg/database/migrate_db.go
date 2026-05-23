package database

import "gorm.io/gorm"

// OpenTenantDBForMigration abre conexión tenant sin pool (CLI / workers).
func OpenTenantDBForMigration(dbName string) (*gorm.DB, error) {
	return openTenantDB(dbName)
}

// CloseTenantDB cierra conexión abierta para migración.
func CloseTenantDB(db *gorm.DB) {
	closeDB(db)
}
