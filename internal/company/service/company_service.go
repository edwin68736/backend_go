package service

import (
	"encoding/json"
	"errors"
	"strings"

	"tukifac/config"
	"tukifac/pkg/database"
	"tukifac/pkg/facturador"

	"gorm.io/gorm"
)

type CompanyService struct {
	db *gorm.DB
}

func NewCompanyService(db *gorm.DB) *CompanyService {
	return &CompanyService{db: db}
}

func (s *CompanyService) GetConfig() (*database.TenantCompanyConfig, error) {
	var cfg database.TenantCompanyConfig
	if err := s.db.First(&cfg).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return &database.TenantCompanyConfig{Currency: "PEN", TaxRate: 18, IgvRegime: "standard"}, nil
		}
		return nil, err
	}
	// Garantizar defaults si campos nuevos están vacíos (registros antiguos)
	if cfg.TaxRate == 0 {
		cfg.TaxRate = 18
	}
	if cfg.IgvRegime == "" {
		cfg.IgvRegime = "standard"
	}
	if strings.TrimSpace(cfg.InvoicingMode) == "" {
		cfg.InvoicingMode = "legacy_backend"
	}
	return &cfg, nil
}

func (s *CompanyService) GetInvoicingSettings() (mode string, pseConfigured bool, err error) {
	var cfg database.TenantCompanyConfig
	if err := s.db.Select("invoicing_mode", "pse_base_url", "pse_token").First(&cfg).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "legacy_backend", false, nil
		}
		return "", false, err
	}
	mode = strings.TrimSpace(cfg.InvoicingMode)
	if mode == "" {
		mode = "legacy_backend"
	}
	pseConfigured = strings.TrimSpace(cfg.PSEBaseURL) != "" && strings.TrimSpace(cfg.PSEToken) != ""
	return mode, pseConfigured, nil
}

func (s *CompanyService) SetInvoicingMode(mode string) error {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		mode = "legacy_backend"
	}
	if mode != "legacy_backend" && mode != "pse" {
		return errors.New("invoicing_mode debe ser legacy_backend o pse")
	}
	var existing database.TenantCompanyConfig
	if err := s.db.First(&existing).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return errors.New("configure primero los datos generales de la empresa")
	}
	return s.db.Model(&existing).Update("invoicing_mode", mode).Error
}

func (s *CompanyService) SaveConfig(input database.TenantCompanyConfig) error {
	var existing database.TenantCompanyConfig
	if err := s.db.First(&existing).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return s.db.Create(&input).Error
	}
	return s.db.Model(&existing).Updates(map[string]interface{}{
		// Razón Social y RUC no se actualizan desde el panel tenant; solo desde el panel central.
		"trade_name":  input.TradeName,
		"address":     input.Address,
		"ubigeo":      input.Ubigeo,
		"country":     input.Country,
		"phone":       input.Phone,
		"email":       input.Email,
		"website":     input.Website,
		"logo_url":    input.LogoURL,
		"currency":    input.Currency,
		"tax_rate":    input.TaxRate,
		"color_theme": input.ColorTheme,
	}).Error
}

// SaveSunatConfigTenant guarda solo los campos que el tenant puede editar: IGV, régimen, zona beneficio.
// sunat_enabled se controla desde el panel central; el tenant no puede activar/desactivar la facturación electrónica.
func (s *CompanyService) SaveSunatConfigTenant(taxRate float64, igvRegime string, taxBenefitZone bool) error {
	var existing database.TenantCompanyConfig
	if err := s.db.First(&existing).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return errors.New("configure primero los datos generales de la empresa")
	}
	if taxRate <= 0 {
		taxRate = 18
	}
	if igvRegime == "" {
		igvRegime = "standard"
	}
	return s.db.Model(&existing).Updates(map[string]interface{}{
		"tax_rate":         taxRate,
		"igv_regime":       igvRegime,
		"tax_benefit_zone": taxBenefitZone,
	}).Error
}

// SaveSunatConfig guarda la configuración completa (panel central): SOL, certificado, ambiente, etc.
func (s *CompanyService) SaveSunatConfig(enabled bool, solUser, solPass, certificate, envMode, tukifacToken string, taxRate float64, igvRegime string, taxBenefitZone bool) error {
	var existing database.TenantCompanyConfig
	if err := s.db.First(&existing).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return errors.New("configure primero los datos generales de la empresa")
	}
	if taxRate <= 0 {
		taxRate = 18
	}
	if igvRegime == "" {
		igvRegime = "standard"
	}
	updates := map[string]interface{}{
		"sunat_enabled":    enabled,
		"sunat_env_mode":   envMode,
		"sunat_sol_user":   solUser,
		"tax_rate":         taxRate,
		"igv_regime":       igvRegime,
		"tax_benefit_zone": taxBenefitZone,
	}
	if solPass != "" {
		updates["sunat_sol_pass"] = solPass
	}
	if certificate != "" {
		updates["sunat_certificate"] = certificate
	}
	if tukifacToken != "" {
		updates["tukifac_token"] = tukifacToken
	}
	return s.db.Model(&existing).Updates(updates).Error
}

func (s *CompanyService) SaveInvoicingConfigCentral(mode, pseBaseURL, pseToken, pseProvider string) error {
	var existing database.TenantCompanyConfig
	if err := s.db.First(&existing).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return errors.New("configure primero los datos generales de la empresa")
	}

	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		mode = "legacy_backend"
	}
	if mode != "legacy_backend" && mode != "pse" {
		return errors.New("invoicing_mode debe ser legacy_backend o pse")
	}

	updates := map[string]interface{}{
		"invoicing_mode": mode,
	}

	pseBaseURL = strings.TrimSpace(pseBaseURL)
	pseToken = strings.TrimSpace(pseToken)
	pseProvider = strings.TrimSpace(pseProvider)

	if pseBaseURL != "" {
		updates["pse_base_url"] = pseBaseURL
	}
	if pseToken != "" {
		updates["pse_token"] = pseToken
	}

	if pseProvider != "" {
		b, _ := json.Marshal(map[string]string{"provider": pseProvider})
		updates["pse_config_json"] = string(b)
	} else if existing.PSEConfigJSON != "" {
		var prev struct {
			Provider string `json:"provider"`
		}
		if err := json.Unmarshal([]byte(existing.PSEConfigJSON), &prev); err == nil && prev.Provider != "" {
			b, _ := json.Marshal(map[string]string{"provider": prev.Provider})
			updates["pse_config_json"] = string(b)
		}
	}

	if mode == "pse" {
		finalBaseURL := pseBaseURL
		if finalBaseURL == "" {
			finalBaseURL = strings.TrimSpace(existing.PSEBaseURL)
		}
		finalToken := pseToken
		if finalToken == "" {
			finalToken = strings.TrimSpace(existing.PSEToken)
		}
		if finalBaseURL == "" || finalToken == "" {
			return errors.New("configuración PSE incompleta: requiere pse_base_url y pse_token para activar modo PSE")
		}
		updates["pse_base_url"] = finalBaseURL
	}

	return s.db.Model(&existing).Updates(updates).Error
}

// SyncFacturadorConfig envía la configuración SUNAT del tenant al backend facturador (Lycet).
// Actualiza el archivo empresas.json en el facturador con SOL_USER, SOL_PASS y nombres de archivo
// para certificado y logo ({ruc}-cert.pem, {ruc}-logo.png). Esos archivos deben existir en data/
// del servidor facturador o enviarse por otro medio.
func (s *CompanyService) SyncFacturadorConfig() error {
	return s.syncFacturador("", "", "", "", "")
}

// SyncFacturadorConfigWithFiles envía la configuración y opcionalmente el certificado y/o logo en base64.
// Si se envían privateKeyBase64 y certificateBase64, se construye un único PEM (clave privada + certificado) que es lo que Lycet necesita para firmar.
// solUserOverride y solPassOverride son opcionales: si se envían en la petición de sync, se usan para esta sincronización en lugar de lo guardado en BD.
func (s *CompanyService) SyncFacturadorConfigWithFiles(certificateBase64, privateKeyBase64, logoBase64, solUserOverride, solPassOverride string) error {
	return s.syncFacturador(certificateBase64, privateKeyBase64, logoBase64, solUserOverride, solPassOverride)
}

func (s *CompanyService) syncFacturador(certificateBase64, privateKeyBase64, logoBase64, solUserOverride, solPassOverride string) error {
	if config.AppConfig.FacturadorBaseURL == "" || config.AppConfig.FacturadorToken == "" {
		return errors.New("facturador no configurado: define FACTURADOR_BASE_URL y FACTURADOR_TOKEN en el servidor")
	}
	var cfg database.TenantCompanyConfig
	if err := s.db.First(&cfg).Error; err != nil {
		return errors.New("configure primero los datos de la empresa y SUNAT")
	}
	if cfg.RUC == "" {
		return errors.New("el RUC de la empresa es requerido para sincronizar con el facturador")
	}
	solUser := cfg.SunatSOLUser
	if solUser == "" {
		solUser = cfg.RUC + "MODDATOS"
	}
	if solUserOverride != "" {
		solUser = solUserOverride
	}
	solPass := cfg.SunatSOLPass
	if solPassOverride != "" {
		solPass = solPassOverride
	}
	client := facturador.NewClient(config.AppConfig.FacturadorBaseURL, config.AppConfig.FacturadorToken)

	// Lycet necesita un único PEM con: primero clave privada, luego certificado.
	if certificateBase64 == "" && cfg.SunatCertificate != "" {
		certificateBase64 = facturador.PEMToBase64(cfg.SunatCertificate)
	}
	if privateKeyBase64 != "" && certificateBase64 != "" {
		combined, err := facturador.BuildCombinedPEMBase64(privateKeyBase64, certificateBase64)
		if err != nil {
			return err
		}
		certificateBase64 = combined
	}
	// Mapear ambiente: API Lycet usa "pruebas" | "produccion"; nosotros usamos production/beta/demo.
	ambiente := "pruebas"
	if cfg.SunatEnvMode == "production" {
		ambiente = "produccion"
	}
	// Sincronizar vía POST /api/v1/empresas (API-EMPRESAS.md); una sola empresa con ruc, SOL_USER, SOL_PASS, ambiente, certificado/logo opcionales.
	return client.SyncEmpresas(cfg.RUC, solUser, solPass, ambiente, certificateBase64, logoBase64)
}

// IsSunatEnabled indica si la empresa tiene activa la conexión con SUNAT.
func (s *CompanyService) IsSunatEnabled() bool {
	var cfg database.TenantCompanyConfig
	if err := s.db.Select("sunat_enabled").First(&cfg).Error; err != nil {
		return false
	}
	return cfg.SunatEnabled
}

// Sucursales
func (s *CompanyService) ListBranches() ([]database.TenantBranch, error) {
	var branches []database.TenantBranch
	err := s.db.Order("is_main DESC, name ASC").Find(&branches).Error
	return branches, err
}

func (s *CompanyService) GetBranch(id uint) (*database.TenantBranch, error) {
	var b database.TenantBranch
	err := s.db.First(&b, id).Error
	return &b, err
}

func (s *CompanyService) CreateBranch(name, address, phone string, isMain bool) (*database.TenantBranch, error) {
	if name == "" {
		return nil, errors.New("el nombre de la sucursal es requerido")
	}
	if isMain {
		s.db.Model(&database.TenantBranch{}).Update("is_main", false)
	}
	b := &database.TenantBranch{Name: name, Address: address, Phone: phone, IsMain: isMain, Active: true}
	err := s.db.Create(b).Error
	return b, err
}

func (s *CompanyService) UpdateBranch(id uint, name, address, phone string, isMain bool) error {
	if isMain {
		s.db.Model(&database.TenantBranch{}).Where("id != ?", id).Update("is_main", false)
	}
	return s.db.Model(&database.TenantBranch{}).Where("id = ?", id).Updates(map[string]interface{}{
		"name":    name,
		"address": address,
		"phone":   phone,
		"is_main": isMain,
	}).Error
}

func (s *CompanyService) DeleteBranch(id uint) error {
	return s.db.Delete(&database.TenantBranch{}, id).Error
}

// Series y correlativos
func (s *CompanyService) ListSeries(branchID uint) ([]database.TenantDocumentSeries, error) {
	var series []database.TenantDocumentSeries
	q := s.db.Model(&database.TenantDocumentSeries{})
	if branchID > 0 {
		q = q.Where("branch_id = ?", branchID)
	}
	err := q.Order("branch_id ASC, doc_type ASC").Find(&series).Error
	return series, err
}

func (s *CompanyService) CreateSeries(branchID uint, docType, sunatCode, category, seriesName string) error {
	if seriesName == "" || docType == "" {
		return errors.New("serie y tipo de documento son requeridos")
	}
	if category == "" {
		category = "venta"
	}
	if sunatCode == "" {
		sunatCode = "01"
	}
	return s.db.Create(&database.TenantDocumentSeries{
		BranchID:    branchID,
		DocType:     docType,
		SunatCode:   sunatCode,
		Category:    category,
		Series:      seriesName,
		Correlative: 1,
		Active:      true,
	}).Error
}

func (s *CompanyService) UpdateSeries(id uint, seriesName string, active bool, docType, sunatCode, category string, correlative *uint) error {
	updates := map[string]interface{}{"series": seriesName, "active": active}
	if docType != "" {
		updates["doc_type"] = docType
	}
	if sunatCode != "" {
		updates["sunat_code"] = sunatCode
	}
	if category != "" {
		updates["category"] = category
	}
	if correlative != nil {
		updates["correlative"] = *correlative
	}
	return s.db.Model(&database.TenantDocumentSeries{}).Where("id = ?", id).Updates(updates).Error
}
