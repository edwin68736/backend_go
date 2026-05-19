package service

import (
	"encoding/json"
	"errors"
	"strings"

	"tukifac/pkg/database"

	"gorm.io/gorm"
)

// InvoiceModeConfig es la estructura que devuelve el servicio de configuración
// y contiene la información necesaria para determinar el modo de facturación.
type InvoiceModeConfig struct {
	Mode        string `json:"invoice_mode"` // "legacy_backend" | "pse"
	PSEProvider string `json:"pse_provider,omitempty"`
	PSEToken    string `json:"pse_token,omitempty"`
	PSEBaseURL  string `json:"pse_base_url,omitempty"` // URL base del PSE
}

// InvoicingConfigService se encarga de recuperar la configuración de facturación del tenant.
type InvoicingConfigService struct {
	db *gorm.DB
}

// NewInvoicingConfigService crea una nueva instancia del servicio.
func NewInvoicingConfigService(db *gorm.DB) *InvoicingConfigService {
	return &InvoicingConfigService{db: db}
}

// GetConfig recupera la configuración de facturación del tenant actual.
// Lee de la tabla TenantCompanyConfig y mapea los campos al struct InvoiceModeConfig.
func (s *InvoicingConfigService) GetConfig() (*InvoiceModeConfig, error) {
	var tenantCfg database.TenantCompanyConfig
	if err := s.db.First(&tenantCfg).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// Si no existe configuración, devolvemos modo legacy por defecto (seguro)
			return &InvoiceModeConfig{Mode: InvoicingModeLegacyBackend}, nil
		}
		return nil, err
	}

	mode := strings.ToLower(strings.TrimSpace(tenantCfg.InvoicingMode))
	if mode == "" {
		mode = InvoicingModeLegacyBackend
	}

	config := &InvoiceModeConfig{
		Mode: mode,
	}

	// Si el modo es PSE, intentamos parsear la configuración JSON adicional
	if mode == InvoicingModePSE {
		// Mapeamos campos directos si existen en TenantCompanyConfig (como PSEBaseURL)
		config.PSEBaseURL = tenantCfg.PSEBaseURL

		// Parseamos PSEConfigJSON si existe
		if tenantCfg.PSEConfigJSON != "" {
			var pseExtra struct {
				Provider string `json:"provider"`
				Token    string `json:"token"`
				BaseURL  string `json:"base_url"` // Override opcional
			}
			if err := json.Unmarshal([]byte(tenantCfg.PSEConfigJSON), &pseExtra); err == nil {
				config.PSEProvider = pseExtra.Provider
				config.PSEToken = pseExtra.Token
				if pseExtra.BaseURL != "" {
					config.PSEBaseURL = pseExtra.BaseURL
				}
			}
		}

		// Fallback: si no hay token en JSON, usar el campo directo si existe (para compatibilidad)
		if config.PSEToken == "" && tenantCfg.PSEToken != "" {
			config.PSEToken = tenantCfg.PSEToken
		}
	}

	return config, nil
}
