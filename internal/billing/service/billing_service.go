package service

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"tukifac/config"
	salesvc "tukifac/internal/sales/service"
	"tukifac/pkg/database"
	"tukifac/pkg/facturador"
	"tukifac/pkg/tenantstorage"

	"gorm.io/gorm"
)

type BillingService struct {
	db           *gorm.DB
	baseURL      string
	token        string
	useLycet     bool // true = Lycet facturador; false = Tukifac legacy
	orchestrator *InvoiceOrchestrator
}

func NewBillingService(db *gorm.DB) *BillingService {
	svc := &BillingService{db: db}
	// Prioridad: Lycet Facturador si está configurado; si no, Tukifac (legacy)
	if config.AppConfig.FacturadorBaseURL != "" && config.AppConfig.FacturadorToken != "" {
		svc.baseURL = strings.TrimSuffix(config.AppConfig.FacturadorBaseURL, "/")
		svc.token = config.AppConfig.FacturadorToken
		svc.useLycet = true
	} else {
		svc.baseURL = config.AppConfig.TukifacBaseURL
		svc.token = config.AppConfig.TukifacAPIToken
		var cfg database.TenantCompanyConfig
		if err := db.Select("tukifac_token").First(&cfg).Error; err == nil && cfg.TukifacToken != "" {
			svc.token = cfg.TukifacToken
		}
	}
	var legacyAdapter LegacyInvoiceAdapter
	if config.AppConfig.LegacyInvoiceEndpoint != "" {
		legacyAdapter = NewExternalPHPAdapter()
	} else {
		legacyAdapter = &legacyBackendAdapter{svc: svc}
	}
	svc.orchestrator = NewInvoiceOrchestrator(
		db,
		legacyAdapter,
		&pseAdapter{
			db:      db,
			ubl:     NewUBLGenerator(db),
			storage: NewBillingStorageService(config.AppConfig.InvoiceStoragePath),
			client:  &http.Client{Timeout: 30 * time.Second},
		},
	)
	return svc
}

// round2 redondea a 2 decimales para montos SUNAT (evita discrepancias 4310/4312).
func round2(v float64) float64 { return math.Round(v*100) / 100 }

// resolveUbigeoToAddress obtiene los nombres de departamento, provincia y distrito desde las tablas de ubigeo.
// SUNAT no acepta "-" en estos campos; se debe enviar el nombre real.
func (s *BillingService) resolveUbigeoToAddress(ubigeo string) (dep, prov, dist string, err error) {
	ubigeo = strings.TrimSpace(ubigeo)
	if ubigeo == "" {
		return "", "", "", fmt.Errorf("ubigeo es obligatorio para el domicilio fiscal")
	}
	var distrito database.UbiDistrito
	if err := s.db.Where("id = ?", ubigeo).First(&distrito).Error; err != nil || distrito.ID == "" {
		return "", "", "", fmt.Errorf("ubigeo %s no encontrado en catálogo; configure un distrito válido", ubigeo)
	}
	var provincia database.UbiProvincia
	if err := s.db.Where("id = ?", distrito.ProvinciaID).First(&provincia).Error; err != nil || provincia.ID == "" {
		return "", "", "", fmt.Errorf("provincia del ubigeo no encontrada")
	}
	var region database.UbiRegion
	if err := s.db.Where("id = ?", distrito.RegionID).First(&region).Error; err != nil || region.ID == "" {
		return "", "", "", fmt.Errorf("departamento del ubigeo no encontrado")
	}
	return region.Nombre, provincia.Nombre, distrito.Nombre, nil
}

// buildInvoiceAddressFromUbigeo arma una InvoiceAddress con nombres reales (sin "-") para SUNAT.
func (s *BillingService) buildInvoiceAddressFromUbigeo(ubigeo, direccion string) (facturador.InvoiceAddress, error) {
	ubigeo = strings.TrimSpace(ubigeo)
	direccion = strings.TrimSpace(direccion)
	if ubigeo == "" || direccion == "" {
		return facturador.InvoiceAddress{}, fmt.Errorf("ubigeo y dirección son obligatorios para SUNAT")
	}
	dep, prov, dist, err := s.resolveUbigeoToAddress(ubigeo)
	if err != nil {
		return facturador.InvoiceAddress{}, err
	}
	return facturador.InvoiceAddress{
		Ubigueo: ubigeo, CodigoPais: "PE",
		Departamento: dep, Provincia: prov, Distrito: dist,
		Urbanizacion: "", Direccion: direccion,
	}, nil
}

// GetNotificationCounts devuelve cantidades de comprobantes electrónicos por estado (solo 01, 03, 07, 08).
// Para notificaciones en el header del tenant.
func (s *BillingService) GetNotificationCounts() (pending, errorCount, rejected int64, err error) {
	electronicCodes := []string{"01", "03", "07", "08"}
	for _, status := range []string{"pending", "error", "rejected"} {
		var n int64
		e := s.db.Model(&database.TenantSale{}).
			Joins("JOIN tenant_document_series ON tenant_document_series.id = tenant_sales.series_id").
			Where("tenant_document_series.sunat_code IN ?", electronicCodes).
			Where("tenant_sales.billing_status = ?", status).
			Count(&n).Error
		if e != nil {
			return 0, 0, 0, e
		}
		switch status {
		case "pending":
			pending = n
		case "error":
			errorCount = n
		case "rejected":
			rejected = n
		}
	}
	return pending, errorCount, rejected, nil
}

type TukifacInvoiceRequest struct {
	TipoDocumento     string               `json:"tipo_documento"`
	Serie             string               `json:"serie"`
	Correlativo       string               `json:"correlativo"`
	FechaEmision      string               `json:"fecha_emision"`
	TipoMoneda        string               `json:"tipo_moneda"`
	EmisorRUC         string               `json:"emisor_ruc"`
	EmisorRazonSocial string               `json:"emisor_razon_social"`
	EmisorDireccion   string               `json:"emisor_direccion"`
	ReceptorTipoDoc   string               `json:"receptor_tipo_doc"`
	ReceptorNumDoc    string               `json:"receptor_num_doc"`
	ReceptorNombre    string               `json:"receptor_nombre"`
	ReceptorDireccion string               `json:"receptor_direccion"`
	SubTotal          float64              `json:"sub_total"`
	IGV               float64              `json:"igv"`
	Total             float64              `json:"total"`
	Items             []TukifacItemRequest `json:"items"`
}

type TukifacItemRequest struct {
	Codigo         string  `json:"codigo"`
	Descripcion    string  `json:"descripcion"`
	Unidad         string  `json:"unidad"`
	Cantidad       float64 `json:"cantidad"`
	PrecioUnit     float64 `json:"precio_unit"`    // precio unitario con IGV (si gravado)
	ValorUnitario  float64 `json:"valor_unitario"` // precio unitario sin IGV
	Descuento      float64 `json:"descuento"`
	SubTotal       float64 `json:"sub_total"` // valor de venta sin IGV
	IGV            float64 `json:"igv"`
	Total          float64 `json:"total"`
	TipoAfectacion string  `json:"tipo_afectacion"` // código catálogo N°07 SUNAT
	TasaIGV        float64 `json:"tasa_igv"`        // tasa efectiva aplicada
}

type TukifacResponse struct {
	Success    bool   `json:"success"`
	ExternalID string `json:"id"`
	Estado     string `json:"estado"`
	Mensaje    string `json:"mensaje"`
	XMLURL     string `json:"xml_url"`
	CDRURL     string `json:"cdr_url"`
	PDFURL     string `json:"pdf_url"`
}

// SendToSUNAT envía una venta al facturador (Lycet o Tukifac) para facturación electrónica.
// Requiere que la empresa tenga activada la conexión SUNAT en su configuración.
func (s *BillingService) SendToSUNAT(saleID uint) (*database.TenantInvoice, error) {
	if s.orchestrator == nil {
		var legacyAdapter LegacyInvoiceAdapter

		// If LEGACY_INVOICE_ENDPOINT is set, use the generic external PHP adapter.
		// Otherwise, fallback to the internal logic (Lycet/Tukifac) via legacyBackendAdapter.
		if config.AppConfig.LegacyInvoiceEndpoint != "" {
			legacyAdapter = NewExternalPHPAdapter()
		} else {
			// If no external endpoint is configured, we use the internal logic (Lycet/Tukifac)
			// which acts as the "legacy adapter" in the current architecture.
			legacyAdapter = &legacyBackendAdapter{svc: s}
		}

		s.orchestrator = NewInvoiceOrchestrator(
			s.db,
			legacyAdapter,
			&pseAdapter{
				db:      s.db,
				ubl:     NewUBLGenerator(s.db),
				storage: NewBillingStorageService(config.AppConfig.InvoiceStoragePath),
				client:  &http.Client{Timeout: 30 * time.Second},
			},
		)
	}
	return s.orchestrator.SendToSUNAT(saleID)
}

func (s *BillingService) sendToLycet(saleID uint, companyCfg *database.TenantCompanyConfig) (*database.TenantInvoice, error) {
	if s.baseURL == "" || s.token == "" {
		return nil, errors.New("URL o token del facturador no configurados — configura FACTURADOR_BASE_URL y FACTURADOR_TOKEN en el servidor")
	}
	var sale database.TenantSale
	if err := s.db.First(&sale, saleID).Error; err != nil {
		return nil, errors.New("venta no encontrada")
	}
	var items []database.TenantSaleItem
	s.db.Where("sale_id = ?", saleID).Find(&items)
	var contact database.TenantContact
	if sale.ContactID != nil {
		s.db.First(&contact, *sale.ContactID)
	}
	tipoDoc := "03"
	if sale.DocType == "FACTURA" || strings.TrimSpace(getSeriesSunatCode(s.db, sale.SeriesID)) == "01" {
		tipoDoc = "01"
	}
	fechaEmision := sale.IssueDate.Format("2006-01-02T15:04:05-07:00")
	ubigueo := strings.TrimSpace(companyCfg.Ubigeo)
	if ubigueo == "" {
		return nil, fmt.Errorf("configure el ubigeo del domicilio fiscal en Configuración → Empresa")
	}
	dep, prov, dist, err := s.resolveUbigeoToAddress(ubigueo)
	if err != nil {
		return nil, err
	}
	direccionEmpresa := strings.TrimSpace(companyCfg.Address)
	if direccionEmpresa == "" {
		return nil, fmt.Errorf("configure la dirección completa del domicilio fiscal en Configuración → Empresa")
	}
	companyAddr := facturador.InvoiceAddress{
		Ubigueo:      ubigueo,
		CodigoPais:   "PE",
		Departamento: dep,
		Provincia:    prov,
		Distrito:     dist,
		Urbanizacion: "",
		Direccion:    direccionEmpresa,
	}
	tipoOperacion := "0101"
	// Cliente: tipoDoc catálogo 06 (1=DNI, 6=RUC, 4=CE…); numDoc obligatorio
	clientTipoDoc := "6"
	clientNumDoc := "00000000000"
	clientRzn := "CLIENTE VARIOS"
	clientDir := ""
	clientUbigeo := ""
	if contact.ID > 0 {
		clientRzn = contact.BusinessName
		clientNumDoc = contact.DocNumber
		clientDir = strings.TrimSpace(contact.Address)
		clientUbigeo = strings.TrimSpace(contact.Ubigeo)
		clientDir, clientUbigeo = database.NormalizeTenantContactAddressUbigeo(clientDir, clientUbigeo)
		switch strings.ToUpper(contact.DocType) {
		case "DNI", "1":
			clientTipoDoc = "1"
		case "RUC", "6":
			clientTipoDoc = "6"
		case "CE", "4", "CARNET":
			clientTipoDoc = "4"
		case "PASAPORTE", "7":
			clientTipoDoc = "7"
		default:
			if len(contact.DocNumber) == 8 {
				clientTipoDoc = "1"
			} else if len(contact.DocNumber) == 11 {
				clientTipoDoc = "6"
			}
		}
	}
	if clientRzn == "" {
		clientRzn = "CLIENTE VARIOS"
	}
	if clientNumDoc == "" {
		clientNumDoc = "00000000000"
	}
	// Cliente no documentado (Lycet: schemeID="0" en XML): enviar tipoDoc "0" y numDoc "99999999999"
	if clientNumDoc == "00000000000" || clientNumDoc == "00000000" || clientNumDoc == "99999999" ||
		(contact.ID > 0 && (strings.ToUpper(strings.TrimSpace(contact.DocType)) == "SIN DOCUMENTO" || strings.TrimSpace(contact.DocType) == "0")) {
		clientTipoDoc = "0"
		clientNumDoc = "99999999999"
	}
	// Dirección cliente: SUNAT no acepta "-"; debe ser dirección y ubigeo reales
	clientAddr := facturador.InvoiceAddress{Ubigueo: "150101", CodigoPais: "PE", Departamento: "Lima", Provincia: "Lima", Distrito: "Lima", Urbanizacion: "", Direccion: clientDir}
	if contact.ID > 0 {
		depC, provC, distC, errC := s.resolveUbigeoToAddress(clientUbigeo)
		if errC != nil {
			return nil, fmt.Errorf("cliente: %w", errC)
		}
		clientAddr = facturador.InvoiceAddress{
			Ubigueo: clientUbigeo, CodigoPais: "PE",
			Departamento: depC, Provincia: provC, Distrito: distC,
			Urbanizacion: "", Direccion: clientDir,
		}
	} else {
		// Cliente genérico (sin contacto): SUNAT exige dirección real; no se acepta "-"
		return nil, errors.New("para facturación electrónica debe asignar un cliente con dirección y ubigeo completos en la venta")
	}
	// Sin fallbacks: el porcentaje IGV debe estar configurado en la empresa (Configuración → SUNAT/IGV).
	if companyCfg.TaxRate <= 0 {
		return nil, fmt.Errorf("configure el porcentaje de IGV en Configuración de la empresa (SUNAT); no se usan valores por defecto")
	}
	companyTaxRate := companyCfg.TaxRate
	details := make([]facturador.InvoiceDetail, len(items))
	for i, item := range items {
		aff := strings.TrimSpace(item.IgvAffectationType)
		if aff == "" {
			return nil, fmt.Errorf("el ítem «%s» no tiene tipo de afectación IGV; configúrelo en el producto (Catálogo SUNAT 07: 10 Gravado, 20 Exonerado, 30 Inafecto)", strings.TrimSpace(item.Description))
		}
		if aff == "10" && item.TaxRate <= 0 {
			return nil, fmt.Errorf("el ítem «%s» es gravado pero tiene porcentaje IGV en 0; configúrelo en el producto", strings.TrimSpace(item.Description))
		}
		mtoValorVenta := round2(item.Subtotal)
		igv := round2(item.TaxAmount)
		cantidad := item.Quantity
		if cantidad <= 0 {
			return nil, fmt.Errorf("el ítem «%s» tiene cantidad inválida", strings.TrimSpace(item.Description))
		}
		mtoValorUnitario := round2(mtoValorVenta / cantidad)
		// MtoPrecioUnitario debe cumplir (MtoPrecioUnitario * Cantidad) = (MtoValorVenta + Igv) para que el facturador no discrepe en TaxInclusiveAmount
		mtoPrecioUnitario := round2((mtoValorVenta + igv) / cantidad)
		codProd := strings.TrimSpace(item.Code)
		if codProd == "" {
			return nil, fmt.Errorf("el ítem «%s» no tiene código de producto", strings.TrimSpace(item.Description))
		}
		desc := strings.TrimSpace(item.Description)
		if desc == "" {
			return nil, fmt.Errorf("ítem en posición %d sin descripción", i+1)
		}
		porcentajeIgv := round2(item.TaxRate)
		if aff != "10" {
			porcentajeIgv = round2(companyTaxRate)
		}
		details[i] = facturador.InvoiceDetail{
			Unidad:            normUnit(item.Unit),
			Cantidad:          cantidad,
			CodProducto:       codProd,
			Descripcion:       desc,
			MtoValorUnitario:  mtoValorUnitario,
			MtoValorVenta:     mtoValorVenta,
			TipAfeIgv:         aff,
			MtoBaseIgv:        mtoValorVenta, // Por línea: gravado = base; exonerado/inafecto = valor de la línea para que Lycet genere cac:TaxTotal con tributo (evita 3105)
			PorcentajeIgv:     porcentajeIgv,
			Igv:               igv,
			TotalImpuestos:    igv,
			MtoPrecioUnitario: mtoPrecioUnitario,
		}
	}
	// Totales por tipo de operación (SUNAT exige tag del total del tributo si hay líneas con ese tipo — error 2638)
	var mtoOperGravadas, mtoOperExoneradas, mtoOperInafectas, mtoIGV float64
	for _, d := range details {
		switch d.TipAfeIgv {
		case "10":
			mtoOperGravadas += d.MtoValorVenta
			mtoIGV += d.Igv
		case "20":
			mtoOperExoneradas += d.MtoValorVenta
		case "30":
			mtoOperInafectas += d.MtoValorVenta
		default:
			// Otros códigos (ej. 40 exportación) se pueden sumar a gravadas o manejar según catálogo 07
			mtoOperGravadas += d.MtoValorVenta
			mtoIGV += d.Igv
		}
	}
	mtoOperGravadas = round2(mtoOperGravadas)
	mtoOperExoneradas = round2(mtoOperExoneradas)
	mtoOperInafectas = round2(mtoOperInafectas)
	mtoIGV = round2(mtoIGV)
	valorVenta := round2(mtoOperGravadas + mtoOperExoneradas + mtoOperInafectas)
	mtoImpVenta := round2(valorVenta + mtoIGV)
	// Total de la venta en BD es la referencia para Lycet (leyenda 1000 usa mtoImpVenta del JSON).
	if sale.Total > 0 {
		mtoImpVenta = round2(sale.Total)
	}
	tipoMoneda := sale.Currency
	if tipoMoneda == "" {
		tipoMoneda = "PEN"
	}
	var legends []facturador.InvoiceLegend
	facturador.SetSUNATLegend1000(&legends, mtoImpVenta, tipoMoneda)
	nombreComercial := companyCfg.TradeName
	if nombreComercial == "" {
		nombreComercial = companyCfg.BusinessName
	}
	// Payload según PAYLOAD-FACTURA-BOLETA.md: todos los campos obligatorios enviados a Lycet
	serie := strings.TrimSpace(sale.Series)
	if serie == "" {
		serie = "B001"
		if tipoDoc == "01" {
			serie = "F001"
		}
	}
	correlativo := strconv.FormatUint(uint64(sale.Correlative), 10)
	// fecVencimiento solo para factura (01); Lycet genera cbc:DueDate solo si se envía. Formato ISO 8601.
	var fecVencimiento string
	if tipoDoc == "01" {
		if sale.DueDate != nil {
			fecVencimiento = sale.DueDate.Format("2006-01-02")
		} else {
			fecVencimiento = sale.IssueDate.AddDate(0, 0, 8).Format("2006-01-02")
		}
	}
	payload := &facturador.InvoicePayload{
		UBLVersion:        "2.1",
		TipoOperacion:     tipoOperacion,
		TipoDoc:           tipoDoc,
		Serie:             serie,
		Correlativo:       correlativo,
		FechaEmision:      fechaEmision,
		FecVencimiento:    fecVencimiento,
		FormaPago:         &facturador.InvoiceFormaPago{Tipo: "Contado"},
		Company:           facturador.InvoiceCompany{RUC: companyCfg.RUC, RazonSocial: companyCfg.BusinessName, NombreComercial: nombreComercial, Address: companyAddr},
		Client:            facturador.InvoiceClient{TipoDoc: clientTipoDoc, NumDoc: clientNumDoc, RznSocial: clientRzn, Address: clientAddr},
		TipoMoneda:        tipoMoneda,
		MtoOperGravadas:   mtoOperGravadas,
		MtoOperExoneradas: mtoOperExoneradas,
		MtoOperInafectas:  mtoOperInafectas,
		MtoIGV:            mtoIGV,
		TotalImpuestos:    mtoIGV,
		ValorVenta:        valorVenta,
		SubTotal:          mtoImpVenta,
		MtoImpVenta:       mtoImpVenta,
		Details:           details,
		Legends:           legends,
	}
	payloadBytes, _ := json.Marshal(payload)
	payloadJSON := string(payloadBytes)
	client := facturador.NewClient(s.baseURL, s.token)
	resp, err := client.SendInvoice(payload)
	if err != nil {
		var invoice database.TenantInvoice
		s.db.Where("sale_id = ?", saleID).FirstOrCreate(&invoice, database.TenantInvoice{SaleID: saleID})
		now := time.Now()
		invoice.SentAt = &now
		invoice.RetryCount++
		invoice.SunatStatus = "error"
		invoice.SunatMessage = err.Error()
		invoice.PayloadJSON = payloadJSON
		s.db.Save(&invoice)
		s.db.Model(&sale).Update("billing_status", "error")
		return &invoice, err
	}
	// Respuesta Lycet según RESPUESTA-SUNAT-BACKEND.md: success, cdrResponse, error (conexión)
	var invoice database.TenantInvoice
	s.db.Where("sale_id = ?", saleID).FirstOrCreate(&invoice, database.TenantInvoice{SaleID: saleID})
	now := time.Now()
	invoice.SentAt = &now
	invoice.ResponseAt = &now
	invoice.RetryCount++
	invoice.PayloadJSON = payloadJSON
	invoice.SunatMessage = resp.Message()
	invoice.SunatCDRCode = resp.CDRCode()
	invoice.SunatCDRNotes = sunatNotesToJSON(resp.CDRNotes())
	invoice.SunatHash = resp.Hash
	invoice.LycetResponseJSON = lycetResponseToJSON(resp)
	if resp.Success() {
		invoice.SunatStatus = "accepted"
		s.db.Model(&sale).Update("billing_status", "accepted")
	} else {
		if resp.ConnectionError() != "" {
			invoice.SunatStatus = "error"
			invoice.SunatMessage = resp.ConnectionError()
			s.db.Model(&sale).Update("billing_status", "error")
		} else {
			invoice.SunatStatus = "rejected"
			s.db.Model(&sale).Update("billing_status", "rejected")
		}
	}
	// Guardar solo XML y CDR en disco del tenant (vienen en la respuesta de /send). El PDF no se almacena, se sirve desde Lycet.
	basePath := config.AppConfig.InvoiceStoragePath
	if basePath == "" {
		basePath = "./storage/invoices"
	}
	ruc := companyCfg.RUC
	if ruc == "" {
		ruc = "default"
	}
	provider := "lycet"
	if !s.useLycet {
		provider = "tukifac"
	}
	if resp.XML != "" {
		xmlPath, _ := saveInvoiceFile(basePath, ruc, provider, tipoDoc, serie, correlativo, "xml", []byte(resp.XML))
		invoice.XMLURL = xmlPath
	}
	if resp.CDRZipBase64() != "" {
		cdrDec, err := base64.StdEncoding.DecodeString(resp.CDRZipBase64())
		if err == nil {
			cdrPath, _ := saveInvoiceFile(basePath, ruc, provider, tipoDoc, serie, correlativo, "cdr.zip", cdrDec)
			invoice.CDRURL = cdrPath
		} else {
			invoice.CDRURL = "(CDR recibido)"
		}
	}
	s.db.Save(&invoice)
	return &invoice, nil
}

// ResendToSUNAT reenvía el comprobante regenerando el XML (y payload) desde la venta actual.
// Solo se permite cuando sunat_status es "error". Si fue rechazado por SUNAT no se debe reenviar el mismo; debe emitirse uno nuevo con correcciones.
func (s *BillingService) ResendToSUNAT(saleID uint) (*database.TenantInvoice, error) {
	var invoice database.TenantInvoice
	if err := s.db.Where("sale_id = ?", saleID).First(&invoice).Error; err != nil || invoice.ID == 0 {
		return nil, errors.New("comprobante no encontrado")
	}
	if invoice.SunatStatus == "accepted" {
		return nil, errors.New("el documento ya fue aceptado por SUNAT; no se puede reenviar")
	}
	if invoice.SunatStatus == "rejected" {
		return nil, errors.New("documento rechazado por SUNAT; debe emitir uno nuevo (corrija errores y use nuevo correlativo), no reenviar el mismo")
	}
	if strings.ToLower(strings.TrimSpace(invoice.SunatStatus)) != "error" {
		return &invoice, errors.New("solo se puede reenviar cuando el estado es error")
	}

	return s.SendToSUNAT(saleID)
}

// CreateCreditNoteAndVoidSale genera una nota de crédito para anular la venta y la envía a SUNAT; luego anula la venta original.
// La venta debe ser factura o boleta ya aceptada por SUNAT. Solo disponible con Lycet.
func (s *BillingService) CreateCreditNoteAndVoidSale(originalSaleID uint) (*database.TenantSale, *database.TenantInvoice, error) {
	if !s.useLycet {
		return nil, nil, errors.New("la anulación con nota de crédito solo está disponible con el facturador Lycet")
	}
	var cfg database.TenantCompanyConfig
	if err := s.db.First(&cfg).Error; err != nil || !cfg.SunatEnabled {
		return nil, nil, errors.New("la conexión con SUNAT no está activada")
	}
	var orig database.TenantSale
	if err := s.db.First(&orig, originalSaleID).Error; err != nil {
		return nil, nil, errors.New("venta no encontrada")
	}
	if orig.Status == "cancelled" {
		return nil, nil, errors.New("la venta ya está anulada")
	}
	if orig.DocType != "FACTURA" && orig.DocType != "BOLETA" {
		return nil, nil, errors.New("solo se puede anular con nota de crédito una factura o boleta")
	}
	if orig.BillingStatus != "accepted" {
		return nil, nil, errors.New("el comprobante debe estar aceptado por SUNAT antes de anularlo con nota de crédito")
	}
	var ncSeries database.TenantDocumentSeries
	if err := s.db.Where("branch_id = ? AND category = ? AND active = ?", orig.BranchID, "nota_credito", true).First(&ncSeries).Error; err != nil {
		return nil, nil, errors.New("no hay serie de nota de crédito configurada para esta sucursal — configure una serie con categoría Nota de crédito")
	}
	saleSvc := salesvc.NewSaleService(s.db)
	nextCorr, err := saleSvc.NextCorrelative(ncSeries.ID)
	if err != nil {
		return nil, nil, err
	}
	numberStr := fmt.Sprintf("%s-%08d", ncSeries.Series, nextCorr)
	now := time.Now()
	origIDRef := originalSaleID
	ncSale := database.TenantSale{
		BranchID:       orig.BranchID,
		ContactID:      orig.ContactID,
		UserID:         orig.UserID,
		CashSessionID:  nil,
		SeriesID:       ncSeries.ID,
		DocType:        "NOTA_CREDITO",
		Series:         ncSeries.Series,
		Correlative:    nextCorr,
		Number:         numberStr,
		IssueDate:      now,
		Subtotal:       orig.Subtotal,
		TaxAmount:      orig.TaxAmount,
		Total:          orig.Total,
		Currency:       orig.Currency,
		PaymentMethod:  orig.PaymentMethod,
		Notes:          "Anulación de la operación",
		Status:         "paid",
		BillingStatus:  "pending",
		OriginalSaleID: &origIDRef,
	}
	if err := s.db.Create(&ncSale).Error; err != nil {
		return nil, nil, fmt.Errorf("crear venta nota de crédito: %w", err)
	}
	var origItems []database.TenantSaleItem
	s.db.Where("sale_id = ?", originalSaleID).Find(&origItems)
	for _, it := range origItems {
		ncItem := database.TenantSaleItem{
			SaleID:             ncSale.ID,
			ProductID:          it.ProductID,
			Code:               it.Code,
			Description:        it.Description,
			Unit:               it.Unit,
			Quantity:           it.Quantity,
			UnitPrice:          it.UnitPrice,
			Discount:           it.Discount,
			TaxRate:            it.TaxRate,
			IgvAffectationType: it.IgvAffectationType,
			Subtotal:           it.Subtotal,
			TaxAmount:          it.TaxAmount,
			Total:              it.Total,
		}
		s.db.Create(&ncItem)
	}
	companyCfg, companyAddr, errAddr := s.getCompanyConfigAndAddress()
	if errAddr != nil {
		return nil, nil, errAddr
	}
	var contact database.TenantContact
	if orig.ContactID != nil {
		s.db.First(&contact, *orig.ContactID)
	}
	clientTipoDoc := "6"
	clientNumDoc := "00000000000"
	clientRzn := "CLIENTE VARIOS"
	clientDir := ""
	clientUbigeo := ""
	if contact.ID > 0 {
		clientRzn = contact.BusinessName
		clientNumDoc = contact.DocNumber
		clientDir = strings.TrimSpace(contact.Address)
		clientUbigeo = strings.TrimSpace(contact.Ubigeo)
		clientDir, clientUbigeo = database.NormalizeTenantContactAddressUbigeo(clientDir, clientUbigeo)
		if strings.ToUpper(contact.DocType) == "DNI" || contact.DocType == "1" {
			clientTipoDoc = "1"
		} else if len(contact.DocNumber) == 8 {
			clientTipoDoc = "1"
		} else if len(contact.DocNumber) == 11 {
			clientTipoDoc = "6"
		}
	}
	if clientNumDoc == "" {
		clientNumDoc = "00000000000"
	}
	// Cliente no documentado (Lycet schemeID="0"): tipoDoc "0", numDoc "99999999999"
	if clientNumDoc == "00000000000" || clientNumDoc == "00000000" || clientNumDoc == "99999999" ||
		(contact.ID > 0 && (strings.ToUpper(strings.TrimSpace(contact.DocType)) == "SIN DOCUMENTO" || strings.TrimSpace(contact.DocType) == "0")) {
		clientTipoDoc = "0"
		clientNumDoc = "99999999999"
	}
	var clientAddr facturador.InvoiceAddress
	if contact.ID > 0 {
		depC, provC, distC, errC := s.resolveUbigeoToAddress(clientUbigeo)
		if errC != nil {
			return nil, nil, fmt.Errorf("cliente: %w", errC)
		}
		clientAddr = facturador.InvoiceAddress{Ubigueo: clientUbigeo, CodigoPais: "PE", Departamento: depC, Provincia: provC, Distrito: distC, Urbanizacion: "", Direccion: clientDir}
	} else {
		return nil, nil, errors.New("para nota de crédito electrónica debe asignar un cliente con dirección y ubigeo en la venta original")
	}
	tipoDocAfectado := "03"
	if orig.DocType == "FACTURA" || getSeriesSunatCode(s.db, orig.SeriesID) == "01" {
		tipoDocAfectado = "01"
	}
	nroDocAfectado := fmt.Sprintf("%s-%d", orig.Series, orig.Correlative)
	// Sin fallbacks: el porcentaje IGV debe estar configurado en la empresa (Configuración → SUNAT/IGV).
	if companyCfg.TaxRate <= 0 {
		return nil, nil, fmt.Errorf("configure el porcentaje de IGV en Configuración de la empresa (SUNAT); no se usan valores por defecto")
	}
	companyTaxRate := companyCfg.TaxRate
	details := make([]facturador.InvoiceDetail, len(origItems))
	for i, it := range origItems {
		aff := strings.TrimSpace(it.IgvAffectationType)
		if aff == "" {
			return nil, nil, fmt.Errorf("el ítem «%s» de la venta original no tiene tipo de afectación IGV", strings.TrimSpace(it.Description))
		}
		if aff == "10" && it.TaxRate <= 0 {
			return nil, nil, fmt.Errorf("el ítem «%s» es gravado pero tiene porcentaje IGV en 0", strings.TrimSpace(it.Description))
		}
		mtoValorVenta := round2(it.Subtotal)
		igv := round2(it.TaxAmount)
		cantidad := it.Quantity
		if cantidad <= 0 {
			return nil, nil, fmt.Errorf("ítem «%s» con cantidad inválida", strings.TrimSpace(it.Description))
		}
		mtoValorUnitario := round2(mtoValorVenta / cantidad)
		mtoPrecioUnitario := round2((mtoValorVenta + igv) / cantidad)
		codProd := strings.TrimSpace(it.Code)
		if codProd == "" {
			return nil, nil, fmt.Errorf("el ítem «%s» no tiene código de producto", strings.TrimSpace(it.Description))
		}
		desc := strings.TrimSpace(it.Description)
		if desc == "" {
			return nil, nil, fmt.Errorf("ítem en posición %d sin descripción", i+1)
		}
		porcentajeIgvNC := round2(it.TaxRate)
		if aff != "10" {
			porcentajeIgvNC = round2(companyTaxRate)
		}
		details[i] = facturador.InvoiceDetail{
			Unidad: normUnit(it.Unit), Cantidad: cantidad, CodProducto: codProd, Descripcion: desc,
			MtoValorUnitario: mtoValorUnitario, MtoValorVenta: mtoValorVenta, TipAfeIgv: aff,
			MtoBaseIgv:    mtoValorVenta,
			PorcentajeIgv: porcentajeIgvNC, Igv: igv, TotalImpuestos: igv, MtoPrecioUnitario: mtoPrecioUnitario,
		}
	}
	var mtoOperGravadasNC, mtoOperExoneradasNC, mtoOperInafectasNC, mtoIGVNC float64
	for _, d := range details {
		switch d.TipAfeIgv {
		case "10":
			mtoOperGravadasNC += d.MtoValorVenta
			mtoIGVNC += d.Igv
		case "20":
			mtoOperExoneradasNC += d.MtoValorVenta
		case "30":
			mtoOperInafectasNC += d.MtoValorVenta
		default:
			mtoOperGravadasNC += d.MtoValorVenta
			mtoIGVNC += d.Igv
		}
	}
	mtoOperGravadasNC = round2(mtoOperGravadasNC)
	mtoOperExoneradasNC = round2(mtoOperExoneradasNC)
	mtoOperInafectasNC = round2(mtoOperInafectasNC)
	mtoIGVNC = round2(mtoIGVNC)
	valorVentaNC := round2(mtoOperGravadasNC + mtoOperExoneradasNC + mtoOperInafectasNC)
	mtoImpVentaNC := round2(valorVentaNC + mtoIGVNC)
	if orig.Total > 0 {
		mtoImpVentaNC = round2(orig.Total)
	}
	nombreComercial := companyCfg.TradeName
	if nombreComercial == "" {
		nombreComercial = companyCfg.BusinessName
	}
	tipoMoneda := orig.Currency
	if tipoMoneda == "" {
		tipoMoneda = "PEN"
	}
	notePayload := &facturador.NotePayload{
		UBLVersion:        "2.1",
		TipoDoc:           "07",
		Serie:             ncSeries.Series,
		Correlativo:       strconv.FormatUint(uint64(nextCorr), 10),
		FechaEmision:      now.Format("2006-01-02T15:04:05-07:00"),
		FormaPago:         &facturador.InvoiceFormaPago{Tipo: "Contado"},
		Company:           facturador.InvoiceCompany{RUC: companyCfg.RUC, RazonSocial: companyCfg.BusinessName, NombreComercial: nombreComercial, Address: companyAddr},
		Client:            facturador.InvoiceClient{TipoDoc: clientTipoDoc, NumDoc: clientNumDoc, RznSocial: clientRzn, Address: clientAddr},
		TipoMoneda:        tipoMoneda,
		CodMotivo:         "01",
		DesMotivo:         "Anulación de la operación",
		RelDocs:           []facturador.NoteRelDoc{{TipoDoc: tipoDocAfectado, NroDoc: nroDocAfectado}},
		MtoOperGravadas:   mtoOperGravadasNC,
		MtoOperExoneradas: mtoOperExoneradasNC,
		MtoOperInafectas:  mtoOperInafectasNC,
		MtoIGV:            mtoIGVNC,
		TotalImpuestos:    mtoIGVNC,
		ValorVenta:        valorVentaNC,
		SubTotal:          mtoImpVentaNC,
		MtoImpVenta:       mtoImpVentaNC,
		Details:           details,
	}
	var ncLegends []facturador.InvoiceLegend
	facturador.SetSUNATLegend1000(&ncLegends, mtoImpVentaNC, tipoMoneda)
	notePayload.Legends = ncLegends
	client := facturador.NewClient(s.baseURL, s.token)
	resp, err := client.SendNote(notePayload)
	notePayloadJSON := ""
	if j, _ := json.Marshal(notePayload); len(j) > 0 {
		notePayloadJSON = string(j)
	}
	var invoice database.TenantInvoice
	invoice.SaleID = ncSale.ID
	invoice.NotePayloadJSON = notePayloadJSON
	nowPt := time.Now()
	invoice.SentAt = &nowPt
	invoice.ResponseAt = &nowPt
	if err != nil {
		invoice.SunatStatus = "error"
		invoice.SunatMessage = err.Error()
		s.db.Create(&invoice)
		s.db.Model(&ncSale).Update("billing_status", "error")
		return &ncSale, &invoice, err
	}
	invoice.SunatMessage = resp.Message()
	invoice.SunatCDRCode = resp.CDRCode()
	invoice.SunatCDRNotes = sunatNotesToJSON(resp.CDRNotes())
	invoice.SunatHash = resp.Hash
	invoice.LycetResponseJSON = lycetResponseToJSON(resp)
	if resp.Success() {
		invoice.SunatStatus = "accepted"
		s.db.Model(&ncSale).Update("billing_status", "accepted")
	} else {
		if resp.ConnectionError() != "" {
			invoice.SunatStatus = "error"
			invoice.SunatMessage = resp.ConnectionError()
			s.db.Model(&ncSale).Update("billing_status", "error")
		} else {
			invoice.SunatStatus = "rejected"
			s.db.Model(&ncSale).Update("billing_status", "rejected")
		}
	}
	basePath := config.AppConfig.InvoiceStoragePath
	if basePath == "" {
		basePath = "./storage/invoices"
	}
	ruc := companyCfg.RUC
	if ruc == "" {
		ruc = "default"
	}
	provider := "lycet"
	if !s.useLycet {
		provider = "tukifac"
	}
	tipoDocNC := "07"
	if resp.XML != "" {
		xmlPath, _ := saveInvoiceFile(basePath, ruc, provider, tipoDocNC, ncSeries.Series, strconv.FormatUint(uint64(nextCorr), 10), "xml", []byte(resp.XML))
		invoice.XMLURL = xmlPath
	}
	if resp.CDRZipBase64() != "" {
		cdrDec, err := base64.StdEncoding.DecodeString(resp.CDRZipBase64())
		if err == nil {
			cdrPath, _ := saveInvoiceFile(basePath, ruc, provider, tipoDocNC, ncSeries.Series, strconv.FormatUint(uint64(nextCorr), 10), "cdr.zip", cdrDec)
			invoice.CDRURL = cdrPath
		}
	}
	if err := s.db.Create(&invoice).Error; err != nil {
		return &ncSale, nil, fmt.Errorf("guardar invoice nota de crédito: %w", err)
	}
	if !resp.Success() {
		return &ncSale, &invoice, err
	}
	if err := saleSvc.Cancel(originalSaleID); err != nil {
		return &ncSale, &invoice, fmt.Errorf("nota de crédito emitida pero error al anular la venta: %w", err)
	}
	return &ncSale, &invoice, nil
}

func getSeriesSunatCode(db *gorm.DB, seriesID uint) string {
	var ser database.TenantDocumentSeries
	if err := db.Select("sunat_code").First(&ser, seriesID).Error; err != nil {
		return ""
	}
	return ser.SunatCode
}

func normUnit(u string) string {
	if strings.TrimSpace(u) == "" {
		return "NIU"
	}
	return strings.TrimSpace(u)
}

// sunatNotesToJSON serializa las notas del CDR para guardar en BD (panel tenant).
func sunatNotesToJSON(notes []string) string {
	if len(notes) == 0 {
		return ""
	}
	b, _ := json.Marshal(notes)
	return string(b)
}

// lycetResponseToJSON guarda la respuesta completa de Lycet (xml, hash, sunatResponse) para consistencia y uso posterior (ej. QR en PDF).
func lycetResponseToJSON(resp *facturador.SunatResponse) string {
	if resp == nil {
		return ""
	}
	b, _ := json.Marshal(resp)
	return string(b)
}

// saveInvoiceFile guarda un archivo en basePath/tenants/ruc/provider/{xml|cdr|signed|pdf}/... y retorna la ruta relativa.
func saveInvoiceFile(basePath, ruc, provider, tipoDoc, serie, correlativo, ext string, content []byte) (relativePath string, err error) {
	safeRUC := tenantstorage.SanitizeRUC(ruc)
	safeProvider := tenantstorage.SanitizePathSegment(provider)
	kind := invoiceFileFolderFromExt(ext)
	dir := filepath.Join(basePath, "tenants", safeRUC, safeProvider, kind)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	name := fmt.Sprintf("%s-%s-%s-%s.%s", safeRUC, tipoDoc, serie, correlativo, ext)
	fullPath := filepath.Join(dir, name)
	if err := os.WriteFile(fullPath, content, 0644); err != nil {
		return "", err
	}
	return filepath.ToSlash(filepath.Join("tenants", safeRUC, safeProvider, kind, name)), nil
}

func invoiceFileFolderFromExt(ext string) string {
	e := strings.ToLower(strings.TrimSpace(ext))
	switch {
	case strings.HasPrefix(e, "cdr"):
		return "cdr"
	case strings.Contains(e, "signed"):
		return "signed"
	case strings.Contains(e, "pdf"):
		return "pdf"
	case strings.HasSuffix(e, "xml"):
		return "xml"
	default:
		return "misc"
	}
}

func (s *BillingService) sendToTukifac(saleID uint, companyCfg *database.TenantCompanyConfig) (*database.TenantInvoice, error) {
	if s.baseURL == "" {
		return nil, errors.New("URL de Tukifac no configurada en el servidor")
	}
	if s.token == "" {
		return nil, errors.New("token de Tukifac no configurado — agrégalo en Configuración → SUNAT")
	}
	var sale database.TenantSale
	if err := s.db.First(&sale, saleID).Error; err != nil {
		return nil, errors.New("venta no encontrada")
	}
	var items []database.TenantSaleItem
	s.db.Where("sale_id = ?", saleID).Find(&items)
	contactFound := false
	var contact database.TenantContact
	if sale.ContactID != nil {
		if err := s.db.First(&contact, *sale.ContactID).Error; err == nil {
			contactFound = true
		}
	}

	// Construir request con tipos de afectación correctos por ítem
	tukifacItems := make([]TukifacItemRequest, len(items))
	for i, item := range items {
		affType := item.IgvAffectationType
		if affType == "" {
			affType = "10" // default gravado
		}
		// ValorUnitario = precio sin IGV; PrecioUnit = precio con IGV (cuando es gravado)
		var valorUnitario, precioUnit float64
		if item.TaxRate > 0 {
			valorUnitario = item.Subtotal / item.Quantity
			precioUnit = item.Total / item.Quantity
		} else {
			valorUnitario = item.UnitPrice
			precioUnit = item.UnitPrice
		}
		tukifacItems[i] = TukifacItemRequest{
			Codigo:         item.Code,
			Descripcion:    item.Description,
			Unidad:         item.Unit,
			Cantidad:       item.Quantity,
			PrecioUnit:     precioUnit,
			ValorUnitario:  valorUnitario,
			Descuento:      item.Discount,
			SubTotal:       item.Subtotal,
			IGV:            item.TaxAmount,
			Total:          item.Total,
			TipoAfectacion: affType,
			TasaIGV:        item.TaxRate,
		}
	}

	receptorTipoDoc := "6" // RUC por defecto
	receptorNumDoc := "00000000000"
	receptorNombre := "CLIENTE VARIOS"
	receptorDir := ""

	if contactFound {
		if contact.DocType == "DNI" {
			receptorTipoDoc = "1"
		}
		receptorNumDoc = contact.DocNumber
		receptorNombre = contact.BusinessName
		receptorDir, _ = database.NormalizeTenantContactAddressUbigeo(contact.Address, contact.Ubigeo)
	}

	docType := "01" // Factura
	if sale.DocType == "BOLETA" {
		docType = "03"
	} else if sale.DocType == "NOTA_CREDITO" {
		docType = "07"
	}

	req := TukifacInvoiceRequest{
		TipoDocumento:     docType,
		Serie:             sale.Series,
		Correlativo:       fmt.Sprintf("%08d", sale.Correlative),
		FechaEmision:      sale.IssueDate.Format("2006-01-02"),
		TipoMoneda:        sale.Currency,
		EmisorRUC:         companyCfg.RUC,
		EmisorRazonSocial: companyCfg.BusinessName,
		EmisorDireccion:   companyCfg.Address,
		ReceptorTipoDoc:   receptorTipoDoc,
		ReceptorNumDoc:    receptorNumDoc,
		ReceptorNombre:    receptorNombre,
		ReceptorDireccion: receptorDir,
		SubTotal:          sale.Subtotal,
		IGV:               sale.TaxAmount,
		Total:             sale.Total,
		Items:             tukifacItems,
	}

	// Serializar y enviar
	body, _ := json.Marshal(req)
	httpReq, err := http.NewRequest("POST", s.baseURL+"/api/documents", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("error creando request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+s.token)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(httpReq)

	// Obtener o crear registro de invoice
	var invoice database.TenantInvoice
	s.db.Where("sale_id = ?", saleID).FirstOrCreate(&invoice, database.TenantInvoice{SaleID: saleID})

	now := time.Now()
	invoice.SentAt = &now
	invoice.RetryCount++

	if err != nil {
		invoice.SunatStatus = "error"
		invoice.SunatMessage = err.Error()
		s.db.Save(&invoice)
		s.db.Model(&sale).Update("billing_status", "error")
		return &invoice, fmt.Errorf("error enviando a Tukifac: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	var tukifacResp TukifacResponse
	json.Unmarshal(respBody, &tukifacResp)

	responseAt := time.Now()
	invoice.ResponseAt = &responseAt
	invoice.ExternalID = tukifacResp.ExternalID
	invoice.SunatStatus = tukifacResp.Estado
	invoice.SunatMessage = tukifacResp.Mensaje
	invoice.XMLURL = tukifacResp.XMLURL
	invoice.CDRURL = tukifacResp.CDRURL
	invoice.PDFURL = tukifacResp.PDFURL

	if tukifacResp.Success {
		s.db.Model(&sale).Update("billing_status", "accepted")
	} else {
		s.db.Model(&sale).Update("billing_status", "rejected")
	}

	s.db.Save(&invoice)
	return &invoice, nil
}

func (s *BillingService) GetInvoice(saleID uint) (*database.TenantInvoice, error) {
	var invoice database.TenantInvoice
	err := s.db.Where("sale_id = ?", saleID).First(&invoice).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &invoice, err
}

// GetInvoiceDocumentPath devuelve la ruta absoluta del archivo XML, CDR o PDF del comprobante para permitir su descarga.
func (s *BillingService) GetInvoiceDocumentPath(saleID uint, kind string) (string, error) {
	invoice, err := s.GetInvoice(saleID)
	if err != nil || invoice == nil {
		return "", err
	}
	basePath := config.AppConfig.InvoiceStoragePath
	if basePath == "" {
		basePath = "./storage/invoices"
	}
	var rel string
	switch kind {
	case "xml":
		rel = invoice.XMLURL
	case "cdr":
		rel = invoice.CDRURL
	case "pdf":
		rel = invoice.PDFURL
	default:
		return "", fmt.Errorf("tipo de documento no válido: %s", kind)
	}
	if (rel == "" || rel == "(CDR recibido)") && kind == "cdr" && s.orchestrator != nil {
		if invCfg, cfgErr := NewInvoicingConfigService(s.db).GetConfig(); cfgErr == nil && invCfg != nil && invCfg.Mode == InvoicingModePSE {
			_, _ = s.orchestrator.CheckStatus(saleID)
			if updated, uerr := s.GetInvoice(saleID); uerr == nil && updated != nil {
				rel = updated.CDRURL
			}
		}
	}
	if rel == "" || rel == "(CDR recibido)" {
		return "", nil
	}

	fullPath := filepath.Join(basePath, filepath.FromSlash(rel))
	if _, statErr := os.Stat(fullPath); statErr == nil {
		return fullPath, nil
	}
	if basePath == "./storage/invoices" {
		legacyPath := filepath.Join("./storage", filepath.FromSlash(rel))
		if _, statErr := os.Stat(legacyPath); statErr == nil {
			return legacyPath, nil
		}
	}
	return fullPath, nil
}

// GetInvoiceDocumentFilename devuelve el nombre de archivo según formato SUNAT (ej. 03-B001-26.xml, 03-B001-26.cdr.zip, 03-B001-26.pdf).
// Las rutas en BD son del tipo ruc/tipoDoc-serie-correlativo.ext; se devuelve solo el nombre del archivo para Content-Disposition.
func (s *BillingService) GetInvoiceDocumentFilename(saleID uint, kind string) (string, error) {
	invoice, err := s.GetInvoice(saleID)
	if err != nil || invoice == nil {
		return "", err
	}
	switch kind {
	case "xml":
		if invoice.XMLURL != "" && invoice.XMLURL != "(CDR recibido)" {
			return filepath.Base(filepath.FromSlash(invoice.XMLURL)), nil
		}
	case "cdr":
		if invoice.CDRURL != "" && invoice.CDRURL != "(CDR recibido)" {
			return filepath.Base(filepath.FromSlash(invoice.CDRURL)), nil
		}
	case "pdf", "xml-generated":
		if invoice.PayloadJSON != "" {
			var payload facturador.InvoicePayload
			if err := json.Unmarshal([]byte(invoice.PayloadJSON), &payload); err == nil {
				base := fmt.Sprintf("%s-%s-%s", payload.TipoDoc, payload.Serie, payload.Correlativo)
				if kind == "pdf" {
					return base + ".pdf", nil
				}
				return base + "-generado.xml", nil
			}
		}
		if invoice.NotePayloadJSON != "" {
			var note facturador.NotePayload
			if err := json.Unmarshal([]byte(invoice.NotePayloadJSON), &note); err == nil {
				base := fmt.Sprintf("%s-%s-%s", note.TipoDoc, note.Serie, note.Correlativo)
				if kind == "pdf" {
					return base + ".pdf", nil
				}
				return base + "-generado.xml", nil
			}
		}
	}
	// Fallback
	switch kind {
	case "pdf":
		return "comprobante.pdf", nil
	case "xml":
		return "comprobante-enviado.xml", nil
	case "xml-generated":
		return "comprobante-generado.xml", nil
	case "cdr":
		return "cdr.zip", nil
	default:
		return "comprobante", nil
	}
}

// GetInvoicePDFContent devuelve el PDF del comprobante. Para Lycet siempre se obtiene de Lycet (POST /invoice/pdf o /note/pdf);
// el XML y CDR ya vienen en la respuesta de /send. Solo el PDF se pide a Lycet al descargar/visualizar.
func (s *BillingService) GetInvoicePDFContent(saleID uint) ([]byte, error) {
	invoice, err := s.GetInvoice(saleID)
	if err != nil || invoice == nil {
		return nil, err
	}
	var sale database.TenantSale
	if s.db.Select("doc_type").First(&sale, saleID).Error != nil {
		return nil, errors.New("venta no encontrada")
	}
	if sale.DocType == "NOTA_CREDITO" && invoice.NotePayloadJSON != "" && s.useLycet {
		var payload facturador.NotePayload
		if err := json.Unmarshal([]byte(invoice.NotePayloadJSON), &payload); err != nil {
			return nil, fmt.Errorf("payload nota inválido: %w", err)
		}
		client := facturador.NewClient(s.baseURL, s.token)
		return client.GetNotePDF(&payload)
	}
	// Para Lycet: siempre obtener PDF del endpoint de Lycet (no generar en este backend).
	if s.useLycet && invoice.PayloadJSON != "" {
		var payload facturador.InvoicePayload
		if err := json.Unmarshal([]byte(invoice.PayloadJSON), &payload); err != nil {
			return nil, fmt.Errorf("payload inválido: %w", err)
		}
		client := facturador.NewClient(s.baseURL, s.token)
		pdfBytes, err := client.GetInvoicePDF(&payload)
		if err != nil {
			return nil, err
		}
		if len(pdfBytes) > 0 {
			return pdfBytes, nil
		}
	}
	// No Lycet (ej. Tukifac) o sin payload: intentar desde disco si existe.
	basePath := config.AppConfig.InvoiceStoragePath
	if basePath == "" {
		basePath = "./storage/invoices"
	}
	if invoice.PDFURL != "" && invoice.PDFURL != "(CDR recibido)" {
		fullPath := filepath.Join(basePath, filepath.FromSlash(invoice.PDFURL))
		if data, err := os.ReadFile(fullPath); err == nil && len(data) > 0 {
			return data, nil
		}
	}
	return nil, nil
}

// GetInvoiceXMLGeneratedContent devuelve el XML firmado generado sin envío a SUNAT (POST /invoice/xml o /note/xml de Lycet).
// No se almacena; se obtiene bajo demanda desde el endpoint de Lycet.
func (s *BillingService) GetInvoiceXMLGeneratedContent(saleID uint) ([]byte, error) {
	invoice, err := s.GetInvoice(saleID)
	if err != nil || invoice == nil {
		return nil, nil
	}
	var sale database.TenantSale
	if s.db.Select("doc_type").First(&sale, saleID).Error == nil && sale.DocType == "NOTA_CREDITO" && invoice.NotePayloadJSON != "" && s.useLycet {
		var payload facturador.NotePayload
		if err := json.Unmarshal([]byte(invoice.NotePayloadJSON), &payload); err != nil {
			return nil, fmt.Errorf("payload nota inválido: %w", err)
		}
		client := facturador.NewClient(s.baseURL, s.token)
		return client.GetNoteXML(&payload)
	}
	if !s.useLycet || invoice.PayloadJSON == "" {
		return nil, nil
	}
	var payload facturador.InvoicePayload
	if err := json.Unmarshal([]byte(invoice.PayloadJSON), &payload); err != nil {
		return nil, fmt.Errorf("payload inválido: %w", err)
	}
	client := facturador.NewClient(s.baseURL, s.token)
	return client.GetInvoiceXML(&payload)
}

type InvoiceListParams struct {
	Status   string
	DateFrom *time.Time
	DateTo   *time.Time
}

func (s *BillingService) ListInvoices(params InvoiceListParams) ([]database.TenantInvoice, error) {
	var invoices []database.TenantInvoice
	q := s.db.Model(&database.TenantInvoice{})
	if params.Status != "" {
		q = q.Where("sunat_status = ?", params.Status)
	}
	if params.DateFrom != nil {
		q = q.Where("sent_at >= ?", params.DateFrom)
	}
	if params.DateTo != nil {
		q = q.Where("sent_at <= ?", params.DateTo)
	}
	err := q.Order("created_at DESC").Find(&invoices).Error
	return invoices, err
}

// --- Resumen diario y Comunicación de baja ---

func (s *BillingService) ListSummaries() ([]database.TenantSunatSummary, error) {
	var list []database.TenantSunatSummary
	err := s.db.Order("fec_resumen DESC, created_at DESC").Find(&list).Error
	return list, err
}

// getCompanyConfigAndAddress obtiene la configuración de la empresa y la dirección para payloads SUNAT (resumen, voided).
// No usa fallbacks "-": SUNAT exige dirección completa y nombres reales de departamento/provincia/distrito.
func (s *BillingService) getCompanyConfigAndAddress() (*database.TenantCompanyConfig, facturador.InvoiceAddress, error) {
	var cfg database.TenantCompanyConfig
	if err := s.db.Select("id", "ruc", "business_name", "trade_name", "address", "ubigeo").First(&cfg).Error; err != nil {
		return nil, facturador.InvoiceAddress{}, err
	}
	ubigueo := strings.TrimSpace(cfg.Ubigeo)
	if ubigueo == "" {
		return nil, facturador.InvoiceAddress{}, fmt.Errorf("configure el ubigeo del domicilio fiscal en Configuración → Empresa / SUNAT")
	}
	dep, prov, dist, err := s.resolveUbigeoToAddress(ubigueo)
	if err != nil {
		return nil, facturador.InvoiceAddress{}, err
	}
	direccion := strings.TrimSpace(cfg.Address)
	if direccion == "" {
		return nil, facturador.InvoiceAddress{}, fmt.Errorf("configure la dirección completa del domicilio fiscal en Configuración → Empresa")
	}
	addr := facturador.InvoiceAddress{
		Ubigueo:      ubigueo,
		CodigoPais:   "PE",
		Departamento: dep,
		Provincia:    prov,
		Distrito:     dist,
		Urbanizacion: "",
		Direccion:    direccion,
	}
	return &cfg, addr, nil
}

// CreateSummary genera y envía el resumen diario a SUNAT para la fecha indicada. Solo incluye ventas con billing_status = accepted.
// Devuelve el registro guardado con ticket; el estado se consulta con GetSummaryStatus.
func (s *BillingService) CreateSummary(fecResumen time.Time) (*database.TenantSunatSummary, error) {
	if !s.useLycet {
		return nil, errors.New("resumen diario solo disponible con facturador Lycet")
	}
	companyCfg, companyAddr, err := s.getCompanyConfigAndAddress()
	if err != nil {
		return nil, fmt.Errorf("configuración de empresa: %w", err)
	}
	nombreComercial := companyCfg.TradeName
	if nombreComercial == "" {
		nombreComercial = companyCfg.BusinessName
	}

	// Ventas del día con estado aceptado por SUNAT (facturas/boletas enviadas y aceptadas)
	dayStart := time.Date(fecResumen.Year(), fecResumen.Month(), fecResumen.Day(), 0, 0, 0, 0, fecResumen.Location())
	dayEnd := dayStart.Add(24 * time.Hour)
	var sales []database.TenantSale
	err = s.db.Where("issue_date >= ? AND issue_date < ? AND billing_status = ?",
		dayStart, dayEnd, "accepted").Find(&sales).Error
	if err != nil {
		return nil, err
	}

	// Correlativo del resumen: secuencia por fecha (RC-YYYYMMDD-NNN o simple número)
	var count int64
	s.db.Model(&database.TenantSunatSummary{}).Where("fec_resumen = ?", dayStart).Count(&count)
	correlativo := strconv.FormatInt(count+1, 10)

	details := make([]facturador.SummaryDetail, 0, len(sales))
	for _, sale := range sales {
		serieDoc := getSeriesSunatCode(s.db, sale.SeriesID)
		if serieDoc == "00" {
			continue
		}
		serieNro := sale.Series + "-" + sale.Number
		clienteTipo := "1"
		clienteNro := "00000000"
		if sale.ContactID != nil {
			var c database.TenantContact
			if s.db.Select("doc_type", "doc_number").First(&c, *sale.ContactID).Error == nil {
				if c.DocType == "RUC" || c.DocType == "6" {
					clienteTipo = "6"
				}
				clienteNro = c.DocNumber
			}
		}
		details = append(details, facturador.SummaryDetail{
			TipoDoc:         serieDoc,
			SerieNro:        serieNro,
			ClienteTipo:     clienteTipo,
			ClienteNro:      clienteNro,
			Total:           sale.Total,
			MtoOperGravadas: sale.Subtotal,
			MtoIGV:          sale.TaxAmount,
		})
	}

	now := time.Now()
	payload := &facturador.SummaryPayload{
		Company:       facturador.InvoiceCompany{RUC: companyCfg.RUC, RazonSocial: companyCfg.BusinessName, NombreComercial: nombreComercial, Address: companyAddr},
		Correlativo:   correlativo,
		FecGeneracion: now.Format(time.RFC3339),
		FecResumen:    dayStart.Format("2006-01-02T15:04:05-07:00"),
		Moneda:        "PEN",
		Details:       details,
	}
	payloadJSON, _ := json.Marshal(payload)

	client := facturador.NewClient(s.baseURL, s.token)
	resp, err := client.SendSummary(payload)
	if err != nil {
		return nil, err
	}

	ticket := resp.Ticket()
	rec := &database.TenantSunatSummary{
		FecResumen:   dayStart,
		Correlativo:  correlativo,
		Ticket:       ticket,
		Status:       "pending",
		PayloadJSON:  string(payloadJSON),
		DetailsCount: len(details),
	}
	if err := s.db.Create(rec).Error; err != nil {
		return nil, err
	}
	return rec, nil
}

// GetSummaryStatus consulta en SUNAT el estado del ticket del resumen; si hay CDR, lo guarda en disco y actualiza el registro.
func (s *BillingService) GetSummaryStatus(id uint) (*database.TenantSunatSummary, error) {
	var rec database.TenantSunatSummary
	if err := s.db.First(&rec, id).Error; err != nil {
		return nil, err
	}
	if rec.Ticket == "" {
		return &rec, nil
	}
	var cfg database.TenantCompanyConfig
	if s.db.Select("ruc").First(&cfg).Error != nil {
		return &rec, nil
	}
	client := facturador.NewClient(s.baseURL, s.token)
	result, err := client.GetSummaryStatus(rec.Ticket, cfg.RUC)
	if err != nil {
		return &rec, err
	}
	if result.Success && result.CDRZip != "" {
		basePath := config.AppConfig.InvoiceStoragePath
		if basePath == "" {
			basePath = "./storage/invoices"
		}
		cdrDec, decErr := base64.StdEncoding.DecodeString(result.CDRZip)
		if decErr == nil {
			ruc := cfg.RUC
			if ruc == "" {
				ruc = "default"
			}
			cdrPath, _ := saveInvoiceFile(basePath, ruc, "lycet", "RC", "RC", rec.Correlativo+"-"+rec.FecResumen.Format("20060102"), "cdr.zip", cdrDec)
			rec.CDRURL = cdrPath
		}
		if result.CDRResponse != nil {
			rec.SunatCode = result.CDRResponse.Code
			rec.SunatMessage = result.CDRResponse.Description
			if result.CDRResponse.Accepted {
				rec.Status = "accepted"
			} else {
				rec.Status = "rejected"
			}
		} else {
			rec.Status = "accepted"
		}
	} else {
		if result.Error != nil {
			rec.SunatMessage = result.Error.Message
			rec.SunatCode = result.Error.Code
			rec.Status = "error"
		}
	}
	s.db.Save(&rec)
	return &rec, nil
}

// CreateVoidedInput es un comprobante a dar de baja para una comunicación de baja.
type CreateVoidedInput struct {
	TipoDoc       string `json:"tipo_doc"` // 01, 03, 07, 08
	Serie         string `json:"serie"`
	Correlativo   string `json:"correlativo"`
	DesMotivoBaja string `json:"des_motivo_baja"`
}

// ListVoided lista las comunicaciones de baja enviadas.
func (s *BillingService) ListVoided() ([]database.TenantSunatVoided, error) {
	var list []database.TenantSunatVoided
	err := s.db.Order("fec_comunicacion DESC, created_at DESC").Find(&list).Error
	return list, err
}

// CreateVoided envía una comunicación de baja a SUNAT. Si la respuesta trae ticket, guarda pendiente; si trae CDR directo, guarda estado y CDR.
func (s *BillingService) CreateVoided(details []CreateVoidedInput) (*database.TenantSunatVoided, error) {
	if !s.useLycet {
		return nil, errors.New("comunicación de baja solo disponible con facturador Lycet")
	}
	if len(details) == 0 {
		return nil, errors.New("se requiere al menos un comprobante para dar de baja")
	}
	companyCfg, companyAddr, err := s.getCompanyConfigAndAddress()
	if err != nil {
		return nil, fmt.Errorf("configuración de empresa: %w", err)
	}
	nombreComercial := companyCfg.TradeName
	if nombreComercial == "" {
		nombreComercial = companyCfg.BusinessName
	}

	now := time.Now()
	var count int64
	s.db.Model(&database.TenantSunatVoided{}).Where("DATE(fec_comunicacion) = ?", now.Format("2006-01-02")).Count(&count)
	correlativo := strconv.FormatInt(count+1, 10)

	voidedDetails := make([]facturador.VoidedDetail, len(details))
	for i, d := range details {
		voidedDetails[i] = facturador.VoidedDetail{
			TipoDoc:       d.TipoDoc,
			Serie:         d.Serie,
			Correlativo:   d.Correlativo,
			DesMotivoBaja: d.DesMotivoBaja,
		}
	}
	payload := &facturador.VoidedPayload{
		Company:         facturador.InvoiceCompany{RUC: companyCfg.RUC, RazonSocial: companyCfg.BusinessName, NombreComercial: nombreComercial, Address: companyAddr},
		Correlativo:     correlativo,
		FecGeneracion:   now.Format(time.RFC3339),
		FecComunicacion: now.Format(time.RFC3339),
		Details:         voidedDetails,
	}
	payloadJSON, _ := json.Marshal(payload)

	client := facturador.NewClient(s.baseURL, s.token)
	resp, err := client.SendVoided(payload)
	if err != nil {
		return nil, err
	}

	rec := &database.TenantSunatVoided{
		FecComunicacion: now,
		Correlativo:     correlativo,
		Ticket:          resp.Ticket(),
		Status:          "pending",
		PayloadJSON:     string(payloadJSON),
		DetailsCount:    len(details),
	}

	if resp.CDRZipBase64() != "" {
		rec.Status = "accepted"
		if resp.SunatResponse != nil && resp.SunatResponse.CDRResponse != nil {
			rec.SunatCode = resp.SunatResponse.CDRResponse.Code
			rec.SunatMessage = resp.SunatResponse.CDRResponse.Description
			if !resp.SunatResponse.CDRResponse.Accepted {
				rec.Status = "rejected"
			}
		}
		basePath := config.AppConfig.InvoiceStoragePath
		if basePath == "" {
			basePath = "./storage/invoices"
		}
		ruc := companyCfg.RUC
		if ruc == "" {
			ruc = "default"
		}
		cdrDec, decErr := base64.StdEncoding.DecodeString(resp.CDRZipBase64())
		if decErr == nil {
			cdrPath, _ := saveInvoiceFile(basePath, ruc, "lycet", "RA", "RA", correlativo+"-"+now.Format("20060102"), "cdr.zip", cdrDec)
			rec.CDRURL = cdrPath
		}
	} else if resp.Message() != "" {
		rec.SunatMessage = resp.Message()
		rec.SunatCode = resp.CDRCode()
		if resp.Success() {
			rec.Status = "accepted"
		} else {
			rec.Status = "rejected"
		}
	}

	if err := s.db.Create(rec).Error; err != nil {
		return nil, err
	}
	return rec, nil
}

// GetVoidedStatus consulta el estado del ticket de la comunicación de baja; si hay CDR, lo guarda y actualiza el registro.
func (s *BillingService) GetVoidedStatus(id uint) (*database.TenantSunatVoided, error) {
	var rec database.TenantSunatVoided
	if err := s.db.First(&rec, id).Error; err != nil {
		return nil, err
	}
	if rec.Ticket == "" {
		return &rec, nil
	}
	var cfg database.TenantCompanyConfig
	if s.db.Select("ruc").First(&cfg).Error != nil {
		return &rec, nil
	}
	client := facturador.NewClient(s.baseURL, s.token)
	result, err := client.GetVoidedStatus(rec.Ticket, cfg.RUC)
	if err != nil {
		return &rec, err
	}
	if result.Success && result.CDRZip != "" {
		basePath := config.AppConfig.InvoiceStoragePath
		if basePath == "" {
			basePath = "./storage/invoices"
		}
		cdrDec, decErr := base64.StdEncoding.DecodeString(result.CDRZip)
		if decErr == nil {
			ruc := cfg.RUC
			if ruc == "" {
				ruc = "default"
			}
			cdrPath, _ := saveInvoiceFile(basePath, ruc, "lycet", "RA", "RA", rec.Correlativo+"-"+rec.FecComunicacion.Format("20060102"), "cdr.zip", cdrDec)
			rec.CDRURL = cdrPath
		}
		if result.CDRResponse != nil {
			rec.SunatCode = result.CDRResponse.Code
			rec.SunatMessage = result.CDRResponse.Description
			if result.CDRResponse.Accepted {
				rec.Status = "accepted"
			} else {
				rec.Status = "rejected"
			}
		} else {
			rec.Status = "accepted"
		}
	} else {
		if result.Error != nil {
			rec.SunatMessage = result.Error.Message
			rec.SunatCode = result.Error.Code
			rec.Status = "error"
		}
	}
	s.db.Save(&rec)
	return &rec, nil
}

// ConsultInvoiceStatus consulta en SUNAT el estado/CDR de un comprobante (GET /invoice/status). Según CONSULTA-COMPROBANTE-CDR.md.
func (s *BillingService) ConsultInvoiceStatus(tipo, serie, numero string) (*facturador.StatusResult, error) {
	if !s.useLycet {
		return nil, errors.New("consulta de comprobante solo disponible con facturador Lycet")
	}
	var cfg database.TenantCompanyConfig
	if s.db.Select("ruc").First(&cfg).Error != nil {
		return nil, errors.New("no hay configuración de empresa")
	}
	client := facturador.NewClient(s.baseURL, s.token)
	return client.GetInvoiceStatus(tipo, serie, numero, cfg.RUC)
}

// --- Guías de remisión (Despatch) ---

// CreateDespatchInput entrada para crear y enviar una guía de remisión.
type CreateDespatchInput struct {
	BranchID     uint                      `json:"branch_id"`
	SeriesID     uint                      `json:"series_id"`
	Destinatario DespatchDestinatarioInput `json:"destinatario"`
	Envio        DespatchEnvioInput        `json:"envio"`
	Details      []DespatchDetailInput     `json:"details"`
}

type DespatchDestinatarioInput struct {
	TipoDoc   string `json:"tipo_doc"`
	NumDoc    string `json:"num_doc"`
	RznSocial string `json:"rzn_social"`
	Address   string `json:"address"`
	Ubigeo    string `json:"ubigeo"`
}

type DespatchEnvioInput struct {
	CodTraslado        string  `json:"cod_traslado"`
	DesTraslado        string  `json:"des_traslado"`
	ModTraslado        string  `json:"mod_traslado"`
	FecTraslado        string  `json:"fec_traslado"`
	PartidaUbigueo     string  `json:"partida_ubigueo"`
	PartidaDireccion   string  `json:"partida_direccion"`
	LlegadaUbigueo     string  `json:"llegada_ubigueo"`
	LlegadaDireccion   string  `json:"llegada_direccion"`
	PesoTotal          float64 `json:"peso_total"`
	UndPesoTotal       string  `json:"und_peso_total"`
	NumBultos          int     `json:"num_bultos"`
	TransportistaRUC   string  `json:"transportista_ruc,omitempty"`
	TransportistaRazon string  `json:"transportista_razon,omitempty"`
	TransportistaPlaca string  `json:"transportista_placa,omitempty"`
	ChoferTipoDoc      string  `json:"chofer_tipo_doc,omitempty"`
	ChoferDoc          string  `json:"chofer_doc,omitempty"`
}

type DespatchDetailInput struct {
	Codigo      string  `json:"codigo"`
	Descripcion string  `json:"descripcion"`
	Unidad      string  `json:"unidad"`
	Cantidad    float64 `json:"cantidad"`
}

func (s *BillingService) ListDespatches() ([]database.TenantDespatch, error) {
	var list []database.TenantDespatch
	err := s.db.Order("issue_date DESC, created_at DESC").Find(&list).Error
	return list, err
}

func (s *BillingService) CreateAndSendDespatch(input CreateDespatchInput) (*database.TenantDespatch, error) {
	if !s.useLycet {
		return nil, errors.New("guías de remisión solo disponibles con facturador Lycet")
	}
	companyCfg, companyAddr, err := s.getCompanyConfigAndAddress()
	if err != nil {
		return nil, err
	}
	nombreComercial := companyCfg.TradeName
	if nombreComercial == "" {
		nombreComercial = companyCfg.BusinessName
	}
	var series database.TenantDocumentSeries
	if err := s.db.First(&series, input.SeriesID).Error; err != nil {
		return nil, fmt.Errorf("serie no encontrada: %w", err)
	}
	if series.SunatCode != "09" {
		return nil, errors.New("la serie debe ser de tipo guía de remisión (09)")
	}
	nextCorr := series.Correlative
	if err := s.db.Model(&series).Update("correlative", nextCorr+1).Error; err != nil {
		return nil, err
	}
	correlativoStr := strconv.FormatUint(uint64(nextCorr), 10)
	now := time.Now()
	fechaEmision := now.Format(time.RFC3339)
	if input.Envio.FecTraslado != "" {
		fechaEmision = input.Envio.FecTraslado
	}
	partidaUbi := strings.TrimSpace(input.Envio.PartidaUbigueo)
	if partidaUbi == "" {
		partidaUbi = strings.TrimSpace(companyCfg.Ubigeo)
	}
	if partidaUbi == "" {
		return nil, fmt.Errorf("ubigeo de partida es obligatorio (o configure ubigeo de empresa)")
	}
	llegadaUbi := strings.TrimSpace(input.Envio.LlegadaUbigueo)
	if llegadaUbi == "" {
		return nil, fmt.Errorf("ubigeo de llegada es obligatorio")
	}
	destAddr, errDest := s.buildInvoiceAddressFromUbigeo(input.Destinatario.Ubigeo, input.Destinatario.Address)
	if errDest != nil {
		return nil, fmt.Errorf("destinatario: %w", errDest)
	}
	partidaDir := strings.TrimSpace(input.Envio.PartidaDireccion)
	if partidaDir == "" {
		partidaDir = strings.TrimSpace(companyCfg.Address)
	}
	if partidaDir == "" {
		return nil, fmt.Errorf("dirección de partida es obligatoria")
	}
	depP, provP, distP, errP := s.resolveUbigeoToAddress(partidaUbi)
	if errP != nil {
		return nil, fmt.Errorf("partida: %w", errP)
	}
	partida := facturador.DespatchDirection{Ubigueo: partidaUbi, CodigoPais: "PE", Departamento: depP, Provincia: provP, Distrito: distP, Direccion: partidaDir}
	llegadaDir := strings.TrimSpace(input.Envio.LlegadaDireccion)
	if llegadaDir == "" {
		llegadaDir = strings.TrimSpace(input.Destinatario.Address)
	}
	if llegadaDir == "" {
		return nil, fmt.Errorf("dirección de llegada es obligatoria")
	}
	depL, provL, distL, errL := s.resolveUbigeoToAddress(llegadaUbi)
	if errL != nil {
		return nil, fmt.Errorf("llegada: %w", errL)
	}
	llegada := facturador.DespatchDirection{Ubigueo: llegadaUbi, CodigoPais: "PE", Departamento: depL, Provincia: provL, Distrito: distL, Direccion: llegadaDir}
	shipment := facturador.DespatchShipment{
		CodTraslado:  input.Envio.CodTraslado,
		DesTraslado:  input.Envio.DesTraslado,
		ModTraslado:  input.Envio.ModTraslado,
		FecTraslado:  input.Envio.FecTraslado,
		Partida:      partida,
		Llegada:      llegada,
		PesoTotal:    input.Envio.PesoTotal,
		UndPesoTotal: input.Envio.UndPesoTotal,
		NumBultos:    input.Envio.NumBultos,
	}
	if input.Envio.UndPesoTotal == "" {
		shipment.UndPesoTotal = "KGM"
	}
	if input.Envio.TransportistaRUC != "" {
		shipment.Transportista = &facturador.DespatchTransportist{
			TipoDoc:       "6",
			NumDoc:        input.Envio.TransportistaRUC,
			RznSocial:     input.Envio.TransportistaRazon,
			Placa:         input.Envio.TransportistaPlaca,
			ChoferTipoDoc: input.Envio.ChoferTipoDoc,
			ChoferDoc:     input.Envio.ChoferDoc,
		}
		if shipment.Transportista.ChoferTipoDoc == "" {
			shipment.Transportista.ChoferTipoDoc = "1"
		}
	}
	details := make([]facturador.DespatchDetail, len(input.Details))
	for i, d := range input.Details {
		details[i] = facturador.DespatchDetail{Codigo: d.Codigo, Descripcion: d.Descripcion, Unidad: d.Unidad, Cantidad: d.Cantidad}
		if details[i].Unidad == "" {
			details[i].Unidad = "NIU"
		}
	}
	payload := &facturador.DespatchPayload{
		Version:      "2020",
		TipoDoc:      "09",
		Serie:        series.Series,
		Correlativo:  correlativoStr,
		FechaEmision: fechaEmision,
		Company:      facturador.InvoiceCompany{RUC: companyCfg.RUC, RazonSocial: companyCfg.BusinessName, NombreComercial: nombreComercial, Address: companyAddr},
		Destinatario: facturador.InvoiceClient{TipoDoc: input.Destinatario.TipoDoc, NumDoc: input.Destinatario.NumDoc, RznSocial: input.Destinatario.RznSocial, Address: destAddr},
		Envio:        shipment,
		Details:      details,
	}
	payloadJSON, _ := json.Marshal(payload)
	client := facturador.NewClient(s.baseURL, s.token)
	resp, err := client.SendDespatch(payload)
	if err != nil {
		return nil, err
	}
	rec := &database.TenantDespatch{
		BranchID:          input.BranchID,
		SeriesID:          input.SeriesID,
		Series:            series.Series,
		Correlative:       nextCorr,
		IssueDate:         now,
		DestinatarioRUC:   input.Destinatario.NumDoc,
		DestinatarioRazon: input.Destinatario.RznSocial,
		Ticket:            resp.Ticket(),
		Status:            "pending",
		PayloadJSON:       string(payloadJSON),
		DetailsCount:      len(details),
	}
	if resp.CDRZipBase64() != "" {
		rec.Status = "accepted"
		if resp.SunatResponse != nil && resp.SunatResponse.CDRResponse != nil {
			rec.SunatCode = resp.SunatResponse.CDRResponse.Code
			rec.SunatMessage = resp.SunatResponse.CDRResponse.Description
			if !resp.SunatResponse.CDRResponse.Accepted {
				rec.Status = "rejected"
			}
		}
		basePath := config.AppConfig.InvoiceStoragePath
		if basePath == "" {
			basePath = "./storage/invoices"
		}
		ruc := companyCfg.RUC
		if ruc == "" {
			ruc = "default"
		}
		cdrDec, _ := base64.StdEncoding.DecodeString(resp.CDRZipBase64())
		if len(cdrDec) > 0 {
			cdrPath, _ := saveInvoiceFile(basePath, ruc, "lycet", "09", series.Series, correlativoStr, "cdr.zip", cdrDec)
			rec.CDRURL = cdrPath
		}
	} else if resp.Message() != "" {
		rec.SunatMessage = resp.Message()
		rec.SunatCode = resp.CDRCode()
		if resp.Success() {
			rec.Status = "accepted"
		} else {
			rec.Status = "rejected"
		}
	}
	if err := s.db.Create(rec).Error; err != nil {
		return nil, err
	}
	return rec, nil
}

func (s *BillingService) GetDespatchStatus(id uint) (*database.TenantDespatch, error) {
	var rec database.TenantDespatch
	if err := s.db.First(&rec, id).Error; err != nil {
		return nil, err
	}
	if rec.Ticket == "" {
		return &rec, nil
	}
	var cfg database.TenantCompanyConfig
	if s.db.Select("ruc").First(&cfg).Error != nil {
		return &rec, nil
	}
	client := facturador.NewClient(s.baseURL, s.token)
	result, err := client.GetDespatchStatus(rec.Ticket, cfg.RUC)
	if err != nil {
		return &rec, err
	}
	if result.Success && result.CDRZip != "" {
		basePath := config.AppConfig.InvoiceStoragePath
		if basePath == "" {
			basePath = "./storage/invoices"
		}
		cdrDec, _ := base64.StdEncoding.DecodeString(result.CDRZip)
		if len(cdrDec) > 0 {
			ruc := cfg.RUC
			if ruc == "" {
				ruc = "default"
			}
			cdrPath, _ := saveInvoiceFile(basePath, ruc, "lycet", "09", rec.Series, strconv.FormatUint(uint64(rec.Correlative), 10), "cdr.zip", cdrDec)
			rec.CDRURL = cdrPath
		}
		if result.CDRResponse != nil {
			rec.SunatCode = result.CDRResponse.Code
			rec.SunatMessage = result.CDRResponse.Description
			if result.CDRResponse.Accepted {
				rec.Status = "accepted"
			} else {
				rec.Status = "rejected"
			}
		} else {
			rec.Status = "accepted"
		}
	} else if result.Error != nil {
		rec.SunatMessage = result.Error.Message
		rec.SunatCode = result.Error.Code
		rec.Status = "error"
	}
	s.db.Save(&rec)
	return &rec, nil
}

// --- Retención ---

func (s *BillingService) ListRetentions() ([]database.TenantRetention, error) {
	var list []database.TenantRetention
	err := s.db.Order("fecha_emision DESC, created_at DESC").Find(&list).Error
	return list, err
}

type CreateRetentionInput struct {
	Series       string                  `json:"series"`
	Correlativo  string                  `json:"correlativo"`
	FechaEmision string                  `json:"fecha_emision"`
	Proveedor    RetentionProveedorInput `json:"proveedor"`
	Regimen      string                  `json:"regimen"`
	Tasa         float64                 `json:"tasa"`
	ImpRetenido  float64                 `json:"imp_retenido"`
	ImpPagado    float64                 `json:"imp_pagado"`
	Details      []RetentionDetailInput  `json:"details"`
}

type RetentionProveedorInput struct {
	TipoDoc   string `json:"tipo_doc"`
	NumDoc    string `json:"num_doc"`
	RznSocial string `json:"rzn_social"`
	Address   string `json:"address"`
	Ubigeo    string `json:"ubigeo"`
}

type RetentionDetailInput struct {
	TipoDoc        string  `json:"tipo_doc"`
	NumDoc         string  `json:"num_doc"`
	FechaEmision   string  `json:"fecha_emision"`
	ImpTotal       float64 `json:"imp_total"`
	Moneda         string  `json:"moneda"`
	FechaRetencion string  `json:"fecha_retencion"`
	ImpRetenido    float64 `json:"imp_retenido"`
	ImpPagar       float64 `json:"imp_pagar"`
}

func (s *BillingService) CreateAndSendRetention(input CreateRetentionInput) (*database.TenantRetention, error) {
	if !s.useLycet {
		return nil, errors.New("retención solo disponible con facturador Lycet")
	}
	companyCfg, companyAddr, err := s.getCompanyConfigAndAddress()
	if err != nil {
		return nil, err
	}
	nombreComercial := companyCfg.TradeName
	if nombreComercial == "" {
		nombreComercial = companyCfg.BusinessName
	}
	provAddr, errProv := s.buildInvoiceAddressFromUbigeo(input.Proveedor.Ubigeo, input.Proveedor.Address)
	if errProv != nil {
		return nil, fmt.Errorf("proveedor: %w", errProv)
	}
	details := make([]facturador.RetentionDetail, len(input.Details))
	for i, d := range input.Details {
		details[i] = facturador.RetentionDetail{
			TipoDoc: d.TipoDoc, NumDoc: d.NumDoc, FechaEmision: d.FechaEmision,
			ImpTotal: d.ImpTotal, Moneda: d.Moneda, FechaRetencion: d.FechaRetencion,
			ImpRetenido: d.ImpRetenido, ImpPagar: d.ImpPagar,
		}
		if details[i].Moneda == "" {
			details[i].Moneda = "PEN"
		}
	}
	payload := &facturador.RetentionPayload{
		Serie:        input.Series,
		Correlativo:  input.Correlativo,
		FechaEmision: input.FechaEmision,
		Company:      facturador.InvoiceCompany{RUC: companyCfg.RUC, RazonSocial: companyCfg.BusinessName, NombreComercial: nombreComercial, Address: companyAddr},
		Proveedor:    facturador.InvoiceClient{TipoDoc: input.Proveedor.TipoDoc, NumDoc: input.Proveedor.NumDoc, RznSocial: input.Proveedor.RznSocial, Address: provAddr},
		Regimen:      input.Regimen,
		Tasa:         input.Tasa,
		ImpRetenido:  input.ImpRetenido,
		ImpPagado:    input.ImpPagado,
		Details:      details,
	}
	payloadJSON, _ := json.Marshal(payload)
	client := facturador.NewClient(s.baseURL, s.token)
	resp, err := client.SendRetention(payload)
	if err != nil {
		return nil, err
	}
	rec := &database.TenantRetention{
		Series:         input.Series,
		Correlative:    input.Correlativo,
		ProveedorRUC:   input.Proveedor.NumDoc,
		ProveedorRazon: input.Proveedor.RznSocial,
		Regimen:        input.Regimen,
		Tasa:           input.Tasa,
		ImpRetenido:    input.ImpRetenido,
		ImpPagado:      input.ImpPagado,
		PayloadJSON:    string(payloadJSON),
		DetailsCount:   len(details),
	}
	if t, err := time.Parse(time.RFC3339, input.FechaEmision); err == nil {
		rec.FechaEmision = t
	} else {
		rec.FechaEmision = time.Now()
	}
	if resp.Success() {
		rec.Status = "accepted"
		rec.SunatCode = resp.CDRCode()
		rec.SunatMessage = resp.Message()
		if resp.CDRZipBase64() != "" {
			basePath := config.AppConfig.InvoiceStoragePath
			if basePath == "" {
				basePath = "./storage/invoices"
			}
			ruc := companyCfg.RUC
			if ruc == "" {
				ruc = "default"
			}
			cdrDec, _ := base64.StdEncoding.DecodeString(resp.CDRZipBase64())
			if len(cdrDec) > 0 {
				cdrPath, _ := saveInvoiceFile(basePath, ruc, "lycet", "20", input.Series, input.Correlativo, "cdr.zip", cdrDec)
				rec.CDRURL = cdrPath
			}
		}
	} else {
		rec.Status = "rejected"
		rec.SunatMessage = resp.Message()
		rec.SunatCode = resp.CDRCode()
	}
	if err := s.db.Create(rec).Error; err != nil {
		return nil, err
	}
	return rec, nil
}

// --- Percepción ---

func (s *BillingService) ListPerceptions() ([]database.TenantPerception, error) {
	var list []database.TenantPerception
	err := s.db.Order("fecha_emision DESC, created_at DESC").Find(&list).Error
	return list, err
}

type CreatePerceptionInput struct {
	Series       string                   `json:"series"`
	Correlativo  string                   `json:"correlativo"`
	FechaEmision string                   `json:"fecha_emision"`
	Proveedor    PerceptionProveedorInput `json:"proveedor"`
	Regimen      string                   `json:"regimen"`
	Tasa         float64                  `json:"tasa"`
	ImpPercibido float64                  `json:"imp_percibido"`
	ImpCobrado   float64                  `json:"imp_cobrado"`
	Details      []PerceptionDetailInput  `json:"details"`
}

type PerceptionProveedorInput struct {
	TipoDoc   string `json:"tipo_doc"`
	NumDoc    string `json:"num_doc"`
	RznSocial string `json:"rzn_social"`
	Address   string `json:"address"`
	Ubigeo    string `json:"ubigeo"`
}

type PerceptionDetailInput struct {
	TipoDoc         string  `json:"tipo_doc"`
	NumDoc          string  `json:"num_doc"`
	FechaEmision    string  `json:"fecha_emision"`
	ImpTotal        float64 `json:"imp_total"`
	Moneda          string  `json:"moneda"`
	FechaPercepcion string  `json:"fecha_percepcion"`
	ImpPercibido    float64 `json:"imp_percibido"`
	ImpCobrar       float64 `json:"imp_cobrar"`
}

func (s *BillingService) CreateAndSendPerception(input CreatePerceptionInput) (*database.TenantPerception, error) {
	if !s.useLycet {
		return nil, errors.New("percepción solo disponible con facturador Lycet")
	}
	companyCfg, companyAddr, err := s.getCompanyConfigAndAddress()
	if err != nil {
		return nil, err
	}
	nombreComercial := companyCfg.TradeName
	if nombreComercial == "" {
		nombreComercial = companyCfg.BusinessName
	}
	provAddr, errProv := s.buildInvoiceAddressFromUbigeo(input.Proveedor.Ubigeo, input.Proveedor.Address)
	if errProv != nil {
		return nil, fmt.Errorf("proveedor: %w", errProv)
	}
	details := make([]facturador.PerceptionDetail, len(input.Details))
	for i, d := range input.Details {
		details[i] = facturador.PerceptionDetail{
			TipoDoc: d.TipoDoc, NumDoc: d.NumDoc, FechaEmision: d.FechaEmision,
			ImpTotal: d.ImpTotal, Moneda: d.Moneda, FechaPercepcion: d.FechaPercepcion,
			ImpPercibido: d.ImpPercibido, ImpCobrar: d.ImpCobrar,
		}
		if details[i].Moneda == "" {
			details[i].Moneda = "PEN"
		}
	}
	payload := &facturador.PerceptionPayload{
		Serie:        input.Series,
		Correlativo:  input.Correlativo,
		FechaEmision: input.FechaEmision,
		Company:      facturador.InvoiceCompany{RUC: companyCfg.RUC, RazonSocial: companyCfg.BusinessName, NombreComercial: nombreComercial, Address: companyAddr},
		Proveedor:    facturador.InvoiceClient{TipoDoc: input.Proveedor.TipoDoc, NumDoc: input.Proveedor.NumDoc, RznSocial: input.Proveedor.RznSocial, Address: provAddr},
		Regimen:      input.Regimen,
		Tasa:         input.Tasa,
		ImpPercibido: input.ImpPercibido,
		ImpCobrado:   input.ImpCobrado,
		Details:      details,
	}
	payloadJSON, _ := json.Marshal(payload)
	client := facturador.NewClient(s.baseURL, s.token)
	resp, err := client.SendPerception(payload)
	if err != nil {
		return nil, err
	}
	rec := &database.TenantPerception{
		Series:         input.Series,
		Correlative:    input.Correlativo,
		ProveedorRUC:   input.Proveedor.NumDoc,
		ProveedorRazon: input.Proveedor.RznSocial,
		Regimen:        input.Regimen,
		Tasa:           input.Tasa,
		ImpPercibido:   input.ImpPercibido,
		ImpCobrado:     input.ImpCobrado,
		PayloadJSON:    string(payloadJSON),
		DetailsCount:   len(details),
	}
	if t, err := time.Parse(time.RFC3339, input.FechaEmision); err == nil {
		rec.FechaEmision = t
	} else {
		rec.FechaEmision = time.Now()
	}
	if resp.Success() {
		rec.Status = "accepted"
		rec.SunatCode = resp.CDRCode()
		rec.SunatMessage = resp.Message()
		if resp.CDRZipBase64() != "" {
			basePath := config.AppConfig.InvoiceStoragePath
			if basePath == "" {
				basePath = "./storage/invoices"
			}
			ruc := companyCfg.RUC
			if ruc == "" {
				ruc = "default"
			}
			cdrDec, _ := base64.StdEncoding.DecodeString(resp.CDRZipBase64())
			if len(cdrDec) > 0 {
				cdrPath, _ := saveInvoiceFile(basePath, ruc, "lycet", "40", input.Series, input.Correlativo, "cdr.zip", cdrDec)
				rec.CDRURL = cdrPath
			}
		}
	} else {
		rec.Status = "rejected"
		rec.SunatMessage = resp.Message()
		rec.SunatCode = resp.CDRCode()
	}
	if err := s.db.Create(rec).Error; err != nil {
		return nil, err
	}
	return rec, nil
}

// --- Reversión (mismo esquema que voided) ---

func (s *BillingService) ListReversions() ([]database.TenantSunatReversion, error) {
	var list []database.TenantSunatReversion
	err := s.db.Order("fec_comunicacion DESC, created_at DESC").Find(&list).Error
	return list, err
}

func (s *BillingService) CreateReversion(details []CreateVoidedInput) (*database.TenantSunatReversion, error) {
	if !s.useLycet {
		return nil, errors.New("reversión solo disponible con facturador Lycet")
	}
	if len(details) == 0 {
		return nil, errors.New("se requiere al menos un comprobante para revertir")
	}
	companyCfg, companyAddr, err := s.getCompanyConfigAndAddress()
	if err != nil {
		return nil, err
	}
	nombreComercial := companyCfg.TradeName
	if nombreComercial == "" {
		nombreComercial = companyCfg.BusinessName
	}
	now := time.Now()
	var count int64
	s.db.Model(&database.TenantSunatReversion{}).Where("DATE(fec_comunicacion) = ?", now.Format("2006-01-02")).Count(&count)
	correlativo := strconv.FormatInt(count+1, 10)
	voidedDetails := make([]facturador.VoidedDetail, len(details))
	for i, d := range details {
		voidedDetails[i] = facturador.VoidedDetail{TipoDoc: d.TipoDoc, Serie: d.Serie, Correlativo: d.Correlativo, DesMotivoBaja: d.DesMotivoBaja}
	}
	payload := &facturador.VoidedPayload{
		Company:         facturador.InvoiceCompany{RUC: companyCfg.RUC, RazonSocial: companyCfg.BusinessName, NombreComercial: nombreComercial, Address: companyAddr},
		Correlativo:     correlativo,
		FecGeneracion:   now.Format(time.RFC3339),
		FecComunicacion: now.Format(time.RFC3339),
		Details:         voidedDetails,
	}
	payloadJSON, _ := json.Marshal(payload)
	client := facturador.NewClient(s.baseURL, s.token)
	resp, err := client.SendReversion(payload)
	if err != nil {
		return nil, err
	}
	rec := &database.TenantSunatReversion{
		FecComunicacion: now,
		Correlativo:     correlativo,
		Ticket:          resp.Ticket(),
		Status:          "pending",
		PayloadJSON:     string(payloadJSON),
		DetailsCount:    len(details),
	}
	if resp.CDRZipBase64() != "" {
		rec.Status = "accepted"
		if resp.SunatResponse != nil && resp.SunatResponse.CDRResponse != nil {
			rec.SunatCode = resp.SunatResponse.CDRResponse.Code
			rec.SunatMessage = resp.SunatResponse.CDRResponse.Description
			if !resp.SunatResponse.CDRResponse.Accepted {
				rec.Status = "rejected"
			}
		}
		basePath := config.AppConfig.InvoiceStoragePath
		if basePath == "" {
			basePath = "./storage/invoices"
		}
		ruc := companyCfg.RUC
		if ruc == "" {
			ruc = "default"
		}
		cdrDec, _ := base64.StdEncoding.DecodeString(resp.CDRZipBase64())
		if len(cdrDec) > 0 {
			cdrPath, _ := saveInvoiceFile(basePath, ruc, "lycet", "RR", "RR", correlativo+"-"+now.Format("20060102"), "cdr.zip", cdrDec)
			rec.CDRURL = cdrPath
		}
	} else if resp.Message() != "" {
		rec.SunatMessage = resp.Message()
		rec.SunatCode = resp.CDRCode()
		if resp.Success() {
			rec.Status = "accepted"
		} else {
			rec.Status = "rejected"
		}
	}
	if err := s.db.Create(rec).Error; err != nil {
		return nil, err
	}
	return rec, nil
}

func (s *BillingService) GetReversionStatus(id uint) (*database.TenantSunatReversion, error) {
	var rec database.TenantSunatReversion
	if err := s.db.First(&rec, id).Error; err != nil {
		return nil, err
	}
	if rec.Ticket == "" {
		return &rec, nil
	}
	var cfg database.TenantCompanyConfig
	if s.db.Select("ruc").First(&cfg).Error != nil {
		return &rec, nil
	}
	client := facturador.NewClient(s.baseURL, s.token)
	result, err := client.GetReversionStatus(rec.Ticket, cfg.RUC)
	if err != nil {
		return &rec, err
	}
	if result.Success && result.CDRZip != "" {
		basePath := config.AppConfig.InvoiceStoragePath
		if basePath == "" {
			basePath = "./storage/invoices"
		}
		cdrDec, _ := base64.StdEncoding.DecodeString(result.CDRZip)
		if len(cdrDec) > 0 {
			ruc := cfg.RUC
			if ruc == "" {
				ruc = "default"
			}
			cdrPath, _ := saveInvoiceFile(basePath, ruc, "lycet", "RR", "RR", rec.Correlativo+"-"+rec.FecComunicacion.Format("20060102"), "cdr.zip", cdrDec)
			rec.CDRURL = cdrPath
		}
		if result.CDRResponse != nil {
			rec.SunatCode = result.CDRResponse.Code
			rec.SunatMessage = result.CDRResponse.Description
			if result.CDRResponse.Accepted {
				rec.Status = "accepted"
			} else {
				rec.Status = "rejected"
			}
		} else {
			rec.Status = "accepted"
		}
	} else if result.Error != nil {
		rec.SunatMessage = result.Error.Message
		rec.SunatCode = result.Error.Code
		rec.Status = "error"
	}
	s.db.Save(&rec)
	return &rec, nil
}
