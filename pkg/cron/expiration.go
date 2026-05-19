package cron

import (
	"log/slog"
	"time"

	"tukifac/pkg/database"
	"tukifac/pkg/logger"
)

// StartExpirationChecker inicia el worker en background que verifica vencimientos.
// Corre inmediatamente al arrancar y luego cada 24 horas.
func StartExpirationChecker() {
	go func() {
		checkExpirations()
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			checkExpirations()
		}
	}()
	logger.L.Info("cron_started", slog.String("job", "expiration_checker"), slog.String("interval", "24h"))
}

// checkExpirations marca suscripciones vencidas como "expired" (solo informativo).
// IMPORTANTE: NO modifica Tenant.Status. La suspensión del tenant es SIEMPRE manual
// desde el panel central. El admin verá las suscripciones vencidas y decidirá.
func checkExpirations() {
	now := time.Now()
	var expired []database.SaasSubscription
	database.CentralDB.
		Where("status = 'active' AND end_date < ?", now).
		Find(&expired)

	if len(expired) == 0 {
		return
	}

	for _, sub := range expired {
		database.CentralDB.Model(&sub).Update("status", "expired")
		logger.L.Info("subscription_expired",
			slog.Uint64("subscription_id", uint64(sub.ID)),
			slog.Uint64("tenant_id", uint64(sub.TenantID)),
		)
	}
	logger.L.Info("cron_expiration_run_complete",
		slog.Int("expired_count", len(expired)),
		slog.String("note", "tenant_status_not_modified"),
	)
}
