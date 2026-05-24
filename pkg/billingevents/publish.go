package billingevents

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"tukifac/pkg/logger"
)

func tenantChannel(tenantID uint) string {
	return fmt.Sprintf("tukifac:tenant:%d:billing_updates", tenantID)
}

// PublishStatusUpdated publica en Redis Pub/Sub (fire-and-forget; no bloquea webhook).
func PublishStatusUpdated(ctx context.Context, evt StatusUpdatedPayload) {
	if globalHub == nil || globalHub.rdb == nil {
		return
	}
	if evt.Event == "" {
		evt.Event = EventStatusUpdated
	}
	data, err := json.Marshal(evt)
	if err != nil {
		return
	}
	if err := globalHub.rdb.Publish(ctx, tenantChannel(evt.TenantID), data).Err(); err != nil {
		logger.L.Warn("billingevents_publish_failed",
			slog.Uint64("tenant_id", uint64(evt.TenantID)),
			slog.Uint64("sale_id", uint64(evt.SaleID)),
			slog.Any("error", err),
		)
	}
}

// PublishRaw reenvía bytes ya serializados (uso interno).
func PublishRaw(ctx context.Context, tenantID uint, payload []byte) {
	if globalHub == nil || globalHub.rdb == nil || len(payload) == 0 {
		return
	}
	_ = globalHub.rdb.Publish(ctx, tenantChannel(tenantID), payload).Err()
}