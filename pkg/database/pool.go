package database

import (
	"database/sql"

	"tukifac/config"
)

func configureCentralPool(db *sql.DB) {
	cfg := config.AppConfig
	applyPoolLimits(db, cfg.DBCentralMaxOpen, cfg.DBCentralMaxIdle)
}

func configureTenantPool(db *sql.DB) {
	cfg := config.AppConfig
	applyPoolLimits(db, cfg.DBTenantMaxOpen, cfg.DBTenantMaxIdle)
}

func applyPoolLimits(db *sql.DB, maxOpen, maxIdle int) {
	cfg := config.AppConfig
	db.SetMaxOpenConns(maxOpen)
	db.SetMaxIdleConns(maxIdle)
	db.SetConnMaxLifetime(cfg.DBConnMaxLifetime)
	db.SetConnMaxIdleTime(cfg.DBConnMaxIdleTime)
}
