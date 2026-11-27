package service

// F2b: costo promedio ponderado de los INSUMOS, por producto y sucursal. Ver docs/recetas/F2_PROPUESTA.md §3.
//
// Se calcula dentro de RecordMovementTx, con la fila de saldo ya bloqueada. Los productos normales no pasan por aquí. Las columnas de
// costo son de solo lectura en los modelos (un tenant sin V196 sigue registrando movimientos) y se escriben aquí por nombre de tabla.

import (
	"errors"
	"math"
	"math/big"

	"tukifac/pkg/database"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const costColumnProbe = "col:tenant_product_stocks.avg_unit_cost"

// SupplyCostsReady: la BD del tenant tiene las columnas de V196.
func SupplyCostsReady(db *gorm.DB) bool {
	return database.CachedSchemaProbe(db, costColumnProbe, func() bool {
		return db.Migrator().HasColumn(&database.TenantProductStock{}, "avg_unit_cost")
	})
}

func round6(v float64) float64 { return math.Round(v*1e6) / 1e6 }

// weightedAverage: (base·avg + qty·cost) / (base + qty). Un saldo negativo o un promedio desconocido no se mezclan: queda cost.
func weightedAverage(balance float64, avg *float64, qty, cost float64) float64 {
	if avg == nil || balance <= 0 {
		return round6(cost)
	}
	num := new(big.Rat).Mul(new(big.Rat).SetFloat64(balance), new(big.Rat).SetFloat64(*avg))
	num.Add(num, new(big.Rat).Mul(new(big.Rat).SetFloat64(qty), new(big.Rat).SetFloat64(cost)))
	out, _ := num.Quo(num, new(big.Rat).SetFloat64(balance+qty)).Float64()
	return round6(out)
}

// supplyCost es lo que un movimiento de insumo deja escrito.
type supplyCost struct {
	unitCost  float64
	unknown   bool
	applied   bool     // este movimiento cambió el promedio
	avgBefore *float64 // promedio antes de aplicarlo
	newAvg    *float64
	setAvg    bool
	undone    uint // movimiento cuyo efecto sobre el promedio se deshizo
}

// currentRead hace que una lectura vea lo último confirmado (y no la foto de la transacción) en MySQL/MariaDB.
func currentRead(tx *gorm.DB, q *gorm.DB) *gorm.DB {
	if forUpdateSupported(tx) && !rowLockingDisabled() {
		return q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	return q
}

func readAvgCost(tx *gorm.DB, productID, branchID uint) (*float64, error) {
	var rows []struct{ AvgUnitCost *float64 }
	q := tx.Table("tenant_product_stocks").Select("avg_unit_cost").Where("product_id = ? AND branch_id = ?", productID, branchID).
		Order("id").Limit(1)
	if err := currentRead(tx, q).Find(&rows).Error; err != nil || len(rows) == 0 {
		return nil, err
	}
	return rows[0].AvgUnitCost, nil
}

// planSupplyCost decide el costo del movimiento y el promedio resultante. balance es el saldo ANTES del movimiento.
func planSupplyCost(tx *gorm.DB, input MovementInput, balance float64) (supplyCost, error) {
	var out supplyCost
	avg, err := readAvgCost(tx, input.ProductID, input.BranchID)
	if err != nil {
		return out, err
	}
	atAverage := func() {
		if avg == nil {
			out.unknown = true
			return
		}
		out.unitCost = *avg
	}
	apply := func(cost float64) {
		next := weightedAverage(balance, avg, input.Quantity, cost)
		out.unitCost, out.applied, out.avgBefore, out.newAvg, out.setAvg = round6(cost), true, avg, &next, true
	}
	inbound := input.Type == "in" || input.Type == "adjustment_in"
	outbound := input.Type == "out" || input.Type == "adjustment_out"

	if input.ReversesMovementID != nil {
		var orig struct {
			ID          uint
			UnitCost    float64
			AvgBefore   *float64
			CostApplied bool
			CostUnknown bool
		}
		if err := currentRead(tx, tx.Table("tenant_stock_movements").
			Select("id", "unit_cost", "avg_before", "cost_applied", "cost_unknown").
			Where("id = ?", *input.ReversesMovementID)).Take(&orig).Error; err != nil {
			return out, errors.New("no se encontró el movimiento que se anula")
		}
		switch {
		case inbound && !orig.CostUnknown && orig.UnitCost > 0:
			apply(orig.UnitCost) // vuelve lo que salió, a su costo
		case outbound:
			out.unitCost, out.unknown = orig.UnitCost, orig.CostUnknown
			if !orig.CostApplied {
				return out, nil
			}
			// Se deshace una entrada: el promedio vuelve al de antes solo si ninguna entrada posterior lo cambió.
			var later []uint
			q := tx.Table("tenant_stock_movements").Select("id").
				Where("product_id = ? AND branch_id = ? AND id > ? AND cost_applied = ?", input.ProductID, input.BranchID, orig.ID, true).Limit(1)
			if err := currentRead(tx, q).Find(&later).Error; err != nil {
				return out, err
			}
			if len(later) == 0 {
				out.newAvg, out.setAvg, out.undone = orig.AvgBefore, true, orig.ID
			}
		default:
			atAverage()
		}
		return out, nil
	}

	if inbound && input.UnitCost > 0 {
		apply(input.UnitCost)
		return out, nil
	}
	// Salidas, ajustes a un saldo y entradas sin costo: se valorizan al promedio y no lo cambian. Lo digitado se ignora.
	atAverage()
	return out, nil
}

// saveSupplyCost escribe el resultado en el movimiento ya creado y en la fila de saldo.
func saveSupplyCost(tx *gorm.DB, movementID, productID, branchID uint, c supplyCost) error {
	if err := tx.Table("tenant_stock_movements").Where("id = ?", movementID).Updates(map[string]interface{}{
		"unit_cost": c.unitCost, "cost_unknown": c.unknown, "cost_applied": c.applied, "avg_before": c.avgBefore,
	}).Error; err != nil {
		return err
	}
	if c.undone > 0 {
		if err := tx.Table("tenant_stock_movements").Where("id = ?", c.undone).Update("cost_applied", false).Error; err != nil {
			return err
		}
	}
	if !c.setAvg {
		return nil
	}
	return tx.Table("tenant_product_stocks").Where("product_id = ? AND branch_id = ?", productID, branchID).
		Update("avg_unit_cost", c.newAvg).Error
}
