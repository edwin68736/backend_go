package tenantcache

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"tukifac/config"
	"tukifac/pkg/database"
	"tukifac/pkg/logger"
	"tukifac/pkg/metrics"

	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// Service resuelve tenants con Redis + fallback a MySQL central.
type Service struct {
	rdb        *redis.Client
	ttl        time.Duration
	negativeTTL time.Duration
	maxStale   time.Duration
}

var Default *Service

// Init crea el servicio global (Redis opcional + fallback memoria vía miss path).
func Init(cfg *config.Config, rdb *redis.Client) {
	Default = &Service{
		rdb:         rdb,
		ttl:         cfg.TenantMetadataTTL,
		negativeTTL: cfg.TenantCacheNegativeTTL,
		maxStale:    cfg.TenantCacheMaxStale,
	}
}

// LookupTenantBySlug resuelve tenant por slug (Redis → central DB).
func LookupTenantBySlug(slug string) (*database.Tenant, error) {
	if Default != nil {
		return Default.Lookup(slug)
	}
	return lookupCentralDB(slug)
}

// Invalidate elimina cache Redis y memoria legacy para el slug.
func Invalidate(slug string) {
	if slug == "" {
		return
	}
	if Default != nil {
		Default.invalidate(slug)
	}
}

func (s *Service) Lookup(slug string) (*database.Tenant, error) {
	if slug == "" {
		return nil, gorm.ErrRecordNotFound
	}

	ctx, cancel := context.WithTimeout(context.Background(), config.AppConfig.DBReadTimeout)
	defer cancel()

	if s.rdb != nil {
		if meta, err := s.getRedis(ctx, slug); err == nil && meta != nil {
			if meta.freshEnough(s.maxStale) {
				metrics.TenantCacheHits.Add(1)
				return metaToTenant(meta), nil
			}
			logger.L.Debug("tenant_cache_stale_refresh", slog.String("slug", slug))
		} else if errors.Is(err, errNegativeCache) {
			metrics.TenantCacheNegHits.Add(1)
			return nil, gorm.ErrRecordNotFound
		}
	}

	metrics.TenantCacheMisses.Add(1)
	meta, err := s.loadFromCentral(ctx, slug)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) && s.rdb != nil {
			_ = s.setNegative(ctx, slug)
		}
		return nil, err
	}

	if s.rdb != nil {
		if err := s.setRedis(ctx, slug, meta); err != nil {
			logger.L.Warn("tenant_cache_set_failed", slog.String("slug", slug), slog.Any("error", err))
		}
	}
	return metaToTenant(meta), nil
}

var errNegativeCache = errors.New("tenant negative cache")

func (s *Service) getRedis(ctx context.Context, slug string) (*Meta, error) {
	key := keyPrefix + slug
	b, err := s.rdb.Get(ctx, key).Bytes()
	if err == redis.Nil {
		n, _ := s.rdb.Exists(ctx, negativePrefix+slug).Result()
		if n > 0 {
			redisOK()
			return nil, errNegativeCache
		}
		return nil, gorm.ErrRecordNotFound
	}
	if err != nil {
		redisFail()
		return nil, err
	}
	redisOK()
	return unmarshalMeta(b)
}

func (s *Service) setRedis(ctx context.Context, slug string, m *Meta) error {
	m.CachedAt = time.Now().Unix()
	b, err := m.Marshal()
	if err != nil {
		return err
	}
	if err := s.rdb.Set(ctx, keyPrefix+slug, b, s.ttl).Err(); err != nil {
		redisFail()
		return err
	}
	_ = s.rdb.Del(ctx, negativePrefix+slug).Err()
	redisOK()
	return nil
}

func (s *Service) setNegative(ctx context.Context, slug string) error {
	err := s.rdb.Set(ctx, negativePrefix+slug, "1", s.negativeTTL).Err()
	if err != nil {
		redisFail()
	}
	return err
}

func (s *Service) invalidate(slug string) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if s.rdb != nil {
		_ = s.rdb.Del(ctx, keyPrefix+slug, negativePrefix+slug).Err()
		redisOK()
	}
}

func (s *Service) loadFromCentral(ctx context.Context, slug string) (*Meta, error) {
	if database.CentralDB == nil {
		return nil, errors.New("central db not initialized")
	}
	var tenant database.Tenant
	err := database.CentralDB.WithContext(ctx).Where("slug = ?", slug).First(&tenant).Error
	if err != nil {
		return nil, err
	}

	modules := make([]string, 0)
	var tms []database.TenantModule
	_ = database.CentralDB.WithContext(ctx).
		Where("tenant_id = ? AND enabled = ?", tenant.ID, true).
		Find(&tms).Error
	for _, tm := range tms {
		modules = append(modules, tm.ModuleKey)
	}

	var planID uint
	var sub database.SaasSubscription
	if err := database.CentralDB.WithContext(ctx).
		Where("tenant_id = ? AND status IN ('active','trial')", tenant.ID).
		Order("created_at desc").First(&sub).Error; err == nil {
		planID = sub.PlanID
	}

	return &Meta{
		ID:      tenant.ID,
		Slug:    tenant.Slug,
		DBName:  tenant.DBName,
		Status:  tenant.Status,
		RUC:     tenant.RUC,
		PlanID:  planID,
		Modules: modules,
	}, nil
}

func lookupCentralDB(slug string) (*database.Tenant, error) {
	ctx, cancel := context.WithTimeout(context.Background(), config.AppConfig.DBReadTimeout)
	defer cancel()
	var tenant database.Tenant
	if err := database.CentralDB.WithContext(ctx).Where("slug = ?", slug).First(&tenant).Error; err != nil {
		return nil, err
	}
	return &tenant, nil
}

func metaToTenant(m *Meta) *database.Tenant {
	return &database.Tenant{
		ID:     m.ID,
		Slug:   m.Slug,
		DBName: m.DBName,
		Status: m.Status,
		RUC:    m.RUC,
		Plan:   "", // plan string legacy; PlanID en meta si se necesita luego
	}
}
