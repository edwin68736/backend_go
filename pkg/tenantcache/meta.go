package tenantcache

import (
	"encoding/json"
	"time"
)

const (
	keyPrefix      = "tukifac:tenant:slug:"
	negativePrefix = "tukifac:tenant:miss:"
)

// Meta metadata de tenant cacheada (distribuida).
type Meta struct {
	ID       uint     `json:"id"`
	Slug     string   `json:"slug"`
	DBName   string   `json:"db_name"`
	Status   string   `json:"status"`
	RUC      string   `json:"ruc"`
	PlanID   uint     `json:"plan_id"`
	Modules  []string `json:"modules,omitempty"`
	CachedAt int64    `json:"cached_at"`
}

func (m *Meta) Marshal() ([]byte, error) {
	return json.Marshal(m)
}

func unmarshalMeta(b []byte) (*Meta, error) {
	var m Meta
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

func (m *Meta) freshEnough(maxStale time.Duration) bool {
	if maxStale <= 0 || m.CachedAt == 0 {
		return true
	}
	return time.Since(time.Unix(m.CachedAt, 0)) <= maxStale
}
