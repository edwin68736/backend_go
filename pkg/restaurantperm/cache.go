package restaurantperm

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"tukifac/pkg/tenantcache"
)

const cacheTTL = 15 * time.Minute

type memEntry struct {
	keys    []string
	expires time.Time
}

var (
	memMu sync.RWMutex
	mem   = map[string]memEntry{}
)

func cacheKey(tenantID, userID uint, version uint) string {
	return fmt.Sprintf("tukifac:rp:%d:%d:v%d", tenantID, userID, version)
}

func memKey(tenantID, userID uint, version uint) string {
	return cacheKey(tenantID, userID, version)
}

// GetCached devuelve permisos cacheados (Redis primero, memoria local).
func GetCached(tenantID, userID, version uint) ([]string, bool) {
	key := cacheKey(tenantID, userID, version)
	if rdb := tenantcache.RDB(); rdb != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		raw, err := rdb.Get(ctx, key).Bytes()
		if err == nil && len(raw) > 0 {
			var keys []string
			if json.Unmarshal(raw, &keys) == nil {
				return keys, true
			}
		}
	}
	memMu.RLock()
	e, ok := mem[memKey(tenantID, userID, version)]
	memMu.RUnlock()
	if ok && time.Now().Before(e.expires) {
		return e.keys, true
	}
	return nil, false
}

// SetCached guarda permisos en Redis y memoria.
func SetCached(tenantID, userID, version uint, keys []string) {
	key := cacheKey(tenantID, userID, version)
	if rdb := tenantcache.RDB(); rdb != nil {
		raw, _ := json.Marshal(keys)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = rdb.Set(ctx, key, raw, cacheTTL).Err()
	}
	memMu.Lock()
	mem[memKey(tenantID, userID, version)] = memEntry{keys: keys, expires: time.Now().Add(cacheTTL)}
	memMu.Unlock()
}

// InvalidateUser borra entradas conocidas de versión (best-effort).
func InvalidateUser(tenantID, userID, version uint) {
	key := cacheKey(tenantID, userID, version)
	if rdb := tenantcache.RDB(); rdb != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = rdb.Del(ctx, key).Err()
	}
	memMu.Lock()
	delete(mem, memKey(tenantID, userID, version))
	memMu.Unlock()
}
