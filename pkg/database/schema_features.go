package database

import (
	"errors"

	"gorm.io/gorm"
)

// SchemaAtLeast indica si el tenant tiene aplicada la migración de esquema >= minVersion.
// Compatible con fleet heterogéneo: history + fallback HasColumn.
func SchemaAtLeast(db *gorm.DB, minVersion int) bool {
	if db == nil {
		return false
	}
	if minVersion <= TenantSchemaBaselineVersion {
		return true
	}
	if db.Migrator().HasTable(&TenantMigrationHistory{}) {
		var n int64
		_ = db.Model(&TenantMigrationHistory{}).
			Where("version >= ? AND type = ? AND success = ?", minVersion, MigrationHistoryTypeSchema, true).
			Count(&n).Error
		if n > 0 {
			return true
		}
	}
	return schemaAtLeastFallback(db, minVersion)
}

func schemaAtLeastFallback(db *gorm.DB, minVersion int) bool {
	switch minVersion {
	case 31:
		return TenantBranchMultiSchemaReady(db)
	case 32:
		return db.Migrator().HasColumn("tenant_table_sessions", "order_type")
	case 35:
		return db.Migrator().HasTable("tenant_restaurant_staff")
	default:
		return false
	}
}

// TenantFeatureFlags capacidades derivadas del esquema (para API / frontends).
func TenantFeatureFlags(db *gorm.DB) map[string]bool {
	flags := map[string]bool{
		"multi_branch":        SchemaAtLeast(db, 31),
		"restaurant_orders":   SchemaAtLeast(db, 32),
		"restaurant_staff_v2": SchemaAtLeast(db, 35),
		"restaurant_recipes":  false,
		"advanced_inventory":  false,
	}
	if SchemaAtLeast(db, 35) {
		var cfg TenantRestaurantSetting
		if db.First(&cfg).Error == nil {
			flags["restaurant_staff_v2_enabled"] = cfg.StaffV2Enabled
			flags["restaurant_recipes"] = cfg.RecipesEnabled
		}
	}
	return flags
}

// RecipesEnabled: el superadmin activó insumos y recetas para esta empresa. Una BD sin V195 responde false.
func RecipesEnabled(db *gorm.DB) bool {
	var cfg TenantRestaurantSetting
	return db.Order("id ASC").Limit(1).Find(&cfg).Error == nil && cfg.RecipesEnabled
}

// SetRecipesEnabled enciende o apaga el módulo. Escribe por nombre de tabla: el campo es de solo lectura para GORM.
func SetRecipesEnabled(db *gorm.DB, enabled bool) error {
	if !db.Migrator().HasColumn(&TenantRestaurantSetting{}, "recipes_enabled") {
		return errors.New("la base de datos de la empresa aún no tiene la migración de insumos (V195)")
	}
	var cfg TenantRestaurantSetting
	if err := db.Order("id ASC").Limit(1).Find(&cfg).Error; err != nil {
		return err
	}
	if cfg.ID == 0 {
		if err := db.Create(&cfg).Error; err != nil {
			return err
		}
	}
	return db.Table("tenant_restaurant_settings").Where("id = ?", cfg.ID).Update("recipes_enabled", enabled).Error
}

// CurrentSchemaVersion estima versión actual del tenant (history > fallback > baseline).
func CurrentSchemaVersion(db *gorm.DB) int {
	if db == nil {
		return TenantSchemaBaselineVersion
	}
	if db.Migrator().HasTable(&TenantMigrationHistory{}) {
		var maxVer *int
		_ = db.Model(&TenantMigrationHistory{}).
			Where("type = ? AND success = ?", MigrationHistoryTypeSchema, true).
			Select("COALESCE(MAX(version), 0)").Scan(&maxVer).Error
		if maxVer != nil && *maxVer > 0 {
			return *maxVer
		}
	}
	if db.Migrator().HasColumn("tenant_table_sessions", "order_type") {
		return 32
	}
	if TenantBranchMultiSchemaReady(db) {
		return 31
	}
	return TenantSchemaBaselineVersion
}
