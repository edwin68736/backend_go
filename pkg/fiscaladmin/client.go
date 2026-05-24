// Package fiscaladmin proxy administrativo al facturador_lycet (source of truth fiscal).
package fiscaladmin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client HTTP admin al facturador (sin duplicar BD fiscal).
type Client struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

var shared *Client

// Init configura cliente global (misma URL/token que fiscalclient.Emit).
func Init(baseURL, token string) {
	baseURL = strings.TrimSuffix(strings.TrimSpace(baseURL), "/")
	shared = &Client{
		BaseURL: baseURL,
		Token:   token,
		HTTP:    &http.Client{Timeout: 30 * time.Second},
	}
}

// Enabled indica si el proxy fiscal admin está configurado.
func Enabled() bool {
	return shared != nil && shared.BaseURL != "" && shared.Token != ""
}

func addToken(base, path, token string) string {
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	return base + path + sep + "token=" + url.QueryEscape(token)
}

// GetJSON GET /api/v1/fiscal/* con query params.
func GetJSON(path string, query url.Values) (json.RawMessage, int, error) {
	if !Enabled() {
		return nil, 0, fmt.Errorf("fiscaladmin no configurado")
	}
	if query == nil {
		query = url.Values{}
	}
	endpoint := addToken(shared.BaseURL, path, shared.Token)
	if enc := query.Encode(); enc != "" {
		endpoint += "&" + enc
	}
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := shared.HTTP.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("fiscaladmin GET: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return raw, resp.StatusCode, fmt.Errorf("fiscaladmin GET HTTP %d", resp.StatusCode)
	}
	return raw, resp.StatusCode, nil
}

// PostJSON POST /api/v1/fiscal/* con body JSON.
func PostJSON(path string, body interface{}) (json.RawMessage, int, error) {
	if !Enabled() {
		return nil, 0, fmt.Errorf("fiscaladmin no configurado")
	}
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			return nil, 0, err
		}
	}
	endpoint := addToken(shared.BaseURL, path, shared.Token)
	req, err := http.NewRequest(http.MethodPost, endpoint, &buf)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := shared.HTTP.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("fiscaladmin POST: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return raw, resp.StatusCode, fmt.Errorf("fiscaladmin POST HTTP %d", resp.StatusCode)
	}
	return raw, resp.StatusCode, nil
}

// Download GET binario (XML/CDR/PDF).
func Download(path string) ([]byte, string, int, error) {
	if !Enabled() {
		return nil, "", 0, fmt.Errorf("fiscaladmin no configurado")
	}
	endpoint := addToken(shared.BaseURL, path, shared.Token)
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, "", 0, err
	}
	resp, err := shared.HTTP.Do(req)
	if err != nil {
		return nil, "", 0, fmt.Errorf("fiscaladmin download: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	ct := resp.Header.Get("Content-Type")
	if resp.StatusCode >= 400 {
		return raw, ct, resp.StatusCode, fmt.Errorf("fiscaladmin download HTTP %d", resp.StatusCode)
	}
	return raw, ct, resp.StatusCode, nil
}

// QueryFromFiber copia query string de Fiber a url.Values.
func QueryFromFiber(queries map[string]string) url.Values {
	q := url.Values{}
	for k, v := range queries {
		if v != "" {
			q.Set(k, v)
		}
	}
	return q
}
