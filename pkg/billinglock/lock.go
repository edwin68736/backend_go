// Package billinglock lock distribuido Redis por tenant+venta (anti-duplicidad fiscal).
package billinglock

import (
	"context"
	"fmt"
	"time"

	"tukifac/pkg/tenantcache"
)

const (
	// DefaultTTL protege enqueue, manual, worker y reconcile (30–60s).
	DefaultTTL = 45 * time.Second
	keyPrefix  = "tukifac:billing:lock:"
)

// Key clave multi-tenant: tukifac:billing:lock:{tenantId}:{saleId}
func Key(tenantID, saleID uint) string {
	return fmt.Sprintf("%st%d:%d", keyPrefix, tenantID, saleID)
}

// TryAcquire intenta adquirir el lock. owner identifica la fuente (manual|queue|auto_create|…).
func TryAcquire(tenantID, saleID uint, owner string) (bool, error) {
	rdb := tenantcache.RDB()
	if rdb == nil || tenantID == 0 || saleID == 0 {
		return true, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	ok, err := rdb.SetNX(ctx, Key(tenantID, saleID), owner, DefaultTTL).Result()
	return ok, err
}

// Release libera solo si el owner coincide (evita borrar lock de otro proceso).
func Release(tenantID, saleID uint, owner string) {
	rdb := tenantcache.RDB()
	if rdb == nil || tenantID == 0 || saleID == 0 || owner == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	script := `
if redis.call("GET", KEYS[1]) == ARGV[1] then
  return redis.call("DEL", KEYS[1])
end
return 0`
	_, _ = rdb.Eval(ctx, script, []string{Key(tenantID, saleID)}, owner).Result()
}

// Extend renueva TTL mientras el worker procesa emisión larga.
func Extend(tenantID, saleID uint, owner string, ttl time.Duration) {
	rdb := tenantcache.RDB()
	if rdb == nil || tenantID == 0 || saleID == 0 || owner == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	script := `
if redis.call("GET", KEYS[1]) == ARGV[1] then
  return redis.call("PEXPIRE", KEYS[1], ARGV[2])
end
return 0`
	ms := ttl.Milliseconds()
	if ms < 1000 {
		ms = DefaultTTL.Milliseconds()
	}
	_, _ = rdb.Eval(ctx, script, []string{Key(tenantID, saleID)}, owner, ms).Result()
}
