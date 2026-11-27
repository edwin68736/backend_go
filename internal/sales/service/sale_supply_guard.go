package service

import (
	"tukifac/pkg/supplies"

	"gorm.io/gorm"
)

// rejectSupplyLines: ninguna línea ni salida de inventario de una venta puede ser un insumo (ver pkg/supplies).
func rejectSupplyLines(db *gorm.DB, input CreateSaleInput) error {
	ids := make([]uint, 0, len(input.Items)+len(input.ExtraStockMovements))
	for _, it := range input.Items {
		if it.ProductID != nil {
			ids = append(ids, *it.ProductID)
		}
	}
	for _, mv := range input.ExtraStockMovements {
		ids = append(ids, mv.ProductID)
	}
	return supplies.Reject(db, ids...)
}
