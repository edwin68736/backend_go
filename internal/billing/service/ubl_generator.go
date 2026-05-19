package service

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"strings"

	"tukifac/internal/billing/ubl"
	"tukifac/pkg/database"

	"gorm.io/gorm"
)

// realUBLGenerator implements UBLGenerator interface using the ubl package.
type realUBLGenerator struct {
	db *gorm.DB
}

// NewUBLGenerator creates a new instance of the UBL generator.
func NewUBLGenerator(db *gorm.DB) UBLGenerator {
	return &realUBLGenerator{db: db}
}

// GenerateInvoiceXML generates the UBL 2.1 XML for a given sale.
func (g *realUBLGenerator) GenerateInvoiceXML(saleID uint, companyCfg *database.TenantCompanyConfig) ([]byte, error) {
	// 1. Fetch Sale Data
	var sale database.TenantSale
	if err := g.db.First(&sale, saleID).Error; err != nil {
		return nil, fmt.Errorf("venta no encontrada: %w", err)
	}

	// 2. Fetch Sale Items
	var items []database.TenantSaleItem
	if err := g.db.Where("sale_id = ?", saleID).Find(&items).Error; err != nil {
		return nil, fmt.Errorf("error obteniendo ítems: %w", err)
	}

	// 3. Fetch Customer Contact
	var contact database.TenantContact
	if sale.ContactID != nil {
		if err := g.db.First(&contact, *sale.ContactID).Error; err != nil {
			// Log warning or treat as generic customer if not found
		}
	}

	// 4. Generate XML based on DocType
	docType := strings.ToUpper(strings.TrimSpace(sale.DocType))
	docType = strings.ReplaceAll(docType, " ", "_")
	switch docType {
	case "FACTURA", "BOLETA":
		b, err := ubl.GenerateInvoiceXML(&sale, items, companyCfg, &contact)
		if err != nil {
			return nil, err
		}
		if !bytes.HasPrefix(b, []byte("<?xml")) {
			b = append([]byte(xml.Header), b...)
		}
		return b, nil
	case "NOTA_CREDITO":
		return nil, errors.New("generación XML Nota de Crédito aún no implementada")
	case "NOTA_DEBITO":
		return nil, errors.New("generación XML Nota de Débito aún no implementada")
	case "GUIA_REMISION":
		return nil, errors.New("generación XML Guía de Remisión aún no implementada")
	default:
		return nil, fmt.Errorf("tipo de documento no soportado para generación UBL: %s", sale.DocType)
	}
}
