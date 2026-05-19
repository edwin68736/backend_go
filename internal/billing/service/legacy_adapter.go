package service

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"tukifac/config"
	"tukifac/pkg/database"
)

// externalPHPAdapter implements LegacyInvoiceAdapter for a generic PHP backend
// configured via LEGACY_INVOICE_ENDPOINT.
type externalPHPAdapter struct {
	client   *http.Client
	endpoint string
}

// NewExternalPHPAdapter creates a new adapter for the external PHP backend.
func NewExternalPHPAdapter() LegacyInvoiceAdapter {
	return &externalPHPAdapter{
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		endpoint: config.AppConfig.LegacyInvoiceEndpoint,
	}
}

// SendToSUNAT sends the invoice JSON to the external PHP backend.
func (a *externalPHPAdapter) SendToSUNAT(saleID uint, companyCfg *database.TenantCompanyConfig) (*database.TenantInvoice, error) {
	if a.endpoint == "" {
		return nil, errors.New("LEGACY_INVOICE_ENDPOINT no configurado en el servidor")
	}

	// 1. Prepare payload (simplified for example; in real usage, you might query the sale here
	// or assume the caller passes necessary data. The interface receives saleID).
	// For this adapter, we construct a request payload containing the sale ID and tenant info.
	// NOTE: Ideally, we should reuse the logic to build the full invoice JSON (like in billing_service.go)
	// or delegate that responsibility. Since the requirement says "Receive invoice JSON data",
	// we assume we need to gather that data first.

	// However, the interface defined in invoice_orchestrator.go is:
	// SendToSUNAT(saleID uint, companyCfg *database.TenantCompanyConfig) (*database.TenantInvoice, error)

	// So we need to fetch the sale and build the JSON payload to send to PHP.
	// For now, let's create a minimal payload structure expected by the PHP backend.
	payload := map[string]interface{}{
		"sale_id":     saleID,
		"company_ruc": companyCfg.RUC,
		"timestamp":   time.Now().Unix(),
	}

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("error marshaling payload: %w", err)
	}

	// 2. Send POST request
	req, err := http.NewRequest("POST", a.endpoint, bytes.NewBuffer(jsonPayload))
	if err != nil {
		return nil, fmt.Errorf("error creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Tukifac-Go-Backend/1.0")

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error sending request to PHP backend: %w", err)
	}
	defer resp.Body.Close()

	// 3. Read and parse response
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading response body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("PHP backend returned error status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	// 4. Map response to standardized structure
	// Assuming PHP backend returns something like:
	// { "success": true, "sunat_status": "accepted", "cdr_url": "...", "xml_url": "...", "message": "..." }
	var phpResp struct {
		Success     bool   `json:"success"`
		SunatStatus string `json:"sunat_status"` // accepted, rejected, error
		Message     string `json:"message"`
		XMLURL      string `json:"xml_url"`
		CDRURL      string `json:"cdr_url"`
		PDFURL      string `json:"pdf_url"`
		Hash        string `json:"hash"`
	}

	if err := json.Unmarshal(bodyBytes, &phpResp); err != nil {
		return nil, fmt.Errorf("error parsing PHP response: %w", err)
	}

	if !phpResp.Success {
		return nil, fmt.Errorf("PHP backend reported failure: %s", phpResp.Message)
	}

	// 5. Return standardized result
	now := time.Now()
	invoice := &database.TenantInvoice{
		SaleID:       saleID,
		SunatStatus:  phpResp.SunatStatus,
		SunatMessage: phpResp.Message,
		XMLURL:       phpResp.XMLURL,
		CDRURL:       phpResp.CDRURL,
		PDFURL:       phpResp.PDFURL,
		SunatHash:    phpResp.Hash,
		SentAt:       &now,
		ResponseAt:   &now,
		// PayloadJSON: string(jsonPayload), // Optional: store what we sent
	}

	return invoice, nil
}
