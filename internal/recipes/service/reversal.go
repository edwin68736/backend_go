package service

// F5a: reversa de consumos. Siempre sale del registro (nunca de la receta de hoy) y funciona aunque el descuento ya esté apagado.
// El llamador de ReverseSaleTx debe haber bloqueado antes los saldos (SaleStockKeys) para mantener el mismo orden que las ventas.

import (
	"math"

	invsvc "tukifac/internal/inventory/service"
	"tukifac/pkg/database"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const reversalReferencePrefix = "REVERSA RECETA/"

func saleConsumptions(db *gorm.DB, query string, args ...interface{}) ([]database.TenantRecipeConsumption, error) {
	if !ConsumptionReady(db) {
		return nil, nil
	}
	var rows []database.TenantRecipeConsumption
	err := db.Where(query, args...).Order("branch_id, supply_id, id").Find(&rows).Error
	return rows, err
}

func consumptionKeys(rows []database.TenantRecipeConsumption) []invsvc.StockKey {
	keys := make([]invsvc.StockKey, 0, len(rows))
	for _, r := range rows {
		keys = append(keys, invsvc.StockKey{ProductID: r.SupplyID, BranchID: r.BranchID})
	}
	return keys
}

// SaleStockKeys: saldos de insumos que anular o devolver la venta puede mover.
func SaleStockKeys(db *gorm.DB, saleID uint) ([]invsvc.StockKey, error) {
	rows, err := saleConsumptions(db, "sale_id = ?", saleID)
	if err != nil {
		return nil, err
	}
	return consumptionKeys(rows), nil
}

// lockConsumption relee la fila bloqueada por clave primaria (sin bloqueos de rango): su avance es el último confirmado.
func lockConsumption(tx *gorm.DB, id uint) (database.TenantRecipeConsumption, error) {
	var row database.TenantRecipeConsumption
	q := tx.Where("id = ?", id)
	if tx.Dialector.Name() == "mysql" {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	return row, q.Take(&row).Error
}

// restoreTx repone qty del insumo, a lo que costó al salir, y lo registra una sola vez por referencia.
func restoreTx(tx *gorm.DB, row database.TenantRecipeConsumption, qty, returnedUnits float64, reference, number string, userID uint) error {
	var done int64
	if err := tx.Model(&database.TenantRecipeConsumptionReversal{}).Where("consumption_id = ? AND reference = ?", row.ID, reference).
		Count(&done).Error; err != nil || done > 0 {
		return err
	}
	rev := database.TenantRecipeConsumptionReversal{ConsumptionID: row.ID, Reference: reference, Quantity: qty}
	if qty > 0 {
		input := invsvc.MovementInput{
			ProductID: row.SupplyID, BranchID: row.BranchID, Type: "in", Quantity: qty,
			Reference: reversalReferencePrefix + number, UserID: userID, OperationCode: "RECIPE_REVERSAL",
		}
		if row.MovementID > 0 {
			original := row.MovementID
			input.ReversesMovementID = &original
		}
		mv, err := invsvc.NewInventoryService(tx).RecordMovementResultTx(tx, input)
		if err != nil {
			return err
		}
		rev.MovementID = mv.ID
	}
	// El índice único es la última barrera: la misma referencia nunca repone dos veces.
	if err := tx.Create(&rev).Error; err != nil {
		return err
	}
	return tx.Model(&database.TenantRecipeConsumption{}).Where("id = ?", row.ID).Updates(map[string]interface{}{
		"reversed_qty": round3(row.ReversedQty + qty), "returned_units": returnedUnits,
	}).Error
}

// ReverseSaleTx repone todo lo que la venta consumió y aún no se repuso (anulación). reference identifica la operación.
func ReverseSaleTx(tx *gorm.DB, saleID uint, reference, number string, userID uint) error {
	rows, err := saleConsumptions(tx, "sale_id = ?", saleID)
	if err != nil {
		return err
	}
	for _, r := range rows {
		row, err := lockConsumption(tx, r.ID)
		if err != nil {
			return err
		}
		remaining := round3(row.Quantity - row.ReversedQty)
		if remaining <= 0 {
			continue
		}
		if err := restoreTx(tx, row, remaining, row.ReturnedUnits, reference, number, userID); err != nil {
			return err
		}
	}
	return nil
}

// ReversePartialTx repone lo que corresponde a las líneas de una nota de crédito parcial. Es repetible: la misma nota no repone dos
// veces. Se calcula sobre lo devuelto ACUMULADO, así varias notas que devuelven toda la línea reponen exactamente lo consumido.
func ReversePartialTx(tx *gorm.DB, note *database.TenantSale, userID uint) error {
	if note == nil || !ConsumptionReady(tx) {
		return nil
	}
	var noteItems []database.TenantSaleItem
	if err := tx.Where("sale_id = ? AND original_sale_item_id IS NOT NULL", note.ID).Find(&noteItems).Error; err != nil {
		return err
	}
	returned := map[uint]float64{}
	var itemIDs []uint
	for _, ni := range noteItems {
		if ni.OriginalSaleItemID != nil && ni.Quantity > 0 {
			returned[*ni.OriginalSaleItemID] += ni.Quantity
			itemIDs = append(itemIDs, *ni.OriginalSaleItemID)
		}
	}
	if len(itemIDs) == 0 {
		return nil
	}
	rows, err := saleConsumptions(tx, "sale_item_id IN ?", itemIDs)
	if err != nil || len(rows) == 0 {
		return err
	}
	var originals []database.TenantSaleItem
	if err := tx.Select("id", "quantity").Where("id IN ?", itemIDs).Find(&originals).Error; err != nil {
		return err
	}
	sold := map[uint]float64{}
	for _, it := range originals {
		sold[it.ID] = it.Quantity
	}
	if err := invsvc.LockStockRowsTx(tx, consumptionKeys(rows)); err != nil {
		return err
	}
	if userID == 0 {
		userID = note.UserID
	}
	reference := "NC/" + note.Number
	for _, r := range rows {
		row, err := lockConsumption(tx, r.ID)
		if err != nil {
			return err
		}
		total := sold[row.SaleItemID]
		if total <= 0 {
			continue
		}
		units := math.Min(round3(row.ReturnedUnits+returned[row.SaleItemID]), total)
		target := row.Quantity // devuelta toda la línea: todo lo consumido, sin restos de redondeo
		if units < total-0.0005 {
			target = round3(row.Quantity * units / total)
		}
		qty := math.Max(round3(target-row.ReversedQty), 0)
		if err := restoreTx(tx, row, qty, units, reference, note.Number, userID); err != nil {
			return err
		}
	}
	return nil
}
