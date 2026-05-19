package ubl

import (
	"encoding/xml"
	"fmt"
	"strings"

	"tukifac/pkg/database"
	"tukifac/pkg/numeroletras"
)

// GenerateInvoiceXML generates UBL 2.1 XML for Invoice (Factura 01, Boleta 03).
// It maps database.TenantSale and TenantCompanyConfig to the Invoice struct.
func GenerateInvoiceXML(sale *database.TenantSale, items []database.TenantSaleItem, companyCfg *database.TenantCompanyConfig, contact *database.TenantContact) ([]byte, error) {
	// 1. Basic Validation
	if sale == nil || companyCfg == nil {
		return nil, fmt.Errorf("sale or company config is nil")
	}

	// 2. Determine Document Type Code (01 Factura, 03 Boleta)
	docType := strings.ToUpper(strings.TrimSpace(sale.DocType))
	invoiceTypeCode := "01"
	if docType == "BOLETA" {
		invoiceTypeCode = "03"
	}

	// 3. Build Invoice Struct
	currency := strings.TrimSpace(sale.Currency)
	if currency == "" {
		currency = "PEN"
	}
	invoice := &Invoice{
		XmlnsCbc:        URN_CBC,
		XmlnsCac:        URN_CAC,
		XmlnsExt:        URN_EXT,
		XmlnsDs:         URN_DS,
		UBLVersionID:    "2.1",
		CustomizationID: "2.0",
		ID:              fmt.Sprintf("%s-%d", sale.Series, sale.Correlative),
		IssueDate:       sale.IssueDate.Format("2006-01-02"),
		IssueTime:       sale.IssueDate.Format("15:04:05"),
		InvoiceTypeCode: &CodeType{
			Value:          invoiceTypeCode,
			ListID:         "0101",
			ListAgencyName: "PE:SUNAT",
			ListName:       "Tipo de Documento",
			ListURI:        "urn:pe:gob:sunat:cpe:see:gem:catalogos:catalogo01",
		},
		DocumentCurrencyCode: &CodeType{
			Value:          currency,
			ListID:         "ISO 4217 Alpha",
			ListName:       "Currency",
			ListAgencyName: "United Nations Economic Commission for Europe",
		},
		Signature:          buildSignature(companyCfg),
		AccountingSupplier: buildSupplier(companyCfg),
		AccountingCustomer: buildCustomer(contact),
		LegalMonetaryTotal: buildMonetaryTotal(sale, currency),
		TaxTotal:           buildTaxTotal(items, currency),
		InvoiceLine:        buildInvoiceLines(items, currency),
		Note: []*NoteType{
			{Value: numeroletras.MontoEnLetras(sale.Total, currency)},
		},
	}

	// 4. Add UBLExtensions placeholder for Signature
	invoice.UBLExtensions = &UBLExtensions{
		UBLExtension: []*UBLExtension{
			{
				ExtensionContent: ExtensionContent{
					Any: " ", // Placeholder, to be replaced by signer
				},
			},
		},
	}

	// 5. Marshal to XML
	return xml.Marshal(invoice)
}

// Helpers

func buildSupplier(cfg *database.TenantCompanyConfig) *AccountingSupplier {
	tradeName := strings.TrimSpace(cfg.TradeName)
	var partyNames []*PartyName
	if tradeName != "" {
		partyNames = []*PartyName{{Name: tradeName}}
	}

	return &AccountingSupplier{
		Party: &Party{
			PartyIdentification: []*PartyIdentification{
				{
					ID: &IdentifierType{
						Value:            cfg.RUC,
						SchemeID:         "6",
						SchemeName:       "Documento de Identidad",
						SchemeAgencyName: "PE:SUNAT",
						SchemeURI:        "urn:pe:gob:sunat:cpe:see:gem:catalogos:catalogo06",
					},
				},
			},
			PartyName: partyNames,
			PartyLegalEntity: []*PartyLegalEntity{
				{
					RegistrationName: cfg.BusinessName,
					RegistrationAddress: &Address{
						ID: &IdentifierType{
							Value:            cfg.Ubigeo,
							SchemeName:       "Ubigeos",
							SchemeAgencyName: "PE:INEI",
						},
						AddressTypeCode: &CodeType{
							Value:          "0000",
							ListName:       "Establecimientos anexos",
							ListAgencyName: "PE:SUNAT",
						},
						AddressLine: []*AddressLine{
							{Line: cfg.Address},
						},
						Country: &Country{
							IdentificationCode: &CodeType{
								Value:          "PE",
								ListID:         "ISO 3166-1",
								ListName:       "Country",
								ListAgencyName: "United Nations Economic Commission for Europe",
							},
						},
					},
				},
			},
		},
	}
}

func buildCustomer(contact *database.TenantContact) *AccountingCustomer {
	if contact == nil {
		// Public/Generic customer
		return &AccountingCustomer{
			Party: &Party{
				PartyIdentification: []*PartyIdentification{
					{
						ID: &IdentifierType{
							Value:            "00000000000",
							SchemeID:         "0",
							SchemeName:       "Documento de Identidad",
							SchemeAgencyName: "PE:SUNAT",
							SchemeURI:        "urn:pe:gob:sunat:cpe:see:gem:catalogos:catalogo06",
						},
					},
				},
				PartyLegalEntity: []*PartyLegalEntity{
					{RegistrationName: "CLIENTES VARIOS"},
				},
			},
		}
	}

	docType := strings.TrimSpace(contact.DocType)
	switch docType {
	case "0", "1", "4", "6", "7":
	default:
		docType = "1"
		if strings.EqualFold(strings.TrimSpace(contact.DocType), "RUC") || len(strings.TrimSpace(contact.DocNumber)) == 11 {
			docType = "6"
		} else if strings.EqualFold(strings.TrimSpace(contact.DocType), "PASAPORTE") {
			docType = "7"
		} else if strings.EqualFold(strings.TrimSpace(contact.DocType), "CE") {
			docType = "4"
		}
	}

	return &AccountingCustomer{
		Party: &Party{
			PartyIdentification: []*PartyIdentification{
				{
					ID: &IdentifierType{
						Value:            strings.TrimSpace(contact.DocNumber),
						SchemeID:         docType,
						SchemeName:       "Documento de Identidad",
						SchemeAgencyName: "PE:SUNAT",
						SchemeURI:        "urn:pe:gob:sunat:cpe:see:gem:catalogos:catalogo06",
					},
				},
			},
			PartyLegalEntity: []*PartyLegalEntity{
				{RegistrationName: contact.BusinessName},
			},
		},
	}
}

func buildSignature(cfg *database.TenantCompanyConfig) *Signature {
	if cfg == nil {
		return nil
	}
	name := strings.TrimSpace(cfg.BusinessName)
	if name == "" {
		name = strings.TrimSpace(cfg.TradeName)
	}
	if name == "" {
		name = "EMPRESA"
	}
	return &Signature{
		ID: cfg.RUC,
		SignatoryParty: &SignatoryParty{
			PartyIdentification: &PartyIdentification{
				ID: &IdentifierType{Value: cfg.RUC, SchemeID: "6"},
			},
			PartyName: &PartyName{Name: name},
		},
		DigitalSignatureAttachment: &DigitalSignatureAttachment{
			ExternalReference: &ExternalReference{URI: "#SignatureSP"},
		},
	}
}

func buildMonetaryTotal(sale *database.TenantSale, currency string) *LegalMonetaryTotal {
	// Assuming sale.Subtotal is TaxableAmount and sale.Total is PayableAmount
	// Needs refinement based on tax logic
	return &LegalMonetaryTotal{
		LineExtensionAmount: &AmountType{Value: sale.Subtotal, CurrencyID: currency},
		TaxInclusiveAmount:  &AmountType{Value: sale.Total, CurrencyID: currency},
		PayableAmount:       &AmountType{Value: sale.Total, CurrencyID: currency},
	}
}

func buildTaxTotal(items []database.TenantSaleItem, currency string) []*TaxTotal {
	var totalTax float64
	var gravadoBase, gravadoTax float64
	var exoBase, inaBase, expBase, graBase float64
	gravadoAff := "10"
	gravadoRate := 18.00

	for _, item := range items {
		totalTax += item.TaxAmount

		aff := strings.TrimSpace(item.IgvAffectationType)
		if aff == "" {
			aff = "10"
		}
		switch igvKindFromAffectation(aff) {
		case "gravado":
			gravadoBase += item.Subtotal
			gravadoTax += item.TaxAmount
			if gravadoAff == "10" && aff != "" {
				gravadoAff = aff
			}
			if item.TaxRate > 0 {
				gravadoRate = item.TaxRate
			}
		case "exonerado":
			exoBase += item.Subtotal
		case "inafecto":
			inaBase += item.Subtotal
		case "exportacion":
			expBase += item.Subtotal
		case "gratuito":
			graBase += item.Subtotal
		}
	}

	subtotals := make([]*TaxSubtotal, 0, 5)
	if gravadoBase > 0 || gravadoTax > 0 {
		subtotals = append(subtotals, &TaxSubtotal{
			TaxableAmount: &AmountType{Value: gravadoBase, CurrencyID: currency},
			TaxAmount:     &AmountType{Value: gravadoTax, CurrencyID: currency},
			TaxCategory:   taxCategoryFor(gravadoAff, gravadoRate),
		})
	}
	if exoBase > 0 {
		subtotals = append(subtotals, &TaxSubtotal{
			TaxableAmount: &AmountType{Value: exoBase, CurrencyID: currency},
			TaxAmount:     &AmountType{Value: 0, CurrencyID: currency},
			TaxCategory:   taxCategoryFor("20", 0),
		})
	}
	if inaBase > 0 {
		subtotals = append(subtotals, &TaxSubtotal{
			TaxableAmount: &AmountType{Value: inaBase, CurrencyID: currency},
			TaxAmount:     &AmountType{Value: 0, CurrencyID: currency},
			TaxCategory:   taxCategoryFor("30", 0),
		})
	}
	if expBase > 0 {
		subtotals = append(subtotals, &TaxSubtotal{
			TaxableAmount: &AmountType{Value: expBase, CurrencyID: currency},
			TaxAmount:     &AmountType{Value: 0, CurrencyID: currency},
			TaxCategory:   taxCategoryFor("40", 0),
		})
	}
	if graBase > 0 {
		subtotals = append(subtotals, &TaxSubtotal{
			TaxableAmount: &AmountType{Value: graBase, CurrencyID: currency},
			TaxAmount:     &AmountType{Value: 0, CurrencyID: currency},
			TaxCategory:   taxCategoryFor("11", 0),
		})
	}

	return []*TaxTotal{{TaxAmount: &AmountType{Value: totalTax, CurrencyID: currency}, TaxSubtotal: subtotals}}
}

func buildInvoiceLines(items []database.TenantSaleItem, currency string) []*InvoiceLine {
	var lines []*InvoiceLine
	for i, item := range items {
		aff := strings.TrimSpace(item.IgvAffectationType)
		if aff == "" {
			aff = "10"
		}

		rate := item.TaxRate
		if rate <= 0 && igvKindFromAffectation(aff) == "gravado" {
			rate = 18.00
		}

		unitPriceWithTax := safeDiv(item.Total, item.Quantity)
		unitValueNoTax := safeDiv(item.Subtotal, item.Quantity)
		lineTaxCat := taxCategoryFor(aff, rate)
		itemCode := strings.TrimSpace(item.Code)
		if itemCode == "" {
			itemCode = fmt.Sprintf("ITEM%03d", i+1)
		}

		lines = append(lines, &InvoiceLine{
			ID: fmt.Sprintf("%d", i+1),
			InvoicedQuantity: &QuantityType{
				Value:    item.Quantity,
				UnitCode: "NIU", // Default unit
			},
			LineExtensionAmount: &AmountType{Value: item.Subtotal, CurrencyID: currency},
			PricingReference: &PricingReference{
				AlternativeConditionPrice: []*Price{
					{
						PriceAmount:   &AmountType{Value: unitPriceWithTax, CurrencyID: currency},
						PriceTypeCode: &CodeType{Value: "01", ListID: "SUNAT"},
					},
				},
			},
			TaxTotal: []*TaxTotal{
				{
					TaxAmount: &AmountType{Value: item.TaxAmount, CurrencyID: currency},
					TaxSubtotal: []*TaxSubtotal{
						{
							TaxableAmount: &AmountType{Value: item.Subtotal, CurrencyID: currency},
							TaxAmount:     &AmountType{Value: item.TaxAmount, CurrencyID: currency},
							TaxCategory:   lineTaxCat,
						},
					},
				},
			},
			Item: &Item{
				Description: []string{item.Description},
				SellersItemIdentification: &ItemIdentification{
					ID: itemCode,
				},
			},
			Price: &Price{
				PriceAmount: &AmountType{Value: unitValueNoTax, CurrencyID: currency},
			},
		})
	}
	return lines
}

func safeDiv(n, d float64) float64 {
	if d == 0 {
		return 0
	}
	return n / d
}

func igvKindFromAffectation(aff string) string {
	aff = strings.TrimSpace(aff)
	if isGratuitoAffectation(aff) {
		return "gratuito"
	}
	if len(aff) > 0 {
		switch aff[0] {
		case '2':
			return "exonerado"
		case '3':
			return "inafecto"
		case '4':
			return "exportacion"
		}
	}
	return "gravado"
}

func isGratuitoAffectation(aff string) bool {
	aff = strings.TrimSpace(aff)
	if len(aff) < 2 {
		return false
	}
	if aff[0] != '1' && aff[0] != '2' && aff[0] != '3' {
		return false
	}
	return aff[1] != '0'
}

func taxCategoryFor(aff string, percent float64) *TaxCategory {
	aff = strings.TrimSpace(aff)
	if aff == "" {
		aff = "10"
	}
	kind := igvKindFromAffectation(aff)

	idValue := "S"
	schemeValue := "1000"
	schemeName := "IGV"
	taxTypeCode := "VAT"

	switch kind {
	case "exonerado":
		idValue = "E"
		schemeValue = "9997"
		schemeName = "EXO"
		percent = 0
	case "inafecto":
		idValue = "O"
		schemeValue = "9998"
		schemeName = "INA"
		taxTypeCode = "FRE"
		percent = 0
	case "exportacion":
		idValue = "G"
		schemeValue = "9995"
		schemeName = "EXP"
		taxTypeCode = "FRE"
		percent = 0
	case "gratuito":
		idValue = "Z"
		schemeValue = "9996"
		schemeName = "GRA"
		taxTypeCode = "FRE"
		percent = 0
	}

	affValue := aff
	if kind == "exonerado" && strings.HasPrefix(affValue, "1") {
		affValue = "20"
	}
	if kind == "inafecto" && strings.HasPrefix(affValue, "1") {
		affValue = "30"
	}

	return &TaxCategory{
		ID: &IdentifierType{
			Value:            idValue,
			SchemeID:         "UN/ECE 5305",
			SchemeName:       "Tax Category Identifier",
			SchemeAgencyName: "United Nations Economic Commission for Europe",
		},
		TaxExemptionReasonCode: &CodeType{
			Value:          affValue,
			ListAgencyName: "PE:SUNAT",
			ListName:       "Afectacion del IGV",
			ListURI:        "urn:pe:gob:sunat:cpe:see:gem:catalogos:catalogo07",
		},
		Percent: percent,
		TaxScheme: &TaxScheme{
			ID: &IdentifierType{
				Value:    schemeValue,
				SchemeID: "UN/ECE 5153",
			},
			Name:        schemeName,
			TaxTypeCode: taxTypeCode,
		},
	}
}
