package database

import (
	"fmt"
	"regexp"

	"tukifac/config"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

var tenantDBNamePattern = regexp.MustCompile(`^saas_tenant_[a-z0-9_]+$`)

// GetTenantDB retorna el pool GORM del tenant vía TenantDBManager (singleflight + eviction).
// Debe parearse con ReleaseTenantDB al final del request (middleware TenantDBRelease).
func GetTenantDB(dbName string) (*gorm.DB, error) {
	if defaultManager == nil {
		return nil, ErrNoTenantDBManager
	}
	return defaultManager.acquire(dbName)
}

func openTenantDB(dbName string) (*gorm.DB, error) {
	cfg := config.AppConfig
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=True&loc=Local",
		cfg.DBUser, cfg.DBPassword, cfg.DBHost, cfg.DBPort, dbName,
	)
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
		Logger:      gormLogger(),
		PrepareStmt: true,
	})
	if err != nil {
		return nil, fmt.Errorf("conectando BD tenant %s: %w", dbName, err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	configureTenantPool(sqlDB)

	return db, nil
}

// CreateTenantDB crea la base de datos MySQL para un nuevo tenant.
func CreateTenantDB(dbName string) error {
	if err := validateTenantDBName(dbName); err != nil {
		return err
	}
	if CentralDB == nil {
		return fmt.Errorf("BD central no inicializada")
	}
	sql := fmt.Sprintf("CREATE DATABASE IF NOT EXISTS `%s` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci", dbName)
	return CentralDB.Exec(sql).Error
}

// DropTenantDB elimina la base de datos del tenant (rollback al fallar el alta).
func DropTenantDB(dbName string) error {
	if err := validateTenantDBName(dbName); err != nil {
		return err
	}
	if CentralDB == nil {
		return fmt.Errorf("BD central no inicializada")
	}
	RemoveTenantFromPool(dbName)
	sql := fmt.Sprintf("DROP DATABASE IF EXISTS `%s`", dbName)
	return CentralDB.Exec(sql).Error
}

func validateTenantDBName(dbName string) error {
	if dbName == "" || !tenantDBNamePattern.MatchString(dbName) {
		return fmt.Errorf("nombre de base de datos de tenant inválido: %q", dbName)
	}
	return nil
}

// RemoveTenantFromPool cierra y elimina el pool del tenant.
func RemoveTenantFromPool(dbName string) {
	if defaultManager != nil {
		defaultManager.Remove(dbName)
	}
}
