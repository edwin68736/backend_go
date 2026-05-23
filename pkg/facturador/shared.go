package facturador

import (
	"context"
	"errors"
	"io"
	"math/rand"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"tukifac/config"
)

var (
	sharedOnce   sync.Once
	sharedClient *Client
)

// Shared retorna cliente HTTP singleton con keep-alive y timeout de facturación.
func Shared() *Client {
	sharedOnce.Do(func() {
		cfg := config.AppConfig
		transport := &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout:   10 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 20,
			IdleConnTimeout:     90 * time.Second,
			ForceAttemptHTTP2:   true,
		}
		base := strings.TrimSuffix(cfg.FacturadorBaseURL, "/")
		if base != "" && !strings.HasSuffix(base, "/api/v1") {
			base = base + "/api/v1"
		}
		sharedClient = &Client{
			BaseURL: base,
			Token:   cfg.FacturadorToken,
			HTTP: &http.Client{
				Timeout:   cfg.DBBillingTimeout,
				Transport: transport,
			},
		}
	})
	return sharedClient
}

// DoWithRetry ejecuta fn con reintentos exponenciales + jitter (errores de red / timeout).
func (c *Client) DoWithRetry(ctx context.Context, maxAttempts int, fn func() error) error {
	if maxAttempts < 1 {
		maxAttempts = 1
	}
	var lastErr error
	base := 500 * time.Millisecond
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		lastErr = fn()
		if lastErr == nil {
			return nil
		}
		if !isRetryable(lastErr) || attempt == maxAttempts-1 {
			return lastErr
		}
		sleep := base << attempt
		jitter := time.Duration(rand.Int63n(int64(sleep / 2)))
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(sleep + jitter):
		}
	}
	return lastErr
}

func isRetryable(err error) bool {
	if err == nil {
		return false
	}
	var ne net.Error
	if errors.As(err, &ne) && (ne.Timeout() || ne.Temporary()) {
		return true
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "timeout") ||
		strings.Contains(msg, "503") ||
		strings.Contains(msg, "502") ||
		strings.Contains(msg, "504")
}
