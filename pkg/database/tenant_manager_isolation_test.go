package database

import (
	"sync"
	"testing"
	"time"

	"tukifac/config"
	"tukifac/pkg/logger"
)

// Verifica refcount del pool sin abrir MySQL (solo mapa in-memory del manager).
func TestTenantDBManager_releaseDoesNotGoNegative(t *testing.T) {
	logger.Init(&config.Config{LogLevel: "error", AppEnv: "development"})
	cfg := &config.Config{
		AppEnv:                  "development",
		TenantPoolIdleTTL:       time.Hour,
		TenantPoolMaxActive:     50,
		TenantPoolEvictInterval: time.Hour,
	}
	InitTenantDBManager(cfg)
	defer ShutdownTenantDBManager()

	dbName := "saas_tenant_mock_release"
	defaultManager.mu.Lock()
	defaultManager.pools[dbName] = &tenantPoolEntry{lastUsed: time.Now()}
	defaultManager.pools[dbName].inUse.Store(1)
	defaultManager.mu.Unlock()

	defaultManager.release(dbName)
	defaultManager.release(dbName) // segundo release no debe dejar inUse negativo

	defaultManager.mu.Lock()
	inUse := defaultManager.pools[dbName].inUse.Load()
	defaultManager.mu.Unlock()
	if inUse < 0 {
		t.Fatalf("inUse negative: %d", inUse)
	}
}

func TestTenantDBManager_mapKeysArePerDBName(t *testing.T) {
	logger.Init(&config.Config{LogLevel: "error", AppEnv: "development"})
	cfg := &config.Config{
		AppEnv:                  "development",
		TenantPoolIdleTTL:       time.Hour,
		TenantPoolMaxActive:     50,
		TenantPoolEvictInterval: time.Hour,
	}
	InitTenantDBManager(cfg)
	defer ShutdownTenantDBManager()

	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			name := "saas_tenant_key_" + string(rune('a'+i%26))
			defaultManager.mu.Lock()
			if _, ok := defaultManager.pools[name]; !ok {
				defaultManager.pools[name] = &tenantPoolEntry{lastUsed: time.Now()}
			}
			defaultManager.mu.Unlock()
		}(i)
	}
	wg.Wait()

	defaultManager.mu.Lock()
	count := len(defaultManager.pools)
	defaultManager.mu.Unlock()
	if count == 0 {
		t.Fatal("expected pool entries")
	}
}
