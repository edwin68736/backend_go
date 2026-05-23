package docusage

import (
	"encoding/json"

	"tukifac/pkg/database"
	"tukifac/pkg/tenantcache"

	"gorm.io/gorm"
)

func metaJSON(v interface{}) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func logEvent(tenantID uint, subID *uint, eventType, actorType string, actorID *uint, reason, meta string) {
	_ = database.CentralDB.Create(&database.SaasSubscriptionEvent{
		TenantID: tenantID, SubscriptionID: subID, EventType: eventType,
		ActorType: actorType, ActorID: actorID, Reason: reason, MetadataJSON: meta,
	}).Error
}

func logEventTx(tx *gorm.DB, tenantID uint, subID *uint, eventType, actorType string, actorID *uint, reason, meta string) {
	_ = tx.Create(&database.SaasSubscriptionEvent{
		TenantID: tenantID, SubscriptionID: subID, EventType: eventType,
		ActorType: actorType, ActorID: actorID, Reason: reason, MetadataJSON: meta,
	}).Error
}

func invalidateTenantCache(tenantID uint) {
	var t database.Tenant
	if database.CentralDB.First(&t, tenantID).Error == nil {
		tenantcache.Invalidate(t.Slug)
	}
}
