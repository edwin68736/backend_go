package ubl

import (
	"encoding/xml"
)

// Namespaces UBL 2.1 SUNAT
const (
	URN_CBC = "urn:oasis:names:specification:ubl:schema:xsd:CommonBasicComponents-2"
	URN_CAC = "urn:oasis:names:specification:ubl:schema:xsd:CommonAggregateComponents-2"
	URN_EXT = "urn:oasis:names:specification:ubl:schema:xsd:CommonExtensionComponents-2"
	URN_DS  = "http://www.w3.org/2000/09/xmldsig#"
)

// Standard UBL Structures

// Invoice (Factura/Boleta)
type Invoice struct {
	XMLName              xml.Name            `xml:"urn:oasis:names:specification:ubl:schema:xsd:Invoice-2 Invoice"`
	XmlnsCbc             string              `xml:"xmlns:cbc,attr"`
	XmlnsCac             string              `xml:"xmlns:cac,attr"`
	XmlnsExt             string              `xml:"xmlns:ext,attr"`
	XmlnsDs              string              `xml:"xmlns:ds,attr"`
	UBLExtensions        *UBLExtensions      `xml:"ext:UBLExtensions,omitempty"`
	UBLVersionID         string              `xml:"cbc:UBLVersionID"`
	CustomizationID      string              `xml:"cbc:CustomizationID"`
	ID                   string              `xml:"cbc:ID"`
	IssueDate            string              `xml:"cbc:IssueDate"`
	IssueTime            string              `xml:"cbc:IssueTime,omitempty"`
	DueDate              string              `xml:"cbc:DueDate,omitempty"`
	InvoiceTypeCode      *CodeType           `xml:"cbc:InvoiceTypeCode"`
	Note                 []*NoteType         `xml:"cbc:Note,omitempty"`
	DocumentCurrencyCode *CodeType           `xml:"cbc:DocumentCurrencyCode"`
	Signature            *Signature          `xml:"cac:Signature,omitempty"`
	AccountingSupplier   *AccountingSupplier `xml:"cac:AccountingSupplierParty"`
	AccountingCustomer   *AccountingCustomer `xml:"cac:AccountingCustomerParty"`
	PrepaidPayment       []*PrepaidPayment   `xml:"cac:PrepaidPayment,omitempty"`
	TaxTotal             []*TaxTotal         `xml:"cac:TaxTotal"`
	LegalMonetaryTotal   *LegalMonetaryTotal `xml:"cac:LegalMonetaryTotal"`
	InvoiceLine          []*InvoiceLine      `xml:"cac:InvoiceLine"`
}

// CreditNote (Nota de Crédito)
type CreditNote struct {
	XMLName              xml.Name               `xml:"urn:oasis:names:specification:ubl:schema:xsd:CreditNote-2 CreditNote"`
	XmlnsCbc             string                 `xml:"xmlns:cbc,attr"`
	XmlnsCac             string                 `xml:"xmlns:cac,attr"`
	XmlnsExt             string                 `xml:"xmlns:ext,attr"`
	XmlnsDs              string                 `xml:"xmlns:ds,attr"`
	UBLExtensions        *UBLExtensions         `xml:"ext:UBLExtensions,omitempty"`
	UBLVersionID         string                 `xml:"cbc:UBLVersionID"`
	CustomizationID      string                 `xml:"cbc:CustomizationID"`
	ID                   string                 `xml:"cbc:ID"`
	IssueDate            string                 `xml:"cbc:IssueDate"`
	IssueTime            string                 `xml:"cbc:IssueTime,omitempty"`
	DocumentCurrencyCode *CodeType              `xml:"cbc:DocumentCurrencyCode"`
	DiscrepancyResponse  []*DiscrepancyResponse `xml:"cac:DiscrepancyResponse"`
	BillingReference     []*BillingReference    `xml:"cac:BillingReference"`
	Signature            *Signature             `xml:"cac:Signature,omitempty"`
	AccountingSupplier   *AccountingSupplier    `xml:"cac:AccountingSupplierParty"`
	AccountingCustomer   *AccountingCustomer    `xml:"cac:AccountingCustomerParty"`
	TaxTotal             []*TaxTotal            `xml:"cac:TaxTotal"`
	LegalMonetaryTotal   *LegalMonetaryTotal    `xml:"cac:LegalMonetaryTotal"`
	CreditNoteLine       []*InvoiceLine         `xml:"cac:CreditNoteLine"` // Similar structure to InvoiceLine
}

// DebitNote (Nota de Débito)
type DebitNote struct {
	XMLName                xml.Name               `xml:"urn:oasis:names:specification:ubl:schema:xsd:DebitNote-2 DebitNote"`
	XmlnsCbc               string                 `xml:"xmlns:cbc,attr"`
	XmlnsCac               string                 `xml:"xmlns:cac,attr"`
	XmlnsExt               string                 `xml:"xmlns:ext,attr"`
	XmlnsDs                string                 `xml:"xmlns:ds,attr"`
	UBLExtensions          *UBLExtensions         `xml:"ext:UBLExtensions,omitempty"`
	UBLVersionID           string                 `xml:"cbc:UBLVersionID"`
	CustomizationID        string                 `xml:"cbc:CustomizationID"`
	ID                     string                 `xml:"cbc:ID"`
	IssueDate              string                 `xml:"cbc:IssueDate"`
	IssueTime              string                 `xml:"cbc:IssueTime,omitempty"`
	DocumentCurrencyCode   *CodeType              `xml:"cbc:DocumentCurrencyCode"`
	DiscrepancyResponse    []*DiscrepancyResponse `xml:"cac:DiscrepancyResponse"`
	BillingReference       []*BillingReference    `xml:"cac:BillingReference"`
	Signature              *Signature             `xml:"cac:Signature,omitempty"`
	AccountingSupplier     *AccountingSupplier    `xml:"cac:AccountingSupplierParty"`
	AccountingCustomer     *AccountingCustomer    `xml:"cac:AccountingCustomerParty"`
	TaxTotal               []*TaxTotal            `xml:"cac:TaxTotal"`
	RequestedMonetaryTotal *LegalMonetaryTotal    `xml:"cac:RequestedMonetaryTotal"`
	DebitNoteLine          []*InvoiceLine         `xml:"cac:DebitNoteLine"` // Similar structure to InvoiceLine
}

// DespatchAdvice (Guía de Remisión)
type DespatchAdvice struct {
	XMLName                xml.Name            `xml:"urn:oasis:names:specification:ubl:schema:xsd:DespatchAdvice-2 DespatchAdvice"`
	XmlnsCbc               string              `xml:"xmlns:cbc,attr"`
	XmlnsCac               string              `xml:"xmlns:cac,attr"`
	XmlnsExt               string              `xml:"xmlns:ext,attr"`
	XmlnsDs                string              `xml:"xmlns:ds,attr"`
	UBLExtensions          *UBLExtensions      `xml:"ext:UBLExtensions,omitempty"`
	UBLVersionID           string              `xml:"cbc:UBLVersionID"`
	CustomizationID        string              `xml:"cbc:CustomizationID"`
	ID                     string              `xml:"cbc:ID"`
	IssueDate              string              `xml:"cbc:IssueDate"`
	IssueTime              string              `xml:"cbc:IssueTime,omitempty"`
	DespatchAdviceTypeCode string              `xml:"cbc:DespatchAdviceTypeCode"`
	Note                   []string            `xml:"cbc:Note,omitempty"`
	Signature              *Signature          `xml:"cac:Signature,omitempty"`
	DespatchSupplierParty  *AccountingSupplier `xml:"cac:DespatchSupplierParty"`
	DeliveryCustomerParty  *AccountingCustomer `xml:"cac:DeliveryCustomerParty"`
	Shipment               *Shipment           `xml:"cac:Shipment"`
	DespatchLine           []*DespatchLine     `xml:"cac:DespatchLine"`
}

// Common Components

type UBLExtensions struct {
	UBLExtension []*UBLExtension `xml:"ext:UBLExtension"`
}

type UBLExtension struct {
	ExtensionContent ExtensionContent `xml:"ext:ExtensionContent"`
}

type ExtensionContent struct {
	// Placeholder for digital signature
	Any string `xml:",innerxml"`
}

type CodeType struct {
	Value          string `xml:",chardata"`
	ListID         string `xml:"listID,attr,omitempty"`
	ListAgencyName string `xml:"listAgencyName,attr,omitempty"`
	ListName       string `xml:"listName,attr,omitempty"`
	ListURI        string `xml:"listURI,attr,omitempty"`
}

type IdentifierType struct {
	Value            string `xml:",chardata"`
	SchemeID         string `xml:"schemeID,attr,omitempty"`
	SchemeName       string `xml:"schemeName,attr,omitempty"`
	SchemeAgencyName string `xml:"schemeAgencyName,attr,omitempty"`
	SchemeURI        string `xml:"schemeURI,attr,omitempty"`
}

// NoteType representa cbc:Note en factura/boleta UBL 2.1.
// SUNAT billService rechaza languageLocaleID en algunos entornos; usar solo Value (texto de leyenda).
type NoteType struct {
	Value string `xml:",chardata"`
}

type Signature struct {
	ID                         string                      `xml:"cbc:ID"`
	SignatoryParty             *SignatoryParty             `xml:"cac:SignatoryParty"`
	DigitalSignatureAttachment *DigitalSignatureAttachment `xml:"cac:DigitalSignatureAttachment"`
}

type SignatoryParty struct {
	PartyIdentification *PartyIdentification `xml:"cac:PartyIdentification"`
	PartyName           *PartyName           `xml:"cac:PartyName"`
}

type DigitalSignatureAttachment struct {
	ExternalReference *ExternalReference `xml:"cac:ExternalReference"`
}

type ExternalReference struct {
	URI string `xml:"cbc:URI"`
}

type AccountingSupplier struct {
	Party *Party `xml:"cac:Party"`
}

type AccountingCustomer struct {
	Party *Party `xml:"cac:Party"`
}

type Party struct {
	PartyIdentification []*PartyIdentification `xml:"cac:PartyIdentification"`
	PartyName           []*PartyName           `xml:"cac:PartyName,omitempty"`
	PartyLegalEntity    []*PartyLegalEntity    `xml:"cac:PartyLegalEntity"`
}

type PartyIdentification struct {
	ID *IdentifierType `xml:"cbc:ID"`
}

type PartyName struct {
	Name string `xml:"cbc:Name"`
}

type PartyLegalEntity struct {
	RegistrationName    string   `xml:"cbc:RegistrationName"`
	RegistrationAddress *Address `xml:"cac:RegistrationAddress,omitempty"`
}

type Address struct {
	ID                  *IdentifierType `xml:"cbc:ID,omitempty"` // Ubigeo
	AddressTypeCode     *CodeType       `xml:"cbc:AddressTypeCode,omitempty"`
	CitySubdivisionName string          `xml:"cbc:CitySubdivisionName,omitempty"` // Urbanización
	CityName            string          `xml:"cbc:CityName,omitempty"`            // Provincia
	CountrySubentity    string          `xml:"cbc:CountrySubentity,omitempty"`    // Departamento
	District            string          `xml:"cbc:District,omitempty"`            // Distrito
	AddressLine         []*AddressLine  `xml:"cac:AddressLine,omitempty"`
	Country             *Country        `xml:"cac:Country,omitempty"`
}

type AddressLine struct {
	Line string `xml:"cbc:Line"`
}

type Country struct {
	IdentificationCode *CodeType `xml:"cbc:IdentificationCode"`
}

type TaxTotal struct {
	TaxAmount   *AmountType    `xml:"cbc:TaxAmount"`
	TaxSubtotal []*TaxSubtotal `xml:"cac:TaxSubtotal"`
}

type TaxSubtotal struct {
	TaxableAmount *AmountType  `xml:"cbc:TaxableAmount"`
	TaxAmount     *AmountType  `xml:"cbc:TaxAmount"`
	TaxCategory   *TaxCategory `xml:"cac:TaxCategory"`
}

type TaxCategory struct {
	ID                     *IdentifierType `xml:"cbc:ID"` // schemeID="UN/ECE 5305", schemeName="Tax Category Identifier", schemeAgencyName="United Nations Economic Commission for Europe"
	Percent                float64         `xml:"cbc:Percent"`
	TaxExemptionReasonCode *CodeType       `xml:"cbc:TaxExemptionReasonCode,omitempty"`
	TierRange              string          `xml:"cbc:TierRange,omitempty"`
	TaxScheme              *TaxScheme      `xml:"cac:TaxScheme"`
}

type TaxScheme struct {
	ID          *IdentifierType `xml:"cbc:ID"`
	Name        string          `xml:"cbc:Name"`
	TaxTypeCode string          `xml:"cbc:TaxTypeCode"`
}

type LegalMonetaryTotal struct {
	LineExtensionAmount  *AmountType `xml:"cbc:LineExtensionAmount"`            // Valor Venta
	TaxInclusiveAmount   *AmountType `xml:"cbc:TaxInclusiveAmount"`             // Precio Venta (inc. impuestos)
	AllowanceTotalAmount *AmountType `xml:"cbc:AllowanceTotalAmount,omitempty"` // Descuentos
	ChargeTotalAmount    *AmountType `xml:"cbc:ChargeTotalAmount,omitempty"`    // Cargos
	PrepaidAmount        *AmountType `xml:"cbc:PrepaidAmount,omitempty"`        // Anticipos
	PayableAmount        *AmountType `xml:"cbc:PayableAmount"`                  // Importe Total Venta
}

type InvoiceLine struct {
	ID                  string            `xml:"cbc:ID"`
	InvoicedQuantity    *QuantityType     `xml:"cbc:InvoicedQuantity,omitempty"` // Invoice
	CreditedQuantity    *QuantityType     `xml:"cbc:CreditedQuantity,omitempty"` // CreditNote
	DebitedQuantity     *QuantityType     `xml:"cbc:DebitedQuantity,omitempty"`  // DebitNote
	LineExtensionAmount *AmountType       `xml:"cbc:LineExtensionAmount"`
	PricingReference    *PricingReference `xml:"cac:PricingReference,omitempty"`
	TaxTotal            []*TaxTotal       `xml:"cac:TaxTotal"`
	Item                *Item             `xml:"cac:Item"`
	Price               *Price            `xml:"cac:Price"`
}

type QuantityType struct {
	Value    float64 `xml:",chardata"`
	UnitCode string  `xml:"unitCode,attr"`
}

type AmountType struct {
	Value      float64 `xml:",chardata"`
	CurrencyID string  `xml:"currencyID,attr"`
}

type PricingReference struct {
	AlternativeConditionPrice []*Price `xml:"cac:AlternativeConditionPrice"`
}

type Price struct {
	PriceAmount   *AmountType `xml:"cbc:PriceAmount"`
	PriceTypeCode *CodeType   `xml:"cbc:PriceTypeCode,omitempty"` // 01=Precio Unitario con IGV, 02=Valor referencial
}

type Item struct {
	Description               []string                   `xml:"cbc:Description"`
	SellersItemIdentification *ItemIdentification        `xml:"cac:SellersItemIdentification,omitempty"`
	CommodityClassification   []*CommodityClassification `xml:"cac:CommodityClassification,omitempty"`
}

type ItemIdentification struct {
	ID string `xml:"cbc:ID"`
}

type CommodityClassification struct {
	ItemClassificationCode *CodeType `xml:"cbc:ItemClassificationCode"`
}

type PrepaidPayment struct {
	ID            string      `xml:"cbc:ID"`
	PaidAmount    *AmountType `xml:"cbc:PaidAmount"`
	InstructionID string      `xml:"cbc:InstructionID,omitempty"`
}

type DiscrepancyResponse struct {
	ReferenceID  string `xml:"cbc:ReferenceID"`
	ResponseCode string `xml:"cbc:ResponseCode"`
	Description  string `xml:"cbc:Description"`
}

type BillingReference struct {
	InvoiceDocumentReference *DocumentReference `xml:"cac:InvoiceDocumentReference"`
}

type DocumentReference struct {
	ID               string `xml:"cbc:ID"`
	DocumentTypeCode string `xml:"cbc:DocumentTypeCode"`
}

// DespatchAdvice specific structures

type Shipment struct {
	ID                    string                   `xml:"cbc:ID"`
	HandlingCode          string                   `xml:"cbc:HandlingCode"`
	HandlingInstructions  string                   `xml:"cbc:HandlingInstructions,omitempty"`
	GrossWeightMeasure    *QuantityType            `xml:"cbc:GrossWeightMeasure"`
	ShipmentStage         []*ShipmentStage         `xml:"cac:ShipmentStage"`
	Delivery              *Delivery                `xml:"cac:Delivery"`
	TransportHandlingUnit []*TransportHandlingUnit `xml:"cac:TransportHandlingUnit,omitempty"`
}

type ShipmentStage struct {
	TransportModeCode string  `xml:"cbc:TransportModeCode"`
	TransitPeriod     *Period `xml:"cac:TransitPeriod"`
	CarrierParty      *Party  `xml:"cac:CarrierParty,omitempty"` // Transportista
	DriverPerson      *Person `xml:"cac:DriverPerson,omitempty"` // Conductor
}

type Period struct {
	StartDate string `xml:"cbc:StartDate"`
}

type Person struct {
	ID                        *CodeType                  `xml:"cbc:ID"`
	FirstName                 string                     `xml:"cbc:FirstName"`
	FamilyName                string                     `xml:"cbc:FamilyName"`
	JobTitle                  string                     `xml:"cbc:JobTitle,omitempty"`
	IdentityDocumentReference *IdentityDocumentReference `xml:"cac:IdentityDocumentReference,omitempty"` // Licencia
}

type IdentityDocumentReference struct {
	ID string `xml:"cbc:ID"`
}

type Delivery struct {
	DeliveryAddress *Address `xml:"cac:DeliveryAddress"`
}

type TransportHandlingUnit struct {
	TransportEquipment []*TransportEquipment `xml:"cac:TransportEquipment"`
}

type TransportEquipment struct {
	ID string `xml:"cbc:ID"` // Placa
}

type DespatchLine struct {
	ID                 string              `xml:"cbc:ID"`
	DeliveredQuantity  *QuantityType       `xml:"cbc:DeliveredQuantity"`
	OrderLineReference *OrderLineReference `xml:"cac:OrderLineReference,omitempty"`
	Item               *Item               `xml:"cac:Item"`
}

type OrderLineReference struct {
	LineID string `xml:"cbc:LineID"`
}
