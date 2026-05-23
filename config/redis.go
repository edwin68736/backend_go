package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
)

// RedisSettings configuración resuelta para go-redis (Docker + local).
type RedisSettings struct {
	Enabled      bool
	Addr         string // host:port para logs y diagnóstico
	URL          string // URL canónica para redis.ParseURL (puede incluir password)
	Password     string
	DB           int
	PoolSize     int
	MinIdleConns int
	MaxRetries   int
}

// ResolveRedisSettings construye la conexión Redis desde variables de entorno.
//
// Prioridad:
//  1. REDIS_DISABLED=1 | REDIS_URL=none|off|disabled → sin Redis
//  2. REDIS_URL (redis://…)
//  3. REDIS_ADDR (host:port)
//  4. REDIS_HOST + REDIS_PORT
//  5. desarrollo: 127.0.0.1:6379
func ResolveRedisSettings(appEnv string) RedisSettings {
	poolSize := envIntDefault("REDIS_POOL_SIZE", 32)
	minIdle := envIntDefault("REDIS_MIN_IDLE_CONNS", 10)
	maxRetries := envIntDefault("REDIS_MAX_RETRIES", 5)
	db := envIntDefault("REDIS_DB", 0)

	if redisExplicitlyDisabled() {
		return RedisSettings{
			Enabled:      false,
			PoolSize:     poolSize,
			MinIdleConns: minIdle,
			MaxRetries:   maxRetries,
			DB:           db,
		}
	}

	password := strings.TrimSpace(os.Getenv("REDIS_PASSWORD"))

	if rawURL := strings.TrimSpace(os.Getenv("REDIS_URL")); rawURL != "" {
		return finalizeRedisURL(rawURL, password, db, poolSize, minIdle, maxRetries)
	}

	addr := strings.TrimSpace(os.Getenv("REDIS_ADDR"))
	if addr == "" {
		host := strings.TrimSpace(os.Getenv("REDIS_HOST"))
		port := strings.TrimSpace(os.Getenv("REDIS_PORT"))
		if host != "" {
			if port == "" {
				port = "6379"
			}
			addr = net.JoinHostPort(host, port)
		}
	}
	if addr == "" {
		addr = defaultRedisAddr(appEnv)
	}

	redisURL := buildRedisURL(addr, password, db)
	return finalizeRedisURL(redisURL, password, db, poolSize, minIdle, maxRetries)
}

func redisExplicitlyDisabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("REDIS_DISABLED"))) {
	case "1", "true", "yes", "on":
		return true
	}
	switch strings.ToLower(strings.TrimSpace(os.Getenv("REDIS_URL"))) {
	case "", "none", "off", "disabled", "false":
		if os.Getenv("REDIS_URL") != "" {
			return true
		}
	}
	return false
}

func defaultRedisAddr(appEnv string) string {
	// Docker producción: definir REDIS_URL o REDIS_ADDR=tukifac-redis:6379 en .env
	if appEnv == "production" {
		return "127.0.0.1:6379"
	}
	return "127.0.0.1:6379"
}

func buildRedisURL(addr, password string, db int) string {
	u := &url.URL{Scheme: "redis", Host: addr, Path: fmt.Sprintf("/%d", db)}
	if password != "" {
		u.User = url.UserPassword("", password)
	}
	return u.String()
}

func finalizeRedisURL(redisURL, passwordOverride string, db, poolSize, minIdle, maxRetries int) RedisSettings {
	redisURL = strings.TrimSpace(redisURL)
	opt, err := parseRedisURLForSettings(redisURL)
	if err != nil {
		// InitRedis registrará error de parseo; mantener Enabled para no confundir con REDIS_DISABLED.
		return RedisSettings{
			Enabled:      true,
			URL:          redisURL,
			Addr:         redisURL,
			PoolSize:     poolSize,
			MinIdleConns: minIdle,
			MaxRetries:   maxRetries,
		}
	}

	if passwordOverride != "" {
		opt.Password = passwordOverride
	}
	if db >= 0 && os.Getenv("REDIS_DB") != "" {
		opt.DB = db
	}

	finalURL := redisURL
	if passwordOverride != "" || os.Getenv("REDIS_DB") != "" {
		finalURL = buildRedisURL(opt.Addr, opt.Password, opt.DB)
	}

	return RedisSettings{
		Enabled:      true,
		Addr:         opt.Addr,
		URL:          finalURL,
		Password:     opt.Password,
		DB:           opt.DB,
		PoolSize:     poolSize,
		MinIdleConns: minIdle,
		MaxRetries:   maxRetries,
	}
}

type redisURLOpts struct {
	Addr     string
	Password string
	DB       int
}

func parseRedisURLForSettings(redisURL string) (redisURLOpts, error) {
	u, err := url.Parse(redisURL)
	if err != nil {
		return redisURLOpts{}, err
	}
	if u.Scheme != "redis" && u.Scheme != "rediss" {
		return redisURLOpts{}, fmt.Errorf("unsupported redis scheme: %s", u.Scheme)
	}

	addr := u.Host
	if addr == "" {
		return redisURLOpts{}, fmt.Errorf("redis url missing host")
	}

	password, _ := u.User.Password()
	db := 0
	if path := strings.TrimPrefix(u.Path, "/"); path != "" {
		if n, err := strconv.Atoi(path); err == nil {
			db = n
		}
	}

	return redisURLOpts{Addr: addr, Password: password, DB: db}, nil
}

// RedisSafeAddr para logs (sin password).
func (r RedisSettings) RedisSafeAddr() string {
	if r.Addr != "" {
		return r.Addr
	}
	return "unknown"
}

func envIntDefault(key string, defaultVal int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return defaultVal
	}
	i, err := strconv.Atoi(v)
	if err != nil {
		return defaultVal
	}
	if i < 0 {
		return defaultVal
	}
	return i
}
