package database

import (
	"fmt"

	"tukifac/config"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// CentralDB es la conexión a la base de datos central (saas_central).
var CentralDB *gorm.DB

// ConnectCentral establece la conexión con la BD central.
func ConnectCentral() error {
	cfg := config.AppConfig
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=True&loc=Local",
		cfg.DBUser, cfg.DBPassword, cfg.DBHost, cfg.DBPort, cfg.CentralDBName,
	)

	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
		Logger:      logger.Default.LogMode(logger.Warn),
		PrepareStmt: true,
	})
	if err != nil {
		return fmt.Errorf("conectando BD central: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	configureCentralPool(sqlDB)

	CentralDB = db
	return nil
}

// EnsureCentralDB crea la base de datos central si no existe.
func EnsureCentralDB() error {
	cfg := config.AppConfig
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/?charset=utf8mb4&parseTime=True&loc=Local",
		cfg.DBUser, cfg.DBPassword, cfg.DBHost, cfg.DBPort,
	)
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		return fmt.Errorf("conectando para crear BD central: %w", err)
	}
	sqlDB, err := db.DB()
	if err == nil {
		defer sqlDB.Close()
	}
	sql := fmt.Sprintf("CREATE DATABASE IF NOT EXISTS `%s` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci", cfg.CentralDBName)
	return db.Exec(sql).Error
}

// PingCentral verifica conectividad con la BD central (health readiness).
func PingCentral() error {
	if CentralDB == nil {
		return fmt.Errorf("central db not initialized")
	}
	sqlDB, err := CentralDB.DB()
	if err != nil {
		return err
	}
	return sqlDB.Ping()
}
