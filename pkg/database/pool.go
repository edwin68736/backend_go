package database

import (
	"database/sql"

	"tukifac/config"
)

// configureCentralPool — pool único para tukifac_saas.
// Default 25 open: suficiente para login, superadmin y cron sin competir con miles de pools tenant.
func configureCentralPool(db *sql.DB) {
	cfg := config.AppConfig
	applyPoolLimits(db, cfg.DBCentralMaxOpen, cfg.DBCentralMaxIdle)
}

// configureTenantPool — por cada BD tenant activa en el manager.
// Default 2 open × max 200 pools ≈ 400 conexiones pico (ajustar vs max_connections MySQL en VPS2).
// Con 10k tenants solo un subconjunto tiene pool abierto gracias a eviction.
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
