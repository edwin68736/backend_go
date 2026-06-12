package service

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"tukifac/config"
	"tukifac/pkg/email"
)

// EmailReceiptInput datos para enviar comprobante por correo.
type EmailReceiptInput struct {
	Email     string
	PdfBase64 string
}

func (s *SaleService) EmailReceipt(saleID uint, in EmailReceiptInput) error {
	cfg := config.AppConfig
	if !email.IsConfigured(cfg) {
		return email.ErrNotConfigured
	}
	to := strings.TrimSpace(in.Email)
	if !email.ValidateAddress(to) {
		return errors.New("correo del destinatario inválido")
	}

	sale, err := s.GetByID(saleID)
	if err != nil {
		return err
	}

	pdfBytes, err := decodeReceiptPdfBase64(in.PdfBase64)
	if err != nil {
		return err
	}

	docLabel := strings.TrimSpace(sale.DocType)
	if docLabel == "" {
		docLabel = "Comprobante"
	}
	number := strings.TrimSpace(sale.Number)
	if number == "" {
		number = fmt.Sprintf("%d", sale.ID)
	}
	subject := fmt.Sprintf("Comprobante %s %s", docLabel, number)
	body := fmt.Sprintf(
		"Estimado cliente,\n\nAdjuntamos su comprobante %s %s.\n\nGracias por su preferencia.\n",
		docLabel,
		number,
	)
	fileName := fmt.Sprintf("comprobante-%s.pdf", sanitizeFileToken(number))

	return email.SendWithAttachment(cfg, to, subject, body, fileName, pdfBytes)
}

func decodeReceiptPdfBase64(pdfBase64 string) ([]byte, error) {
	raw := strings.TrimSpace(pdfBase64)
	if raw == "" {
		return nil, errors.New("no se recibió el PDF del comprobante")
	}
	if idx := strings.Index(raw, ","); idx >= 0 {
		raw = raw[idx+1:]
	}
	pdf, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return nil, errors.New("PDF del comprobante inválido")
	}
	if len(pdf) == 0 {
		return nil, errors.New("PDF del comprobante vacío")
	}
	return pdf, nil
}

func sanitizeFileToken(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "venta"
	}
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		} else if r == ' ' {
			b.WriteRune('-')
		}
	}
	out := b.String()
	if out == "" {
		return "venta"
	}
	return out
}
