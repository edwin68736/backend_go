package cronlock

import (
	"context"
	"fmt"
	"os"
	"time"

	"tukifac/pkg/tenantcache"
)

const keyPrefix = "tukifac:cronlock:"

// TryAcquire adquiere lock distribuido (Redis SETNX). Si Redis no está, permite ejecución (dev single-node).
func TryAcquire(jobKey string, ttl time.Duration) (release func(), acquired bool) {
	noop := func() {}
	rdb := tenantcache.RDB()
	if rdb == nil {
		return noop, true
	}
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	key := keyPrefix + jobKey
	owner := fmt.Sprintf("%s:%d", hostname(), os.Getpid())
	ctx := context.Background()
	ok, err := rdb.SetNX(ctx, key, owner, ttl).Result()
	if err != nil || !ok {
		return noop, false
	}
	return func() {
		_ = rdb.Del(context.Background(), key).Err()
	}, true
}

// TryAcquireDaily lock por job + fecha Lima (evita doble run el mismo día).
func TryAcquireDaily(jobKey string, limaDate string, ttl time.Duration) (func(), bool) {
	return TryAcquire(jobKey+":"+limaDate, ttl)
}

func hostname() string {
	h, err := os.Hostname()
	if err != nil || h == "" {
		return "unknown"
	}
	return h
}
