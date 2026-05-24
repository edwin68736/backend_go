package tenantcache

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"tukifac/pkg/logger"
)

// Invalidate elimina cache de metadata del tenant (slug) y namespaces relacionados en Redis.
func Invalidate(slug string) {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return
	}
	if Default != nil {
		Default.invalidateSlug(slug)
	}
	flushRedisNamespace(slug)
}

func (s *Service) invalidateSlug(slug string) {
	if s == nil || s.rdb == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = s.rdb.Del(ctx, keyPrefix+slug, negativePrefix+slug).Err()
	redisOK()
}

// flushRedisNamespace borra claves tukifac:tenant:{slug}:* (permisos, etc.).
func flushRedisNamespace(slug string) {
	if slug == "" {
		return
	}
	rdb := RDB()
	if rdb == nil {
		return
	}
	pattern := fmt.Sprintf("tukifac:tenant:%s:*", slug)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var cursor uint64
	var removed int
	for {
		keys, next, err := rdb.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			logger.L.Warn("tenant_cache_flush_scan_failed",
				slog.String("slug", slug),
				slog.String("pattern", pattern),
				slog.Any("error", err),
			)
			return
		}
		if len(keys) > 0 {
			if err := rdb.Del(ctx, keys...).Err(); err != nil {
				redisFail()
			} else {
				removed += len(keys)
				redisOK()
			}
		}
		cursor = next
		if cursor == 0 {
			break
		}
	}
	if removed > 0 {
		logger.L.Info("tenant_cache_namespace_flushed",
			slog.String("slug", slug),
			slog.Int("keys_removed", removed),
		)
	}
}
