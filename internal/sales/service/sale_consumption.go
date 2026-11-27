package service

// F5a: enganche de las ventas con el consumo de insumos por receta. Ver internal/recipes/service/consumption.go.

import (
	"sort"

	invsvc "tukifac/internal/inventory/service"
	recipesvc "tukifac/internal/recipes/service"
	"tukifac/pkg/database"

	"gorm.io/gorm"
)

func presentationID(p *uint) uint {
	if p == nil {
		return 0
	}
	return *p
}

// planSaleConsumption: demandas de las líneas de la venta. Devuelve nil con el descuento apagado, sin consultar nada más.
func planSaleConsumption(db *gorm.DB, items []SaleItemInput) (*recipesvc.Plan, error) {
	active, block, err := recipesvc.ConsumptionPolicy(db)
	if err != nil || !active {
		return nil, err
	}
	var demands []recipesvc.Demand
	for i, it := range items {
		if it.ProductID == nil || *it.ProductID == 0 {
			continue
		}
		if len(it.comboComponents) > 0 {
			ids := make([]uint, 0, len(it.comboComponents))
			for id := range it.comboComponents {
				ids = append(ids, id)
			}
			sort.Slice(ids, func(a, b int) bool { return ids[a] < ids[b] })
			for _, id := range ids {
				demands = append(demands, recipesvc.Demand{Line: i, ProductID: id, Quantity: it.comboComponents[id], Component: true})
			}
			continue
		}
		d := recipesvc.Demand{Line: i, ProductID: *it.ProductID, PresentationID: presentationID(it.PresentationID), Quantity: it.Quantity}
		// Con unidad de venta, la receta cuenta unidades base del plato (igual que su stock).
		if d.PresentationID == 0 && it.SaleUnitID != nil && *it.SaleUnitID > 0 {
			res, err := resolveSaleUnitForLine(db, *it.ProductID, it.SaleUnitID, it.Quantity)
			if err != nil {
				return nil, err
			}
			d.Quantity = res.BaseQuantity
		}
		demands = append(demands, d)
	}
	return recipesvc.PlanConsumption(db, block, demands)
}

// restoreSaleStockTx devuelve lo que salió por una venta anulada: su stock directo (según el kardex) y los insumos consumidos (según el
// registro de consumos). Todos los saldos se bloquean primero y juntos, en el mismo orden que al vender.
func restoreSaleStockTx(tx *gorm.DB, sale *database.TenantSale, ref string, userID uint) error {
	supplyKeys, err := recipesvc.SaleStockKeys(tx, sale.ID)
	if err != nil {
		return err
	}
	if len(supplyKeys) > 0 {
		var outs []database.TenantStockMovement
		if err := tx.Where("reference = ? AND type = ?", saleStockReference(sale.Number), "out").Find(&outs).Error; err != nil {
			return err
		}
		if err := invsvc.LockStockRowsTx(tx, append(movementStockKeys(outs), supplyKeys...)); err != nil {
			return err
		}
	}
	if err := restoreStockFromKardexTx(tx, sale, ref, userID); err != nil {
		return err
	}
	if len(supplyKeys) == 0 {
		return nil
	}
	uid := userID
	if uid == 0 {
		uid = sale.UserID
	}
	return recipesvc.ReverseSaleTx(tx, sale.ID, ref, sale.Number, uid)
}
