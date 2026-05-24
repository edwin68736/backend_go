package database

import "tukifac/pkg/database/tenantmigrations"

// Versiones de esquema tenant (Migration System v2).
// V30 = baseline congelado: estado del sistema antes del registry incremental.
const TenantSchemaBaselineVersion = 30

// TenantSchemaTargetVersion versión objetivo del binario (registry incremental).
func TenantSchemaTargetVersion() int {
	return tenantmigrations.MaxVersion()
}
