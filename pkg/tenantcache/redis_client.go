package tenantcache

import (
	"context"
	"log/slog"
	"time"

	"tukifac/config"
	"tukifac/pkg/logger"
	"tukifac/pkg/metrics"

	"github.com/redis/go-redis/v9"
)

var globalRDB *redis.Client

// InitRedis conecta a Redis si REDIS_URL está configurado. Si falla o está vacío, opera sin Redis.
func InitRedis(cfg *config.Config) *redis.Client {
	if cfg.RedisURL == "" {
		logger.L.Info("redis_disabled", slog.String("reason", "REDIS_URL empty"))
		return nil
	}
	opt, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		logger.L.Error("redis_parse_failed", slog.Any("error", err))
		return nil
	}
	opt.PoolSize = cfg.RedisPoolSize
	opt.MinIdleConns = 2
	opt.ReadTimeout = 3 * time.Second
	opt.WriteTimeout = 3 * time.Second

	rdb := redis.NewClient(opt)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		logger.L.Error("redis_ping_failed", slog.Any("error", err))
		_ = rdb.Close()
		return nil
	}
	globalRDB = rdb
	logger.L.Info("redis_connected", slog.String("addr", opt.Addr))
	return rdb
}

// RDB retorna el cliente global (puede ser nil).
func RDB() *redis.Client {
	return globalRDB
}

// Close cierra Redis en shutdown.
func Close() error {
	if globalRDB == nil {
		return nil
	}
	return globalRDB.Close()
}

func redisOK() {
	metrics.RedisOpsOK.Add(1)
}

func redisFail() {
	metrics.RedisOpsFail.Add(1)
}
