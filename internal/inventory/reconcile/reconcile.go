// Package reconcile audita, SOLO EN LECTURA, la coherencia entre el saldo de stock (tenant_product_stocks y
// tenant_product_presentation_stocks) y el kardex (tenant_stock_movements) de UN tenant.
//
// Es la fase F0a-1 del endurecimiento de inventario (docs/recetas/F0a_DIAGNOSTICO_Y_PROPUESTA.md): no escribe nada, no
// consolida duplicados y no corrige diferencias; solo las detecta y las informa. Todas las consultas son SELECT crudos.
//
// Semántica de los movimientos (inventory_service.go, RecordMovementTx):
//   - "in" y "adjustment_in" suman Quantity; "out" y "adjustment_out" la restan.
//   - "adjustment" FIJA el saldo: su Quantity es el saldo ABSOLUTO resultante, no un delta. La cadena se reinicia ahí.
//   - Balance es el saldo resultante de cada movimiento (la cadena esperada es prev ± q, o q en un "adjustment").
package reconcile

import (
	"database/sql"
	"fmt"
	"math"
	"sort"

	"gorm.io/gorm"
)

// Tolerance: Quantity y Balance son decimal(15,3); medio milésimo evita falsos positivos por redondeo.
const Tolerance = 0.0005

// Tipos de entidad.
const (
	KindProduct      = "product"      // tenant_product_stocks (clave producto+sucursal)
	KindPresentation = "presentation" // tenant_product_presentation_stocks (clave presentación+sucursal)
)

// Categorías de diferencia entre saldo y kardex.
const (
	DiffMismatch      = "DIFERENCIA"        // el saldo no coincide con el último Balance del kardex
	DiffStockNoKardex = "STOCK_SIN_KARDEX"  // saldo distinto de 0 y ningún movimiento (stock previo al kardex)
	DiffKardexNoStock = "KARDEX_SIN_STOCK"  // hay movimientos y no hay fila de saldo
	DiffNegative      = "SALDO_NEGATIVO"    // saldo < 0
	DiffZeroKeyRows   = "CLAVE_NULA_O_CERO" // fila con product_id/presentation_id/branch_id nulo o 0
)

// Options acota el alcance (0 = sin filtro) y el detalle conservado en memoria.
type Options struct {
	ProductID uint
	BranchID  uint
	// MaxDetail: máximo de filas de detalle por categoría que se conservan (0 = todas). Los conteos del resumen son siempre exactos.
	MaxDetail int
}

// DuplicateRow es una fila de saldo dentro de un grupo duplicado.
type DuplicateRow struct {
	ID       uint    `json:"id"`
	Quantity float64 `json:"quantity"`
}

// Duplicate: más de una fila de saldo para la misma clave (entidad + sucursal). Hoy no hay UNIQUE que lo impida.
type Duplicate struct {
	Kind     string         `json:"kind"`
	EntityID uint           `json:"entity_id"`
	BranchID uint           `json:"branch_id"`
	Rows     []DuplicateRow `json:"rows"`
	Sum      float64        `json:"sum"`
	// WriterRowID: la fila que RecordMovementTx actualiza (la de menor id, porque First ordena por clave primaria).
	WriterRowID uint `json:"writer_row_id"`
	// KardexBalance: último Balance del kardex para esa clave (nil si no hay movimientos).
	KardexBalance *float64 `json:"kardex_balance,omitempty"`
}

// Diff: diferencia entre el saldo de una clave y su kardex.
type Diff struct {
	Category       string   `json:"category"`
	Kind           string   `json:"kind"`
	EntityID       uint     `json:"entity_id"`
	BranchID       uint     `json:"branch_id"`
	Stock          *float64 `json:"stock,omitempty"`
	StockSum       *float64 `json:"stock_sum,omitempty"` // solo si hay filas duplicadas
	KardexBalance  *float64 `json:"kardex_balance,omitempty"`
	Delta          float64  `json:"delta"`
	LastMovementID uint     `json:"last_movement_id,omitempty"`
	Movements      int      `json:"movements"`
}

// ChainBreak: un movimiento cuyo Balance no coincide con el esperado a partir del anterior (señal de actualización perdida o de
// una escritura fuera de RecordMovementTx).
type ChainBreak struct {
	Kind       string  `json:"kind"`
	EntityID   uint    `json:"entity_id"`
	BranchID   uint    `json:"branch_id"`
	MovementID uint    `json:"movement_id"`
	Type       string  `json:"type"`
	Quantity   float64 `json:"quantity"`
	Previous   float64 `json:"previous_balance"`
	Expected   float64 `json:"expected_balance"`
	Stored     float64 `json:"stored_balance"`
}

// Summary: conteos exactos (no truncados por MaxDetail).
type Summary struct {
	ProductStockRows      int `json:"product_stock_rows"`
	PresentationStockRows int `json:"presentation_stock_rows"`
	Movements             int `json:"movements"`
	DuplicateGroups       int `json:"duplicate_groups"`
	DuplicateExtraRows    int `json:"duplicate_extra_rows"`
	ZeroOrNullKeyRows     int `json:"zero_or_null_key_rows"`
	Mismatches            int `json:"mismatches"`
	StockWithoutKardex    int `json:"stock_without_kardex"`
	KardexWithoutStock    int `json:"kardex_without_stock"`
	NegativeBalances      int `json:"negative_balances"`
	ChainBreaks           int `json:"chain_breaks"`
	// InitialOffsets: claves cuyo primer movimiento no parte de saldo 0 (stock previo al kardex). Informativo, no es un error.
	InitialOffsets int `json:"initial_offsets"`
	// UnknownTypeMovements: movimientos con un tipo que la cadena no entiende (no se evalúan).
	UnknownTypeMovements int `json:"unknown_type_movements"`
}

// Report es el resultado de auditar un tenant.
type Report struct {
	Skipped     string       `json:"skipped,omitempty"` // motivo si no se pudo auditar (tablas ausentes)
	Summary     Summary      `json:"summary"`
	Duplicates  []Duplicate  `json:"duplicates,omitempty"`
	Diffs       []Diff       `json:"diffs,omitempty"`
	ChainBreaks []ChainBreak `json:"chain_breaks,omitempty"`
	Truncated   bool         `json:"detail_truncated,omitempty"`
}

type key struct {
	kind     string
	entity   uint
	branchID uint
}

type stockRow struct {
	id  uint
	qty float64
}

type chainAcc struct {
	count       int
	prev        float64
	lastID      uint
	lastBalance float64
}

// Run audita un tenant. db debe apuntar a la BD del tenant y NO se le escribe nada. Devuelve un reporte aunque haya diferencias
// (encontrarlas no es un error); solo devuelve error ante fallas de lectura.
func Run(db *gorm.DB, opts Options) (*Report, error) {
	rep := &Report{}
	for _, t := range []string{"tenant_product_stocks", "tenant_stock_movements"} {
		if !tableExists(db, t) {
			rep.Skipped = "falta la tabla " + t
			return rep, nil
		}
	}
	hasPresStock := tableExists(db, "tenant_product_presentation_stocks")

	// 1) Saldos.
	stocks := map[key][]stockRow{}
	if err := loadStocks(db, KindProduct, "tenant_product_stocks", "product_id", opts, stocks, rep); err != nil {
		return nil, err
	}
	if hasPresStock {
		if err := loadStocks(db, KindPresentation, "tenant_product_presentation_stocks", "presentation_id", opts, stocks, rep); err != nil {
			return nil, err
		}
	}

	// 2) Kardex: una sola pasada en orden de id, memoria proporcional a las claves (no a los movimientos).
	acc := map[key]*chainAcc{}
	if err := streamMovements(db, opts, acc, rep); err != nil {
		return nil, err
	}

	// 3) Duplicados.
	for k, rows := range stocks {
		if len(rows) < 2 {
			continue
		}
		sort.Slice(rows, func(i, j int) bool { return rows[i].id < rows[j].id })
		d := Duplicate{Kind: k.kind, EntityID: k.entity, BranchID: k.branchID, WriterRowID: rows[0].id}
		for _, r := range rows {
			d.Rows = append(d.Rows, DuplicateRow{ID: r.id, Quantity: r.qty})
			d.Sum += r.qty
		}
		if a := acc[k]; a != nil {
			b := a.lastBalance
			d.KardexBalance = &b
		}
		rep.Summary.DuplicateGroups++
		rep.Summary.DuplicateExtraRows += len(rows) - 1
		keepDetail(&rep.Duplicates, d, opts.MaxDetail, &rep.Truncated, rep.Summary.DuplicateGroups)
	}

	// 4) Saldo vs kardex.
	keys := make([]key, 0, len(stocks)+len(acc))
	seen := map[key]bool{}
	for k := range stocks {
		keys = append(keys, k)
		seen[k] = true
	}
	for k := range acc {
		if !seen[k] {
			keys = append(keys, k)
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := keys[i], keys[j]
		if a.kind != b.kind {
			return a.kind < b.kind
		}
		if a.entity != b.entity {
			return a.entity < b.entity
		}
		return a.branchID < b.branchID
	})
	counts := map[string]int{}
	for _, k := range keys {
		rows := stocks[k]
		a := acc[k]
		switch {
		case len(rows) > 0 && a == nil:
			q := writerQty(rows)
			if math.Abs(q) > Tolerance {
				rep.Summary.StockWithoutKardex++
				counts[DiffStockNoKardex]++
				keepDetail(&rep.Diffs, Diff{Category: DiffStockNoKardex, Kind: k.kind, EntityID: k.entity, BranchID: k.branchID,
					Stock: ptr(q), Delta: q, StockSum: sumIfDup(rows)}, opts.MaxDetail, &rep.Truncated, counts[DiffStockNoKardex])
			}
		case len(rows) == 0 && a != nil:
			if math.Abs(a.lastBalance) > Tolerance {
				rep.Summary.KardexWithoutStock++
				counts[DiffKardexNoStock]++
				keepDetail(&rep.Diffs, Diff{Category: DiffKardexNoStock, Kind: k.kind, EntityID: k.entity, BranchID: k.branchID,
					KardexBalance: ptr(a.lastBalance), Delta: -a.lastBalance, LastMovementID: a.lastID, Movements: a.count}, opts.MaxDetail, &rep.Truncated, counts[DiffKardexNoStock])
			}
		case len(rows) > 0 && a != nil:
			q := writerQty(rows)
			if d := q - a.lastBalance; math.Abs(d) > Tolerance {
				rep.Summary.Mismatches++
				counts[DiffMismatch]++
				keepDetail(&rep.Diffs, Diff{Category: DiffMismatch, Kind: k.kind, EntityID: k.entity, BranchID: k.branchID,
					Stock: ptr(q), StockSum: sumIfDup(rows), KardexBalance: ptr(a.lastBalance), Delta: round3(d),
					LastMovementID: a.lastID, Movements: a.count}, opts.MaxDetail, &rep.Truncated, counts[DiffMismatch])
			}
		}
		if len(rows) > 0 {
			if q := writerQty(rows); q < -Tolerance {
				rep.Summary.NegativeBalances++
				counts[DiffNegative]++
				keepDetail(&rep.Diffs, Diff{Category: DiffNegative, Kind: k.kind, EntityID: k.entity, BranchID: k.branchID,
					Stock: ptr(q), Delta: q}, opts.MaxDetail, &rep.Truncated, counts[DiffNegative])
			}
		}
	}
	return rep, nil
}

// tableExists sondea la tabla con un SELECT. No usa db.Migrator(): con una conexión fijada en solo lectura (db.Connection) el migrador
// de GORM entra en pánico al consultar la BD actual (visto en MariaDB 10.4), y esta auditoría corre justo en ese modo.
func tableExists(db *gorm.DB, table string) bool {
	rows, err := db.Raw("SELECT 1 FROM " + table + " LIMIT 1").Rows()
	if err != nil {
		return false
	}
	_ = rows.Close()
	return true
}

// writerQty: el saldo de la fila que RecordMovementTx lee y actualiza (la de menor id).
func writerQty(rows []stockRow) float64 {
	best := rows[0]
	for _, r := range rows[1:] {
		if r.id < best.id {
			best = r
		}
	}
	return best.qty
}

func sumIfDup(rows []stockRow) *float64 {
	if len(rows) < 2 {
		return nil
	}
	s := 0.0
	for _, r := range rows {
		s += r.qty
	}
	return ptr(round3(s))
}

func loadStocks(db *gorm.DB, kind, table, col string, opts Options, out map[key][]stockRow, rep *Report) error {
	q := fmt.Sprintf("SELECT id, %s, branch_id, quantity FROM %s", col, table)
	var args []any
	where := ""
	if kind == KindProduct && opts.ProductID != 0 {
		where += " AND product_id = ?"
		args = append(args, opts.ProductID)
	}
	if opts.BranchID != 0 {
		where += " AND branch_id = ?"
		args = append(args, opts.BranchID)
	}
	if where != "" {
		q += " WHERE 1=1" + where
	}
	q += " ORDER BY id"
	rows, err := db.Raw(q, args...).Rows()
	if err != nil {
		return fmt.Errorf("leer %s: %w", table, err)
	}
	defer rows.Close()
	for rows.Next() {
		var id uint
		var entity, branch sql.NullInt64
		var qty sql.NullFloat64
		if err := rows.Scan(&id, &entity, &branch, &qty); err != nil {
			return fmt.Errorf("leer %s: %w", table, err)
		}
		if kind == KindProduct {
			rep.Summary.ProductStockRows++
		} else {
			rep.Summary.PresentationStockRows++
		}
		if !entity.Valid || entity.Int64 == 0 || !branch.Valid || branch.Int64 == 0 {
			rep.Summary.ZeroOrNullKeyRows++
			continue
		}
		k := key{kind: kind, entity: uint(entity.Int64), branchID: uint(branch.Int64)}
		out[k] = append(out[k], stockRow{id: id, qty: qty.Float64})
	}
	return rows.Err()
}

func streamMovements(db *gorm.DB, opts Options, acc map[key]*chainAcc, rep *Report) error {
	q := "SELECT id, product_id, presentation_id, branch_id, type, quantity, balance FROM tenant_stock_movements"
	var args []any
	where := ""
	if opts.ProductID != 0 {
		where += " AND product_id = ?"
		args = append(args, opts.ProductID)
	}
	if opts.BranchID != 0 {
		where += " AND branch_id = ?"
		args = append(args, opts.BranchID)
	}
	if where != "" {
		q += " WHERE 1=1" + where
	}
	q += " ORDER BY id"
	rows, err := db.Raw(q, args...).Rows()
	if err != nil {
		return fmt.Errorf("leer tenant_stock_movements: %w", err)
	}
	defer rows.Close()
	breaks := 0
	for rows.Next() {
		var id uint
		var product, pres, branch sql.NullInt64
		var typ sql.NullString
		var qty, bal sql.NullFloat64
		if err := rows.Scan(&id, &product, &pres, &branch, &typ, &qty, &bal); err != nil {
			return fmt.Errorf("leer tenant_stock_movements: %w", err)
		}
		rep.Summary.Movements++
		k := key{kind: KindProduct, entity: uint(product.Int64), branchID: uint(branch.Int64)}
		if pres.Valid && pres.Int64 != 0 {
			k = key{kind: KindPresentation, entity: uint(pres.Int64), branchID: uint(branch.Int64)}
		}
		a := acc[k]
		if a == nil {
			a = &chainAcc{}
			acc[k] = a
		}
		expected, known := expectedBalance(typ.String, a.prev, qty.Float64)
		if !known {
			rep.Summary.UnknownTypeMovements++
		} else if a.count == 0 {
			// Primer movimiento de la clave: si no parte de saldo 0 hay stock previo al kardex (informativo).
			if math.Abs(bal.Float64-expected) > Tolerance {
				rep.Summary.InitialOffsets++
			}
		} else if math.Abs(bal.Float64-expected) > Tolerance {
			rep.Summary.ChainBreaks++
			breaks++
			keepDetail(&rep.ChainBreaks, ChainBreak{Kind: k.kind, EntityID: k.entity, BranchID: k.branchID, MovementID: id,
				Type: typ.String, Quantity: qty.Float64, Previous: a.prev, Expected: round3(expected), Stored: bal.Float64},
				opts.MaxDetail, &rep.Truncated, breaks)
		}
		// Se continúa desde el saldo GUARDADO: cada divergencia se cuenta una sola vez en vez de arrastrarse.
		a.prev = bal.Float64
		a.lastBalance = bal.Float64
		a.lastID = id
		a.count++
	}
	return rows.Err()
}

// expectedBalance: saldo esperado tras un movimiento. known=false si el tipo no es de los que la cadena entiende.
func expectedBalance(typ string, prev, qty float64) (float64, bool) {
	switch typ {
	case "in", "adjustment_in":
		return prev + qty, true
	case "out", "adjustment_out":
		return prev - qty, true
	case "adjustment":
		return qty, true // saldo absoluto
	}
	return 0, false
}

func keepDetail[T any](dst *[]T, v T, max int, truncated *bool, countSoFar int) {
	if max > 0 && countSoFar > max {
		*truncated = true
		return
	}
	*dst = append(*dst, v)
}

func ptr(v float64) *float64 { return &v }

func round3(v float64) float64 { return math.Round(v*1000) / 1000 }
