package service

import (
	"errors"

	"tukifac/pkg/billingstate"
	"tukifac/pkg/database"

	"gorm.io/gorm"
)

// GetBillingStatus devuelve estado verificable para polling (GET /billing/status/:saleId).
func (s *BillingService) GetBillingStatus(saleID uint) (*billingstate.StatusView, error) {
	var sale database.TenantSale
	if err := s.db.First(&sale, saleID).Error; err != nil {
		return nil, errors.New("venta no encontrada")
	}
	var inv database.TenantInvoice
	err := s.db.Where("sale_id = ?", saleID).First(&inv).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		v := billingstate.BuildStatusView(nil, &sale)
		return &v, nil
	}
	if err != nil {
		return nil, err
	}
	v := billingstate.BuildStatusView(&inv, &sale)
	return &v, nil
}

// IsSafeToPrint indica si hay evidencia SUNAT para imprimir/PDF oficial.
func (s *BillingService) IsSafeToPrint(saleID uint) (bool, error) {
	st, err := s.GetBillingStatus(saleID)
	if err != nil {
		return false, err
	}
	return st.SafeToPrint, nil
}
