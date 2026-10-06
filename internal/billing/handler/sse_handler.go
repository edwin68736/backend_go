package handler

import (
	"bufio"
	"fmt"
	"time"

	"tukifac/pkg/billingevents"
	"tukifac/pkg/database"

	"github.com/gofiber/fiber/v3"
)

// sseKeepaliveInterval cada cuánto se escribe un comentario SSE para mantener viva la conexión
// (Cloudflare corta conexiones inactivas) y para detectar al cliente que ya se fue. Variable para tests.
var sseKeepaliveInterval = 25 * time.Second

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
	// El contexto se captura AHORA: dentro del stream writer c ya no es seguro (Fiber lo reutiliza
	// cuando el handler retorna).
	ctx := c.Context()

	return c.SendStreamWriter(func(w *bufio.Writer) {
		// unsub se ejecuta aquí, cuando el stream REALMENTE termina (cliente desconectado, apagado del
		// servidor o error de escritura). Antes era un `defer` del handler: corría al retornar este
		// método —antes de que el writer empezara—, cerraba el canal y el stream moría a los pocos
		// milisegundos; el navegador reconectaba cada 3 s (retry) y nunca recibía eventos.
		defer unsub()

		_, _ = fmt.Fprintf(w, "retry: 3000\n\n")
		if err := w.Flush(); err != nil {
			return
		}

		ping := time.NewTicker(sseKeepaliveInterval)
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
