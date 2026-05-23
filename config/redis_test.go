package config

import (
	"os"
	"testing"
)

func TestResolveRedisSettingsFromURL(t *testing.T) {
	t.Setenv("REDIS_URL", "redis://tukifac-redis:6379/0")
	t.Setenv("REDIS_ADDR", "")
	t.Setenv("REDIS_HOST", "")
	t.Setenv("REDIS_DISABLED", "")

	rs := ResolveRedisSettings("production")
	if !rs.Enabled {
		t.Fatal("expected enabled")
	}
	if rs.Addr != "tukifac-redis:6379" {
		t.Fatalf("addr=%q", rs.Addr)
	}
	if rs.DB != 0 {
		t.Fatalf("db=%d", rs.DB)
	}
}

func TestResolveRedisSettingsFromHostPort(t *testing.T) {
	os.Unsetenv("REDIS_URL")
	t.Setenv("REDIS_HOST", "tukifac-redis")
	t.Setenv("REDIS_PORT", "6379")
	t.Setenv("REDIS_DB", "2")
	t.Setenv("REDIS_PASSWORD", "secret")

	rs := ResolveRedisSettings("production")
	if rs.Addr != "tukifac-redis:6379" {
		t.Fatalf("addr=%q", rs.Addr)
	}
	if rs.DB != 2 {
		t.Fatalf("db=%d", rs.DB)
	}
	if rs.Password != "secret" {
		t.Fatal("expected password from env")
	}
}

func TestResolveRedisSettingsFromAddr(t *testing.T) {
	os.Unsetenv("REDIS_URL")
	os.Unsetenv("REDIS_HOST")
	t.Setenv("REDIS_ADDR", "tukifac-redis:6379")

	rs := ResolveRedisSettings("production")
	if rs.Addr != "tukifac-redis:6379" {
		t.Fatalf("addr=%q", rs.Addr)
	}
}

func TestResolveRedisDisabled(t *testing.T) {
	t.Setenv("REDIS_DISABLED", "true")
	t.Setenv("REDIS_URL", "redis://tukifac-redis:6379/0")

	rs := ResolveRedisSettings("production")
	if rs.Enabled {
		t.Fatal("expected disabled")
	}
}

func TestResolveRedisURLNone(t *testing.T) {
	t.Setenv("REDIS_DISABLED", "")
	t.Setenv("REDIS_URL", "none")

	rs := ResolveRedisSettings("production")
	if rs.Enabled {
		t.Fatal("expected disabled when REDIS_URL=none")
	}
}
