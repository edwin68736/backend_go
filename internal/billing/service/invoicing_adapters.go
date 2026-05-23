package service

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"tukifac/pkg/billingstate"
	"tukifac/pkg/database"

	"gorm.io/gorm"
)

// legacyBackendAdapter implements LegacyInvoiceAdapter for existing PHP backend flow.
type legacyBackendAdapter struct {
	svc *BillingService
}

// SendToSUNAT sends the invoice data to the legacy backend (Lycet or Tukifac).
func (a *legacyBackendAdapter) SendToSUNAT(saleID uint, companyCfg *database.TenantCompanyConfig) (*database.TenantInvoice, error) {
	if a == nil || a.svc == nil {
		return nil, errors.New("adaptador legacy no inicializado")
	}
	if a.svc.useLycet {
		return a.svc.sendToLycet(saleID, companyCfg)
	}
	return a.svc.sendToTukifac(saleID, companyCfg)
}

type UBLGenerator interface {
	GenerateInvoiceXML(saleID uint, companyCfg *database.TenantCompanyConfig) ([]byte, error)
}

type stubUBLGenerator struct{}

func (g *stubUBLGenerator) GenerateInvoiceXML(saleID uint, companyCfg *database.TenantCompanyConfig) ([]byte, error) {
	return nil, errors.New("generación UBL 2.1 (modo PSE) aún no implementada")
}

// pseAdapter implements PSEInvoiceAdapter for the new PSE flow.
type pseAdapter struct {
	db      *gorm.DB
	ubl     UBLGenerator
	storage *BillingStorageService
	client  *http.Client
}

type validaPSERequest struct {
	NombreArchivo    string `json:"nombre_archivo"`
	ContenidoArchivo string `json:"contenido_archivo"`
}

type validaPSEResponse struct {
	IsSuccess  bool   `json:"isSuccess"`
	Estado     int    `json:"estado"`
	CodigoHash string `json:"codigo_hash"`
	Mensaje    string `json:"mensaje"`
	Message    string `json:"message"`
	Errors     string `json:"errors"`
	XML        string `json:"xml"`         // Base64 signed XML
	CDR        string `json:"cdr"`         // Base64 CDR ZIP (si el proveedor lo devuelve en generarenviar)
	ExternalID string `json:"external_id"` // Hash or ID
}

func (r validaPSEResponse) userMessage() string {
	if strings.TrimSpace(r.Mensaje) != "" {
		return strings.TrimSpace(r.Mensaje)
	}
	if strings.TrimSpace(r.Message) != "" {
		return strings.TrimSpace(r.Message)
	}
	if strings.TrimSpace(r.Errors) != "" {
		return strings.TrimSpace(r.Errors)
	}
	return ""
}

func validaPSEBestEffortMessage(body []byte, primary string) string {
	if strings.TrimSpace(primary) != "" {
		return strings.TrimSpace(primary)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(body, &m); err == nil {
		for _, k := range []string{"mensaje", "message", "error"} {
			if v, ok := m[k]; ok {
				if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
					return strings.TrimSpace(s)
				}
			}
		}
		if v, ok := m["errors"]; ok {
			if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
				return strings.TrimSpace(s)
			}
		}
	}
	raw := strings.TrimSpace(string(body))
	if raw != "" {
		if len(raw) > 600 {
			return raw[:600]
		}
		return raw
	}
	return ""
}

type validaPSECDRResponse struct {
	IsSuccess  bool   `json:"isSuccess"`
	Estado     int    `json:"estado"`
	CodigoHash string `json:"codigo_hash"`
	Mensaje    string `json:"mensaje"`
	Message    string `json:"message"`
	Errors     string `json:"errors"`
	CDR        string `json:"cdr"` // Base64 CDR ZIP
}

func (r validaPSECDRResponse) userMessage() string {
	if strings.TrimSpace(r.Mensaje) != "" {
		return strings.TrimSpace(r.Mensaje)
	}
	if strings.TrimSpace(r.Message) != "" {
		return strings.TrimSpace(r.Message)
	}
	if strings.TrimSpace(r.Errors) != "" {
		return strings.TrimSpace(r.Errors)
	}
	return ""
}

func validaPSEBuildEndpoint(baseURL, path string) string {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if strings.HasSuffix(base, "/api") {
		base = strings.TrimSuffix(base, "/api")
	}
	return base + path
}

// SendToSUNAT generates UBL XML and sends it to the PSE provider.
func (a *pseAdapter) SendToSUNAT(saleID uint, companyCfg *database.TenantCompanyConfig) (*database.TenantInvoice, error) {
	if a == nil || a.db == nil {
		return nil, errors.New("adaptador PSE no inicializado")
	}
	if a.client == nil {
		a.client = &http.Client{Timeout: 30 * time.Second}
	}

	// 1. Validation
	baseURL := strings.TrimSpace(companyCfg.PSEBaseURL)
	if baseURL == "" {
		return a.recordError(saleID, "configuración PSE incompleta: falta pse_base_url")
	}
	token := strings.TrimSpace(companyCfg.PSEToken)
	if token == "" {
		return a.recordError(saleID, "configuración PSE incompleta: falta token PSE")
	}
	if a.ubl == nil {
		return a.recordError(saleID, "generador UBL no configurado")
	}

	// 2. Fetch Sale Metadata (for filename)
	var sale database.TenantSale
	if err := a.db.First(&sale, saleID).Error; err != nil {
		return a.recordError(saleID, fmt.Sprintf("error obteniendo venta: %v", err))
	}

	saleDocType := strings.ToUpper(strings.TrimSpace(sale.DocType))
	docType := "01"
	if saleDocType == "BOLETA" {
		docType = "03"
	}
	// Filename format: RUC-TIPO-SERIE-NUMERO
	filenameBase := fmt.Sprintf("%s-%s-%s-%d", companyCfg.RUC, docType, sale.Series, sale.Correlative)

	// 3. Generate UBL XML
	xmlBytes, err := a.ubl.GenerateInvoiceXML(saleID, companyCfg)
	if err != nil {
		return a.recordError(saleID, fmt.Sprintf("error generando UBL: %v", err))
	}

	unsignedXMLPath := ""
	if a.storage != nil {
		meta, err := a.storage.SaveFile(
			companyCfg.RUC,
			"pse",
			docType,
			sale.Series,
			fmt.Sprintf("%d", sale.Correlative),
			FileTypeXML,
			xmlBytes,
		)
		if err != nil {
			return a.recordError(saleID, fmt.Sprintf("error guardando XML generado: %v", err))
		}
		unsignedXMLPath = meta.FilePath
	}

	// 4. Encode XML to Base64
	xmlBase64 := base64.StdEncoding.EncodeToString(xmlBytes)

	// 5. Prepare Request
	payload := validaPSERequest{
		NombreArchivo:    filenameBase,
		ContenidoArchivo: xmlBase64,
	}
	payloadBytes, _ := json.Marshal(payload)

	path := "/api/cpe/generarenviar"
	if strings.TrimSpace(companyCfg.SunatEnvMode) != "production" {
		path = "/api/cpe/generarenviar-demo"
	}
	endpoint := validaPSEBuildEndpoint(baseURL, path)

	req, err := http.NewRequest("POST", endpoint, bytes.NewBuffer(payloadBytes))
	if err != nil {
		return a.recordError(saleID, fmt.Sprintf("error creando request PSE: %v", err))
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	// 6. Send Request
	resp, err := a.client.Do(req)
	if err != nil {
		return a.recordError(saleID, fmt.Sprintf("error conectando con PSE: %v", err))
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)

	// 7. Parse Response
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return a.recordError(saleID, fmt.Sprintf("PSE retornó error HTTP %d: %s", resp.StatusCode, string(bodyBytes)))
	}

	var pseResp validaPSEResponse
	if err := json.Unmarshal(bodyBytes, &pseResp); err != nil {
		return a.recordErrorWithPayload(
			saleID,
			fmt.Sprintf("error parseando respuesta PSE: %v", err),
			map[string]interface{}{
				"mode":         InvoicingModePSE,
				"sale_id":      saleID,
				"unsigned_xml": unsignedXMLPath,
				"endpoint":     endpoint,
				"http_status":  resp.StatusCode,
				"raw_response": string(bodyBytes),
			},
		)
	}

	if !pseResp.IsSuccess {
		msg := validaPSEBestEffortMessage(bodyBytes, pseResp.userMessage())
		if msg == "" {
			msg = "PSE rechazó envío"
		}
		return a.recordErrorWithPayload(
			saleID,
			fmt.Sprintf("%s (Estado: %d)", msg, pseResp.Estado),
			map[string]interface{}{
				"mode":         InvoicingModePSE,
				"sale_id":      saleID,
				"unsigned_xml": unsignedXMLPath,
				"endpoint":     endpoint,
				"http_status":  resp.StatusCode,
				"response":     pseResp,
				"raw_response": string(bodyBytes),
			},
		)
	}

	var invoice database.TenantInvoice
	a.db.Where("sale_id = ?", saleID).FirstOrCreate(&invoice, database.TenantInvoice{SaleID: saleID})
	if billingstate.HasFinalSunatOutcome(&invoice) {
		return &invoice, errors.New("comprobante ya tiene resultado SUNAT definitivo")
	}

	now := time.Now()
	invoice.SentAt = &now
	invoice.ResponseAt = &now
	invoice.RetryCount++
	invoice.SunatMessage = pseResp.userMessage()
	invoice.SunatHash = pseResp.CodigoHash
	invoice.ExternalID = pseResp.ExternalID
	invoice.SunatCDRCode = "0"

	xmlSaved := false
	cdrSaved := false
	if pseResp.XML != "" && a.storage != nil {
		signedXMLBytes, err := base64.StdEncoding.DecodeString(pseResp.XML)
		if err == nil {
			meta, err := a.storage.SaveFile(
				companyCfg.RUC,
				"pse",
				docType,
				sale.Series,
				fmt.Sprintf("%d", sale.Correlative),
				FileTypeSigned,
				signedXMLBytes,
			)
			if err == nil {
				invoice.XMLURL = meta.FilePath
				xmlSaved = true
			}
		}
	}
	if pseResp.CDR != "" && a.storage != nil {
		cdrBytes, err := base64.StdEncoding.DecodeString(pseResp.CDR)
		if err == nil && len(cdrBytes) > 0 {
			meta, err := a.storage.SaveFile(
				companyCfg.RUC,
				"pse",
				docType,
				sale.Series,
				fmt.Sprintf("%d", sale.Correlative),
				FileTypeCDR,
				cdrBytes,
			)
			if err == nil {
				invoice.CDRURL = meta.FilePath
				cdrSaved = true
			}
		}
	}

	pipeline := billingstate.EvaluatePSE(pseResp.IsSuccess, pseResp.CodigoHash, pseResp.XML, pseResp.CDR, xmlSaved, cdrSaved)
	if !cdrSaved {
		pipeline = billingstate.FAILED
		invoice.SunatCDRCode = ""
	}
	patch := billingstate.BuildPatch(pipeline, &invoice, invoice.SunatMessage)
	patch.XMLURL = invoice.XMLURL
	patch.CDRURL = invoice.CDRURL
	patch.SunatHash = invoice.SunatHash
	billingstate.ApplyToInvoice(&invoice, patch)
	invoice.PayloadJSON = psePayloadToJSON(map[string]interface{}{
		"mode": InvoicingModePSE, "endpoint": endpoint, "unsigned_xml": unsignedXMLPath, "response": pseResp,
	})
	_ = a.db.Save(&invoice).Error
	_ = billingstate.SyncSaleBillingStatus(a.db, saleID, pipeline)

	if pipeline != billingstate.SUNAT_ACCEPTED {
		msg := invoice.SunatMessage
		if msg == "" {
			msg = "PSE no confirmó aceptación SUNAT con CDR"
		}
		return &invoice, fmt.Errorf("%s", msg)
	}
	return &invoice, nil
}

func (a *pseAdapter) CheckStatus(saleID uint, companyCfg *database.TenantCompanyConfig) (*database.TenantInvoice, error) {
	if a == nil || a.db == nil {
		return nil, errors.New("adaptador PSE no inicializado")
	}
	if a.client == nil {
		a.client = &http.Client{Timeout: 30 * time.Second}
	}

	// 1. Validation
	baseURL := strings.TrimSpace(companyCfg.PSEBaseURL)
	if baseURL == "" {
		return nil, errors.New("configuración PSE incompleta: falta pse_base_url")
	}
	token := strings.TrimSpace(companyCfg.PSEToken)
	if token == "" {
		return nil, errors.New("configuración PSE incompleta: falta token PSE")
	}

	// 2. Get Invoice Data
	var invoice database.TenantInvoice
	if err := a.db.Where("sale_id = ?", saleID).First(&invoice).Error; err != nil {
		return nil, fmt.Errorf("comprobante no encontrado para venta %d", saleID)
	}

	var sale database.TenantSale
	if err := a.db.First(&sale, saleID).Error; err != nil {
		return nil, fmt.Errorf("venta no encontrada: %v", err)
	}

	// 3. Construct filename
	saleDocType := strings.ToUpper(strings.TrimSpace(sale.DocType))
	docType := "01"
	if saleDocType == "BOLETA" {
		docType = "03"
	}
	filenameBase := fmt.Sprintf("%s-%s-%s-%d", companyCfg.RUC, docType, sale.Series, sale.Correlative)

	path := "/api/cpe/consultar/" + filenameBase
	if strings.TrimSpace(companyCfg.SunatEnvMode) != "production" {
		path = "/api/cpe/consultar-demo/" + filenameBase
	}
	endpoint := validaPSEBuildEndpoint(baseURL, path)

	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("error creando request CDR: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error conectando con PSE: %v", err)
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("PSE retornó error HTTP %d: %s", resp.StatusCode, string(bodyBytes))
	}

	// 5. Parse Response
	var cdrResp validaPSECDRResponse
	if err := json.Unmarshal(bodyBytes, &cdrResp); err != nil {
		return nil, fmt.Errorf("error parseando respuesta CDR: %v", err)
	}

	// 6. Update Invoice Status
	invoice.SunatMessage = cdrResp.userMessage()
	if cdrResp.CodigoHash != "" {
		invoice.SunatHash = cdrResp.CodigoHash
	}

	cdrSaved := false
	if cdrResp.CDR != "" && a.storage != nil {
		cdrBytes, err := base64.StdEncoding.DecodeString(cdrResp.CDR)
		if err == nil {
			meta, err := a.storage.SaveFile(
				companyCfg.RUC,
				"pse",
				docType,
				sale.Series,
				fmt.Sprintf("%d", sale.Correlative),
				FileTypeCDR,
				cdrBytes,
			)
			if err == nil {
				invoice.CDRURL = meta.FilePath
				cdrSaved = true
			}
		}
	}
	pipeline := billingstate.EvaluatePSE(cdrResp.IsSuccess, cdrResp.CodigoHash, "", cdrResp.CDR, invoice.XMLURL != "", cdrSaved)
	if cdrResp.IsSuccess && cdrSaved {
		invoice.SunatCDRCode = "0"
	} else if !cdrResp.IsSuccess && cdrResp.Estado != 0 {
		pipeline = billingstate.SUNAT_REJECTED
	} else if !cdrSaved {
		pipeline = billingstate.UNKNOWN
	}
	patch := billingstate.BuildPatch(pipeline, &invoice, invoice.SunatMessage)
	billingstate.ApplyToInvoice(&invoice, patch)
	now := time.Now()
	invoice.ResponseAt = &now
	_ = a.db.Save(&invoice).Error
	_ = billingstate.SyncSaleBillingStatus(a.db, saleID, pipeline)
	return &invoice, nil
}

func (a *pseAdapter) recordError(saleID uint, message string) (*database.TenantInvoice, error) {
	return a.recordErrorWithPayload(saleID, message, nil)
}

func (a *pseAdapter) recordErrorWithPayload(saleID uint, message string, payload map[string]interface{}) (*database.TenantInvoice, error) {
	var invoice database.TenantInvoice
	a.db.Where("sale_id = ?", saleID).FirstOrCreate(&invoice, database.TenantInvoice{SaleID: saleID})
	now := time.Now()
	invoice.SentAt = &now
	invoice.ResponseAt = &now
	invoice.RetryCount++
	invoice.SunatStatus = "error"
	invoice.SunatMessage = message
	m := map[string]interface{}{
		"mode":    InvoicingModePSE,
		"sale_id": saleID,
		"error":   message,
	}
	for k, v := range payload {
		m[k] = v
	}
	invoice.PayloadJSON = psePayloadToJSON(m)
	_ = a.db.Save(&invoice).Error
	_ = a.db.Model(&database.TenantSale{}).Where("id = ?", saleID).Update("billing_status", "error").Error
	return &invoice, errors.New(message)
}

func psePayloadToJSON(v interface{}) string {
	b, _ := json.Marshal(v)
	return string(b)
}
