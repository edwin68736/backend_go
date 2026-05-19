package service

import (
	"errors"
	"strings"

	"tukifac/pkg/database"

	"gorm.io/gorm"
)

const (
	InvoicingModeLegacyBackend = "legacy_backend"
	InvoicingModePSE           = "pse"
)

// LegacyInvoiceAdapter defines the interface for the legacy invoicing flow
// (sending JSON to external PHP backend).
type LegacyInvoiceAdapter interface {
	SendToSUNAT(saleID uint, companyCfg *database.TenantCompanyConfig) (*database.TenantInvoice, error)
}

// PSEInvoiceAdapter defines the interface for the PSE invoicing flow
// (generating UBL locally and sending to PSE).
type PSEInvoiceAdapter interface {
	SendToSUNAT(saleID uint, companyCfg *database.TenantCompanyConfig) (*database.TenantInvoice, error)
	CheckStatus(saleID uint, companyCfg *database.TenantCompanyConfig) (*database.TenantInvoice, error)
}

// InvoiceOrchestrator decides which invoicing flow to use based on tenant configuration.
type InvoiceOrchestrator struct {
	db     *gorm.DB
	legacy LegacyInvoiceAdapter
	pse    PSEInvoiceAdapter
}

// NewInvoiceOrchestrator creates a new orchestrator with injected adapters.
func NewInvoiceOrchestrator(db *gorm.DB, legacy LegacyInvoiceAdapter, pse PSEInvoiceAdapter) *InvoiceOrchestrator {
	return &InvoiceOrchestrator{db: db, legacy: legacy, pse: pse}
}

// SendToSUNAT orchestrates the invoicing process.
// It retrieves the invoicing mode configuration and delegates to the appropriate adapter.
func (o *InvoiceOrchestrator) SendToSUNAT(saleID uint) (*database.TenantInvoice, error) {
	cfgSvc := NewInvoicingConfigService(o.db)
	invCfg, err := cfgSvc.GetConfig()
	if err != nil {
		return nil, err
	}

	var saleRow database.TenantSale
	if err := o.db.First(&saleRow, saleID).Error; err != nil {
		return nil, errors.New("venta no encontrada")
	}
	var serRow database.TenantDocumentSeries
	if err := o.db.First(&serRow, saleRow.SeriesID).Error; err == nil {
		if strings.TrimSpace(serRow.SunatCode) == "00" {
			return nil, errors.New("las notas de venta (SUNAT 00) no se envían a SUNAT. Emita primero una factura o boleta desde Notas de venta y envíe ese comprobante")
		}
	}

	var cfg database.TenantCompanyConfig
	if err := o.db.First(&cfg).Error; err != nil || !cfg.SunatEnabled {
		return nil, errors.New("la conexión con SUNAT no está activada — actívala en Configuración → SUNAT")
	}

	// El modo se determina por la configuración obtenida
	switch invCfg.Mode {
	case InvoicingModePSE:
		if o.pse == nil {
			return nil, errors.New("modo PSE no disponible en este servidor")
		}
		// Podríamos pasar invCfg al adaptador si lo necesitara, pero por ahora mantenemos compatibilidad
		// con la interfaz que recibe *database.TenantCompanyConfig.
		// El adaptador PSE deberá saber leer los campos extra o podríamos extender la interfaz.
		return o.pse.SendToSUNAT(saleID, &cfg)
	case InvoicingModeLegacyBackend:
		fallthrough
	default:
		if o.legacy == nil {
			return nil, errors.New("modo legacy_backend no disponible en este servidor")
		}
		return o.legacy.SendToSUNAT(saleID, &cfg)
	}
}

// CheckStatusOrchestrator checks the status of an invoice.
// Only applicable for PSE mode as legacy handles status differently (resend/consult).
func (o *InvoiceOrchestrator) CheckStatus(saleID uint) (*database.TenantInvoice, error) {
	cfgSvc := NewInvoicingConfigService(o.db)
	invCfg, err := cfgSvc.GetConfig()
	if err != nil {
		return nil, err
	}

	if invCfg.Mode != InvoicingModePSE {
		return nil, errors.New("la consulta de estado CDR solo está disponible en modo PSE")
	}

	var cfg database.TenantCompanyConfig
	if err := o.db.First(&cfg).Error; err != nil {
		return nil, err
	}

	if o.pse == nil {
		return nil, errors.New("modo PSE no disponible en este servidor")
	}

	return o.pse.CheckStatus(saleID, &cfg)
}
