package handler

import (
	"bufio"
	"fmt"
	"time"

	"tukifac/pkg/billingevents"
	"tukifac/pkg/database"

	"github.com/gofiber/fiber/v3"
)

// BillingEventsSSE GET /api/billing/events — SSE autenticado por tenant.
func (h *BillingHandler) BillingEventsSSE(c fiber.Ctx) error {
	tenant, ok := c.Locals("tenant").(*database.Tenant)
	if !ok || tenant == nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "tenant requerido"})
	}

	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache, no-transform")
	c.Set("Connection", "keep-alive")
	c.Set("X-Accel-Buffering", "no")

	ch, unsub := billingevents.Subscribe(tenant.ID)
	defer unsub()

	return c.SendStreamWriter(func(w *bufio.Writer) {
		ctx := c.Context()
		_, _ = fmt.Fprintf(w, "retry: 3000\n\n")
		_ = w.Flush()

		ping := time.NewTicker(25 * time.Second)
		defer ping.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case data, ok := <-ch:
				if !ok {
					return
				}
				_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", billingevents.EventStatusUpdated, data)
				if err := w.Flush(); err != nil {
					return
				}
			case <-ping.C:
				_, _ = fmt.Fprint(w, ": keepalive\n\n")
				if err := w.Flush(); err != nil {
					return
				}
			}
		}
	})
}
