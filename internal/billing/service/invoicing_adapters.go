package service

import (
	"errors"

	"tukifac/pkg/database"
)

type fiscalEmitterAdapter struct {
	svc *BillingService
}

func (a *fiscalEmitterAdapter) SendToSUNAT(saleID uint, companyCfg *database.TenantCompanyConfig) (*database.TenantInvoice, error) {
	if a == nil || a.svc == nil {
		return nil, errors.New("servicio fiscal no inicializado")
	}
	return a.svc.sendToFacturador(saleID, companyCfg)
}
