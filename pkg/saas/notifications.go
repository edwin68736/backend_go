package saas

import (
	"encoding/json"
	"log/slog"
	"time"

	"tukifac/pkg/database"
	"tukifac/pkg/logger"
)

// QueueNotification encola notificación (procesada por worker/cron).
func QueueNotification(tenantID uint, subscriptionID uint, channel, templateKey string, payload map[string]interface{}) {
	if database.CentralDB == nil {
		return
	}
	var subID *uint
	if subscriptionID > 0 {
		subID = &subscriptionID
	}
	b, _ := json.Marshal(payload)
	row := database.SaasNotificationLog{
		TenantID:       tenantID,
		SubscriptionID: subID,
		Channel:        channel,
		TemplateKey:    templateKey,
		PayloadJSON:    string(b),
		Status:         "queued",
		ScheduledAt:    time.Now(),
	}
	if err := database.CentralDB.Create(&row).Error; err != nil {
		logger.L.Warn("saas_notification_enqueue_failed", slog.Any("error", err))
	}
}

// ProcessNotificationQueue marca como enviadas (stub: integrar SMTP/WhatsApp después).
func ProcessNotificationQueue(limit int) int {
	if limit <= 0 {
		limit = 50
	}
	var rows []database.SaasNotificationLog
	database.CentralDB.Where("status = ?", "queued").
		Order("scheduled_at asc").Limit(limit).Find(&rows)
	now := time.Now()
	for _, r := range rows {
		// TODO: integrar proveedores reales (email/WhatsApp).
		database.CentralDB.Model(&r).Updates(map[string]interface{}{
			"status":  "sent",
			"sent_at": now,
		})
	}
	return len(rows)
}
