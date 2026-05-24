// Package fiscalqueue cola Redis dedicada a emisión fiscal (separada de billing SaaS).
package fiscalqueue

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"time"

	"tukifac/config"
	"tukifac/pkg/fiscalclient"
	"tukifac/pkg/logger"
	"tukifac/pkg/metrics"

	"github.com/redis/go-redis/v9"
)

const (
	queueKey       = "tukifac:fiscal:queue"
	processingKey  = "tukifac:fiscal:processing"
	claimPrefix    = "tukifac:fiscal:claim:"
	StatusPending  = "pending"
	StatusDone     = "done"
	StatusFailed   = "failed"
)

// Job encola emisión fiscal hacia facturador_lycet.
type Job struct {
	TenantDB   string `json:"tenant_db"`
	TenantID   uint   `json:"tenant_id"`
	TenantSlug string `json:"tenant_slug"`
	SaleID     uint   `json:"sale_id"`
	IdempotencyKey string `json:"idempotency_key"`
}

type Processor func(job Job) error

var (
	rdb       *redis.Client
	processor Processor
	stopCh    chan struct{}
	wg        sync.WaitGroup
	workers   int
)

// Start inicia workers fiscales ERP → facturador.
func Start(cfg *config.Config, client *redis.Client, proc Processor) {
	if client == nil || proc == nil || !fiscalclient.Enabled() {
		return
	}
	rdb = client
	processor = proc
	workers = cfg.FiscalQueueWorkers
	if workers < 1 {
		workers = 2
	}
	stopCh = make(chan struct{})
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go loop(i)
	}
}

func Enabled() bool { return rdb != nil && processor != nil }

func Stop() {
	if stopCh == nil {
		return
	}
	close(stopCh)
	wg.Wait()
}

// TryClaim evita doble encolado concurrente por idempotency key.
func TryClaim(key string, ttl time.Duration) (bool, error) {
	if rdb == nil {
		return true, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	ok, err := rdb.SetNX(ctx, claimPrefix+key, "1", ttl).Result()
	return ok, err
}

func ReleaseClaim(key string) {
	if rdb == nil || key == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = rdb.Del(ctx, claimPrefix+key).Err()
}

// Enqueue añade job fiscal.
func Enqueue(job Job) error {
	if rdb == nil {
		return errors.New("fiscalqueue not enabled")
	}
	b, err := json.Marshal(job)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := rdb.RPush(ctx, queueKey, b).Err(); err != nil {
		return err
	}
	metrics.FiscalQueueEnqueued.Add(1)
	return nil
}

func loop(id int) {
	defer wg.Done()
	for {
		select {
		case <-stopCh:
			return
		default:
		}
		if rdb == nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		raw, err := rdb.BLPop(ctx, 5*time.Second, queueKey).Result()
		cancel()
		if err != nil || len(raw) < 2 {
			continue
		}
		var job Job
		if json.Unmarshal([]byte(raw[1]), &job) != nil {
			continue
		}
		if err := processor(job); err != nil {
			logger.L.Error("fiscal_queue_job_failed",
				slog.Int("worker_id", id),
				slog.Uint64("tenant_id", uint64(job.TenantID)),
				slog.String("tenant_slug", job.TenantSlug),
				slog.Uint64("sale_id", uint64(job.SaleID)),
				slog.Any("error", err),
			)
		}
	}
}
