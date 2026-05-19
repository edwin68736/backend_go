package middleware

import (
	"sync"
	"time"

	"tukifac/config"
	"tukifac/pkg/database"
)

type tenantCacheEntry struct {
	tenant    database.Tenant
	expiresAt time.Time
}

var tenantBySlug sync.Map // map[string]tenantCacheEntry

// LookupTenantBySlug consulta la BD central con caché TTL (reduce carga con cientos de tenants).
func LookupTenantBySlug(slug string) (*database.Tenant, error) {
	ttl := config.AppConfig.TenantMetadataTTL
	if ttl > 0 {
		if raw, ok := tenantBySlug.Load(slug); ok {
			entry := raw.(tenantCacheEntry)
			if time.Now().Before(entry.expiresAt) {
				t := entry.tenant
				return &t, nil
			}
			tenantBySlug.Delete(slug)
		}
	}

	var tenant database.Tenant
	if err := database.CentralDB.Where("slug = ?", slug).First(&tenant).Error; err != nil {
		return nil, err
	}

	if ttl > 0 {
		tenantBySlug.Store(slug, tenantCacheEntry{
			tenant:    tenant,
			expiresAt: time.Now().Add(ttl),
		})
	}
	return &tenant, nil
}

// InvalidateTenantCache invalida metadata en memoria (tras actualizar tenant en panel central).
func InvalidateTenantCache(slug string) {
	if slug != "" {
		tenantBySlug.Delete(slug)
	}
}
