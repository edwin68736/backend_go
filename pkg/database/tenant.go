package database

import (
	"fmt"
	"regexp"
	"sync"

	"tukifac/config"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var tenantDBNamePattern = regexp.MustCompile(`^saas_tenant_[a-z0-9_]+$`)

var tenantPool sync.Map // map[string]*gorm.DB

// GetTenantDB retorna (o crea) la conexión GORM del tenant desde el pool.
// No ejecuta migraciones: el esquema debe estar al día vía `./tukifac-api migrate`.
func GetTenantDB(dbName string) (*gorm.DB, error) {
	if err := validateTenantDBName(dbName); err != nil {
		return nil, err
	}

	if cached, ok := tenantPool.Load(dbName); ok {
		return cached.(*gorm.DB), nil
	}

	db, err := openTenantDB(dbName)
	if err != nil {
		return nil, err
	}
	tenantPool.Store(dbName, db)
	return db, nil
}

func openTenantDB(dbName string) (*gorm.DB, error) {
	cfg := config.AppConfig
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=True&loc=Local",
		cfg.DBUser, cfg.DBPassword, cfg.DBHost, cfg.DBPort, dbName,
	)
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
		Logger:      logger.Default.LogMode(logger.Warn),
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

// RemoveTenantFromPool elimina una conexión del pool y cierra sus conexiones MySQL.
func RemoveTenantFromPool(dbName string) {
	if cached, ok := tenantPool.LoadAndDelete(dbName); ok {
		if db, ok := cached.(*gorm.DB); ok {
			closeDB(db)
		}
	}
}
