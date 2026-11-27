package service

import (
	"math"

	"tukifac/pkg/database"
	"tukifac/pkg/taxregime"

	"gorm.io/gorm"
)

// supplyBaseCost: costo por unidad base, en soles, con el que un INSUMO entra al kardex y a «último costo» (F2b). Va sin IGV, salvo
// en el Nuevo RUS, que no lo recupera y por eso es parte del costo. Se calcula sobre el importe de la línea (no sobre el precio
// unitario redondeado) y con el tipo de cambio a 4 decimales, que es el que la compra guarda.
func supplyBaseCost(db *gorm.DB, line database.TenantPurchaseItem, penFactor, baseQuantity float64) float64 {
	if baseQuantity <= 0 {
		return 0
	}
	var cfg database.TenantCompanyConfig
	amount := line.Subtotal
	if err := db.Select("taxpayer_regime").Limit(1).Find(&cfg).Error; err == nil && taxregime.Normalize(cfg.TaxpayerRegime) == taxregime.NRUS {
		amount = line.Total
	}
	rate := math.Round(penFactor*1e4) / 1e4
	return math.Round(amount*rate/baseQuantity*1e6) / 1e6
}
