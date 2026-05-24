package service

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"tukifac/pkg/billingstate"
	"tukifac/pkg/database"
	"tukifac/pkg/fiscaladmin"
)

var documentHTTPClient = &http.Client{Timeout: 45 * time.Second}

// GetInvoiceDocumentContent resuelve XML firmado / CDR desde disco tenant o SSOT fiscal.
func (s *BillingService) GetInvoiceDocumentContent(saleID uint, kind string) ([]byte, string, error) {
	invoice, err := s.GetInvoice(saleID)
	if err != nil {
		return nil, "", err
	}
	if invoice == nil {
		return nil, "", errors.New("comprobante no encontrado")
	}

	var sale database.TenantSale
	_ = s.db.Select("billing_status").First(&sale, saleID).Error
	billingStatus := billingstate.NormalizeBillingStatus(sale.BillingStatus)

	switch kind {
	case "cdr":
		if billingStatus != billingstate.BillingAccepted && billingStatus != billingstate.BillingRejected {
			return nil, "", errors.New("CDR disponible solo tras respuesta de SUNAT (aceptado o rechazado)")
		}
	case "xml":
		if billingStatus == billingstate.BillingPending {
			return nil, "", errors.New("XML firmado enviado a SUNAT no disponible en este estado")
		}
	default:
		return nil, "", fmt.Errorf("tipo no soportado: %s", kind)
	}

	if data, ct, ok := s.readInvoiceDocumentLocal(saleID, kind, invoice); ok {
		return data, ct, nil
	}

	if data, ct, err := s.fetchInvoiceDocumentHTTP(invoice, kind); err == nil && len(data) > 0 {
		return data, ct, nil
	}

	if data, ct, err := s.fetchInvoiceDocumentFromFacturador(invoice, kind); err == nil && len(data) > 0 {
		return data, ct, nil
	}

	return nil, "", errors.New("documento no disponible")
}

func (s *BillingService) readInvoiceDocumentLocal(saleID uint, kind string, invoice *database.TenantInvoice) ([]byte, string, bool) {
	fullPath, err := s.GetInvoiceDocumentPath(saleID, kind)
	if err != nil || fullPath == "" {
		return nil, "", false
	}
	if strings.HasPrefix(strings.ToLower(fullPath), "http://") || strings.HasPrefix(strings.ToLower(fullPath), "https://") {
		return nil, "", false
	}
	data, err := os.ReadFile(fullPath)
	if err != nil || len(data) == 0 {
		return nil, "", false
	}
	return data, contentTypeForKind(kind, filepath.Base(fullPath)), true
}

func (s *BillingService) fetchInvoiceDocumentHTTP(invoice *database.TenantInvoice, kind string) ([]byte, string, error) {
	rawURL := documentURLForKind(invoice, kind)
	if rawURL == "" || rawURL == "(CDR recibido)" {
		return nil, "", errors.New("sin url")
	}
	if !strings.HasPrefix(strings.ToLower(rawURL), "http://") && !strings.HasPrefix(strings.ToLower(rawURL), "https://") {
		return nil, "", errors.New("no es url http")
	}
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, "", err
	}
	resp, err := documentHTTPClient.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil || len(data) == 0 {
		return nil, "", errors.New("vacío")
	}
	ct := resp.Header.Get("Content-Type")
	if ct == "" {
		ct = contentTypeForKind(kind, rawURL)
	}
	return data, ct, nil
}

func (s *BillingService) fetchInvoiceDocumentFromFacturador(invoice *database.TenantInvoice, kind string) ([]byte, string, error) {
	if !fiscaladmin.Enabled() {
		return nil, "", errors.New("fiscaladmin no configurado")
	}
	docUUID := strings.TrimSpace(invoice.ExternalID)
	if docUUID == "" {
		return nil, "", errors.New("sin document_uuid")
	}
	fiscalType := "signed_xml"
	if kind == "cdr" {
		fiscalType = "cdr"
	}
	path := fmt.Sprintf("/api/v1/fiscal/documents/%s/download/%s", url.PathEscape(docUUID), fiscalType)
	data, ct, status, err := fiscaladmin.Download(path)
	if err != nil {
		if status == 404 && kind == "xml" {
			path = fmt.Sprintf("/api/v1/fiscal/documents/%s/download/xml", url.PathEscape(docUUID))
			data, ct, _, err = fiscaladmin.Download(path)
		}
		if err != nil {
			return nil, "", err
		}
	}
	if len(data) == 0 {
		return nil, "", errors.New("vacío")
	}
	if ct == "" {
		ct = contentTypeForKind(kind, "")
	}
	return data, ct, nil
}

func documentURLForKind(invoice *database.TenantInvoice, kind string) string {
	if invoice == nil {
		return ""
	}
	switch kind {
	case "xml":
		return strings.TrimSpace(invoice.XMLURL)
	case "cdr":
		return strings.TrimSpace(invoice.CDRURL)
	case "pdf":
		return strings.TrimSpace(invoice.PDFURL)
	default:
		return ""
	}
}

func contentTypeForKind(kind, hint string) string {
	h := strings.ToLower(hint)
	switch kind {
	case "cdr":
		return "application/zip"
	case "xml":
		return "text/xml; charset=utf-8"
	case "pdf":
		return "application/pdf"
	default:
		if strings.HasSuffix(h, ".zip") {
			return "application/zip"
		}
		if strings.HasSuffix(h, ".xml") {
			return "text/xml; charset=utf-8"
		}
		return "application/octet-stream"
	}
}

// DocumentAvailable indica si el tenant puede descargar el tipo de documento para la venta.
func (s *BillingService) DocumentAvailable(saleID uint, kind string) bool {
	_, _, err := s.GetInvoiceDocumentContent(saleID, kind)
	return err == nil
}
