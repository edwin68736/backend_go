package fiscaldedup

import (
	"context"
	"sync"
	"time"

	"tukifac/pkg/tenantcache"

	"github.com/redis/go-redis/v9"
)

const keyPrefix = "tukifac:fiscal:webhook:dedup:"

var (
	fallback sync.Map
	defaultTTL = 72 * time.Hour
)

// TryMarkProcessed devuelve true si el evento es nuevo (debe procesarse).
func TryMarkProcessed(eventID string) bool {
	if eventID == "" {
		return true
	}
	rdb := tenantcache.RDB()
	if rdb == nil {
		_, loaded := fallback.LoadOrStore(eventID, true)
		return !loaded
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ok, err := rdb.SetNX(ctx, keyPrefix+eventID, "1", defaultTTL).Result()
	if err != nil {
		_, loaded := fallback.LoadOrStore(eventID, true)
		return !loaded
	}
	return ok
}

// Release elimina la marca (p.ej. si ApplyStatus falló).
func Release(eventID string) {
	if eventID == "" {
		return
	}
	rdb := tenantcache.RDB()
	if rdb == nil {
		fallback.Delete(eventID)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = rdb.Del(ctx, keyPrefix+eventID).Err()
}

// SetClientForTest inyecta Redis en tests.
func SetClientForTest(rdb *redis.Client) {
	// no-op: usa tenantcache global en prod; tests usan miniredis vía InitRedis
	_ = rdb
}
