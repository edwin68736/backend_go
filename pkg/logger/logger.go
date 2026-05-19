package logger

import (
	"log/slog"
	"os"
	"strings"

	"tukifac/config"
)

// L es el logger estructurado de la aplicación (JSON en producción).
var L *slog.Logger

// Init configura slog según APP_ENV y LOG_LEVEL.
func Init(cfg *config.Config) {
	level := parseLevel(cfg.LogLevel)
	opts := &slog.HandlerOptions{
		Level: level,
	}

	var handler slog.Handler
	if cfg.IsProd() {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		handler = slog.NewTextHandler(os.Stdout, opts)
	}
	L = slog.New(handler)
	slog.SetDefault(L)
}

func parseLevel(s string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
