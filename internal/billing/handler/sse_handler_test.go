package handler

import (
	"bufio"
	"context"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"tukifac/pkg/billingevents"
	"tukifac/pkg/database"

	"github.com/alicebob/miniredis/v2"
	"github.com/gofiber/fiber/v3"
	"github.com/redis/go-redis/v9"
)

// Regresión: el stream SSE moría a los pocos ms (unsub corría al retornar el handler), el navegador
// reconectaba cada 3 s y nunca recibía eventos.

type sseClient struct {
	resp  *http.Response
	lines chan string
}

func startSSEServer(t *testing.T) (baseURL string) {
	t.Helper()
	app := fiber.New()
	app.Get("/events/:tenant", func(c fiber.Ctx) error {
		id := uint(1)
		if c.Params("tenant") == "2" {
			id = 2
		}
		c.Locals("tenant", &database.Tenant{ID: id})
		return (&BillingHandler{}).BillingEventsSSE(c)
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = app.Listener(ln, fiber.ListenConfig{DisableStartupMessage: true}) }()
	t.Cleanup(func() { _ = app.Shutdown() })
	return "http://" + ln.Addr().String()
}

func openSSE(t *testing.T, url string) *sseClient {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	cl := &sseClient{resp: resp, lines: make(chan string, 64)}
	go func() {
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			cl.lines <- sc.Text()
		}
		close(cl.lines)
	}()
	t.Cleanup(func() { _ = resp.Body.Close() })
	return cl
}

// waitLine espera una línea que contenga want; devuelve false si el stream se cerró o venció el plazo.
func (c *sseClient) waitLine(want string, d time.Duration) bool {
	timeout := time.After(d)
	for {
		select {
		case l, ok := <-c.lines:
			if !ok {
				return false
			}
			if strings.Contains(l, want) {
				return true
			}
		case <-timeout:
			return false
		}
	}
}

func setupHub(t *testing.T) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	billingevents.Init(rdb)
	t.Cleanup(func() { billingevents.Shutdown(); _ = rdb.Close() })
}

func waitSubs(want int, d time.Duration) int {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if billingevents.ActiveSubscriptions() == want {
			return want
		}
		time.Sleep(20 * time.Millisecond)
	}
	return billingevents.ActiveSubscriptions()
}

func TestBillingEventsSSE_StaysOpenAndDeliversEvents(t *testing.T) {
	sseKeepaliveInterval = 80 * time.Millisecond
	defer func() { sseKeepaliveInterval = 25 * time.Second }()
	setupHub(t)
	base := startSSEServer(t)

	cl := openSSE(t, base+"/events/1")
	if !cl.waitLine("retry: 3000", 2*time.Second) {
		t.Fatal("no llegó el preámbulo SSE")
	}
	if n := waitSubs(1, 2*time.Second); n != 1 {
		t.Fatalf("suscripciones activas = %d, want 1", n)
	}

	// Sigue abierta: con el bug el stream se cerraba en milisegundos. Esperamos varios keepalives.
	if !cl.waitLine(": keepalive", 2*time.Second) {
		t.Fatal("la conexión se cerró o no hubo keepalive (el stream no permanece abierto)")
	}
	if !cl.waitLine(": keepalive", 2*time.Second) {
		t.Fatal("la conexión no sigue abierta tras varios keepalives")
	}
	if n := billingevents.ActiveSubscriptions(); n != 1 {
		t.Fatalf("suscripciones activas = %d tras mantener abierto, want 1 (no debe reconectar)", n)
	}

	// Entrega de eventos reales.
	billingevents.PublishStatusUpdated(context.Background(), billingevents.NewStatusUpdated(1, 4321, "accepted", "", "ok"))
	if !cl.waitLine("event: billing.status.updated", 3*time.Second) {
		t.Fatal("el evento no llegó al cliente")
	}
	if !cl.waitLine(`"sale_id":4321`, 3*time.Second) {
		t.Fatal("el payload del evento no llegó")
	}
}

func TestBillingEventsSSE_UnsubscribesWhenClientLeaves(t *testing.T) {
	sseKeepaliveInterval = 50 * time.Millisecond
	defer func() { sseKeepaliveInterval = 25 * time.Second }()
	setupHub(t)
	base := startSSEServer(t)

	cl := openSSE(t, base+"/events/1")
	if !cl.waitLine("retry: 3000", 2*time.Second) {
		t.Fatal("sin preámbulo")
	}
	if n := waitSubs(1, 2*time.Second); n != 1 {
		t.Fatalf("suscripciones = %d, want 1", n)
	}
	_ = cl.resp.Body.Close()
	if n := waitSubs(0, 3*time.Second); n != 0 {
		t.Fatalf("tras irse el cliente quedaron %d suscripciones (fuga)", n)
	}
}

func TestBillingEventsSSE_TenantIsolationAndFanout(t *testing.T) {
	sseKeepaliveInterval = 80 * time.Millisecond
	defer func() { sseKeepaliveInterval = 25 * time.Second }()
	setupHub(t)
	base := startSSEServer(t)

	a1 := openSSE(t, base+"/events/1")
	a2 := openSSE(t, base+"/events/1")
	b := openSSE(t, base+"/events/2")
	for _, c := range []*sseClient{a1, a2, b} {
		if !c.waitLine("retry: 3000", 2*time.Second) {
			t.Fatal("sin preámbulo")
		}
	}
	if n := waitSubs(3, 2*time.Second); n != 3 {
		t.Fatalf("suscripciones = %d, want 3", n)
	}
	billingevents.PublishStatusUpdated(context.Background(), billingevents.NewStatusUpdated(1, 99, "rejected", "", "x"))
	if !a1.waitLine(`"sale_id":99`, 3*time.Second) || !a2.waitLine(`"sale_id":99`, 3*time.Second) {
		t.Fatal("los dos clientes del tenant 1 deben recibir el evento")
	}
	if b.waitLine(`"sale_id":99`, 500*time.Millisecond) {
		t.Fatal("el tenant 2 recibió un evento del tenant 1 (fuga entre tenants)")
	}
}
