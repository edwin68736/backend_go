package health

import (
	"fmt"
	"runtime"
	"time"

	"github.com/gofiber/fiber/v3"
)

var startedAt = time.Now()

// Metrics expone métricas mínimas estilo Prometheus (sin dependencia externa).
// Exponer solo en red interna (127.0.0.1 / NPM restringido).
func Metrics(c fiber.Ctx) error {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	uptime := time.Since(startedAt).Seconds()
	body := fmt.Sprintf(`# HELP tukifac_up Backend process is running.
# TYPE tukifac_up gauge
tukifac_up 1
# HELP tukifac_uptime_seconds Uptime since process start.
# TYPE tukifac_uptime_seconds gauge
tukifac_uptime_seconds %.0f
# HELP go_goroutines Number of goroutines.
# TYPE go_goroutines gauge
go_goroutines %d
# HELP go_memstats_alloc_bytes Bytes allocated and in use.
# TYPE go_memstats_alloc_bytes gauge
go_memstats_alloc_bytes %d
# HELP go_memstats_sys_bytes Bytes obtained from system.
# TYPE go_memstats_sys_bytes gauge
go_memstats_sys_bytes %d
`,
		uptime,
		runtime.NumGoroutine(),
		m.Alloc,
		m.Sys,
	)

	c.Set(fiber.HeaderContentType, "text/plain; version=0.0.4; charset=utf-8")
	return c.SendString(body)
}
