package fiscalqueue

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"tukifac/config"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestConcurrentEnqueue100Tenants(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	defer mr.Close()

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	cfg := &config.Config{FiscalQueueWorkers: 4}
	Start(cfg, rdb, func(job Job) error { return nil })
	defer Stop()

	const tenants = 100
	var created int64
	var wg sync.WaitGroup
	start := time.Now()

	for i := 1; i <= tenants; i++ {
		wg.Add(1)
		go func(tenantID int) {
			defer wg.Done()
			key := fmt.Sprintf("load:%d:03:B001:1", tenantID)
			ok, err := TryClaim(key, time.Minute)
			if err != nil {
				t.Errorf("claim: %v", err)
				return
			}
			if !ok {
				return
			}
			atomic.AddInt64(&created, 1)
			_ = Enqueue(Job{
				TenantSlug:     fmt.Sprintf("tenant-%d", tenantID),
				TenantID:       uint(tenantID),
				SaleID:         uint(tenantID),
				IdempotencyKey: key,
			})
		}(i)
	}
	wg.Wait()
	elapsed := time.Since(start)

	if created != tenants {
		t.Fatalf("expected %d created, got %d", tenants, created)
	}
	t.Logf("100 tenants concurrent enqueue: created=%d elapsed=%v", created, elapsed)
}
