package tenantmigrations

import (
	"fmt"
	"math/big"

	"gorm.io/gorm"
)

type v196Stock struct {
	AvgUnitCost *float64 `gorm:"column:avg_unit_cost;type:decimal(15,6)"`
}

func (v196Stock) TableName() string { return "tenant_product_stocks" }

type v196Movement struct {
	AvgBefore   *float64 `gorm:"column:avg_before;type:decimal(15,6)"`
	CostApplied bool     `gorm:"column:cost_applied;not null;default:false"`
	CostUnknown bool     `gorm:"column:cost_unknown;not null;default:false"`
}

func (v196Movement) TableName() string { return "tenant_stock_movements" }

type v196TransferLog struct {
	UnitCost *float64 `gorm:"column:unit_cost;type:decimal(15,6)"`
}

func (v196TransferLog) TableName() string { return "tenant_transfer_logs" }

// V196SupplyCosts (F2b) agrega el costo promedio de insumos. Solo columnas nuevas (anulables o con valor por defecto); los productos
// normales no las usan. Calcula el promedio de los insumos que ya tuvieran movimientos. Idempotente.
type V196SupplyCosts struct{}

func (V196SupplyCosts) Version() int { return 196 }
func (V196SupplyCosts) Name() string { return "supply_costs" }

func (V196SupplyCosts) Up(db *gorm.DB) error {
	mig := db.Migrator()
	add := func(table string, model interface{}, fields ...string) error {
		if !mig.HasTable(table) {
			return nil
		}
		for _, f := range fields {
			if !mig.HasColumn(model, f) {
				if err := mig.AddColumn(model, f); err != nil {
					return fmt.Errorf("add %s.%s: %w", table, f, err)
				}
			}
		}
		return nil
	}
	if err := add("tenant_product_stocks", &v196Stock{}, "AvgUnitCost"); err != nil {
		return err
	}
	if err := add("tenant_stock_movements", &v196Movement{}, "AvgBefore", "CostApplied", "CostUnknown"); err != nil {
		return err
	}
	if err := add("tenant_transfer_logs", &v196TransferLog{}, "UnitCost"); err != nil {
		return err
	}
	return v196Backfill(db)
}

// v196Backfill recorre el kardex de cada insumo: las entradas con costo ponderan; lo demás solo mueve el saldo.
func v196Backfill(db *gorm.DB) error {
	mig := db.Migrator()
	if !mig.HasTable("tenant_stock_movements") || !mig.HasTable("tenant_product_stocks") || !mig.HasTable("tenant_products") ||
		!mig.HasColumn("tenant_products", "is_supply") {
		return nil
	}
	var pairs []struct {
		ProductID uint
		BranchID  uint
	}
	if err := db.Raw(`SELECT s.product_id, s.branch_id FROM tenant_product_stocks s
		JOIN tenant_products p ON p.id = s.product_id
		WHERE p.is_supply = ? AND s.avg_unit_cost IS NULL`, true).Scan(&pairs).Error; err != nil {
		return fmt.Errorf("insumos con saldo: %w", err)
	}
	for _, pair := range pairs {
		var movs []struct {
			Type     string
			Quantity float64
			UnitCost float64
			Balance  float64
		}
		if err := db.Raw(`SELECT type, quantity, unit_cost, balance FROM tenant_stock_movements
			WHERE product_id = ? AND branch_id = ? ORDER BY id`, pair.ProductID, pair.BranchID).Scan(&movs).Error; err != nil {
			return fmt.Errorf("kardex del insumo %d: %w", pair.ProductID, err)
		}
		var qty float64
		var avg *float64
		for _, m := range movs {
			if (m.Type == "in" || m.Type == "adjustment_in") && m.UnitCost > 0 {
				base := qty
				if base < 0 || avg == nil {
					base = 0
				}
				next := m.UnitCost
				if base > 0 {
					num := new(big.Rat).Mul(new(big.Rat).SetFloat64(base), new(big.Rat).SetFloat64(*avg))
					num.Add(num, new(big.Rat).Mul(new(big.Rat).SetFloat64(m.Quantity), new(big.Rat).SetFloat64(m.UnitCost)))
					next, _ = num.Quo(num, new(big.Rat).SetFloat64(base+m.Quantity)).Float64()
				}
				avg = &next
			}
			qty = m.Balance
		}
		if avg == nil {
			continue
		}
		if err := db.Exec(`UPDATE tenant_product_stocks SET avg_unit_cost = ? WHERE product_id = ? AND branch_id = ?`,
			*avg, pair.ProductID, pair.BranchID).Error; err != nil {
			return fmt.Errorf("promedio del insumo %d: %w", pair.ProductID, err)
		}
	}
	return nil
}
