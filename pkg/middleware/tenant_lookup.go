package middleware

import (
	"tukifac/pkg/database"
	"tukifac/pkg/restaurantperm"
	"tukifac/pkg/tenantcache"
)

func releaseTenantDB(dbName string) {
	database.ReleaseTenantDB(dbName)
}

// LookupTenantBySlug resuelve metadata del tenant (Redis distribuido + fallback central DB).
func LookupTenantBySlug(slug string) (*database.Tenant, error) {
	return tenantcache.LookupTenantBySlug(slug)
}

// InvalidateTenantCache invalida cache distribuida (Redis) tras cambios en panel central.
func InvalidateTenantCache(slug string) {
	tenantcache.Invalidate(slug)
	restaurantperm.InvalidateTenantMem(slug)
}
