package service

// F0a-3: bloqueo de la fila de saldo, creación segura, orden estable de bloqueo y reintento de operaciones de inventario.
// Ver docs/recetas/F0a_DIAGNOSTICO_Y_PROPUESTA.md y F0a-3_INFORME.md.

import (
	"errors"
	"fmt"
	"math/rand"
	"os"
	"sort"
	"strings"
	"time"

	"tukifac/pkg/database"

	mysqlerr "github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ErrInsufficientStock identifica el rechazo por falta de stock. El texto que ve el usuario no cambia.
var ErrInsufficientStock = errors.New("stock insuficiente")

const maxInventoryTxAttempts = 3

// rowLockingDisabled: interruptor de emergencia. INVENTORY_ROW_LOCKS=off vuelve al comportamiento anterior (sin bloqueo, sin
// creación anticipada de la fila, sin pre-bloqueo ni reintentos) sin migrar nada.
func rowLockingDisabled() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("INVENTORY_ROW_LOCKS")), "off")
}

// forUpdateSupported: SELECT … FOR UPDATE solo en MySQL/MariaDB (SQLite, usado en pruebas, no lo tiene).
func forUpdateSupported(tx *gorm.DB) bool { return tx.Dialector.Name() == "mysql" }

type stockRowState struct {
	ID       uint
	Quantity float64
}

type stockTarget struct {
	byPresentation bool
	entityID       uint // product_id o presentation_id
	branchID       uint
}

func (t stockTarget) model() interface{} {
	if t.byPresentation {
		return &database.TenantProductPresentationStock{}
	}
	return &database.TenantProductStock{}
}

func (t stockTarget) column() string {
	if t.byPresentation {
		return "presentation_id"
	}
	return "product_id"
}

// readStockRow lee la fila de saldo de menor id (la misma que se actualiza). Con lock=true usa FOR UPDATE.
func readStockRow(tx *gorm.DB, t stockTarget, lock bool) (stockRowState, bool, error) {
	q := tx.Model(t.model()).Select("id", "quantity").
		Where(t.column()+" = ? AND branch_id = ?", t.entityID, t.branchID).Order("id").Limit(1)
	if lock && forUpdateSupported(tx) {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var rows []stockRowState
	if err := q.Find(&rows).Error; err != nil {
		return stockRowState{}, false, err
	}
	if len(rows) == 0 {
		return stockRowState{}, false, nil
	}
	return rows[0], true, nil
}

// lockStockRowByID bloquea exactamente esa fila (por clave primaria: no toma bloqueos de rango, funcione o no el índice único) y
// devuelve su saldo MÁS RECIENTE confirmado. Una lectura simple, bajo REPEATABLE READ, vería la foto de la transacción.
func lockStockRowByID(tx *gorm.DB, t stockTarget, id uint) (stockRowState, error) {
	q := tx.Model(t.model()).Select("id", "quantity").Where("id = ?", id)
	if forUpdateSupported(tx) {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var rows []stockRowState
	if err := q.Find(&rows).Error; err != nil {
		return stockRowState{}, err
	}
	if len(rows) == 0 {
		return stockRowState{}, fmt.Errorf("la fila de saldo %d ya no existe", id)
	}
	return rows[0], nil
}

// acquireStockRow devuelve la fila de saldo BLOQUEADA, creándola en 0 si no existe. La creación es segura: con el índice único,
// dos transacciones simultáneas terminan en la misma fila (insert que ignora el conflicto + lectura con bloqueo).
// Con el interruptor apagado devuelve exactamente lo de antes: lectura simple y, si no hay fila, found=false.
func acquireStockRow(tx *gorm.DB, t stockTarget) (stockRowState, bool, error) {
	row, found, err := readStockRow(tx, t, false)
	if err != nil {
		return stockRowState{}, false, err
	}
	if rowLockingDisabled() {
		return row, found, nil
	}
	if found {
		locked, err := lockStockRowByID(tx, t, row.ID)
		return locked, err == nil, err
	}

	// Se serializa la creación bloqueando la fila PADRE (producto o presentación): así funciona también en tenants sin el índice
	// único. Solo ocurre la primera vez que un producto toca una sucursal.
	if err := lockStockParent(tx, t); err != nil {
		return stockRowState{}, false, err
	}
	if row, found, err = readStockRow(tx, t, true); err != nil || found {
		return row, found, err
	}

	var seed interface{}
	if t.byPresentation {
		seed = &database.TenantProductPresentationStock{PresentationID: t.entityID, BranchID: t.branchID}
	} else {
		seed = &database.TenantProductStock{ProductID: t.entityID, BranchID: t.branchID}
	}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(seed).Error; err != nil {
		return stockRowState{}, false, err
	}
	row, found, err = readStockRow(tx, t, true) // lectura con bloqueo: ve también una fila recién confirmada por otra transacción
	if err != nil {
		return stockRowState{}, false, err
	}
	if !found {
		// Hay una fila para esa clave pero no es visible (p. ej. fuera de la sucursal de la sesión): fallar es mejor que crear otra.
		return stockRowState{}, false, errors.New("no se pudo preparar el saldo de stock de esa sucursal")
	}
	return row, true, nil
}

func lockStockParent(tx *gorm.DB, t stockTarget) error {
	if !forUpdateSupported(tx) {
		return nil
	}
	var parent interface{} = &database.TenantProduct{}
	if t.byPresentation {
		parent = &database.TenantProductPresentation{}
	}
	var ids []uint
	return tx.Unscoped().Model(parent).Select("id").Where("id = ?", t.entityID).
		Clauses(clause.Locking{Strength: "UPDATE"}).Find(&ids).Error
}

// StockKey identifica el saldo que un movimiento va a tocar.
type StockKey struct {
	ProductID      uint
	PresentationID *uint
	BranchID       uint
}

func (k StockKey) target() stockTarget {
	if k.PresentationID != nil && *k.PresentationID > 0 {
		return stockTarget{byPresentation: true, entityID: *k.PresentationID, branchID: k.BranchID}
	}
	return stockTarget{entityID: k.ProductID, branchID: k.BranchID}
}

// LockStockRowsTx bloquea de antemano, SIEMPRE EN EL MISMO ORDEN, las filas de saldo que una operación de varias líneas va a
// mover: así dos operaciones con las mismas líneas en distinto orden se esperan en vez de bloquearse mutuamente. No crea filas,
// no cambia ningún dato ni la lógica del llamador; las filas que aún no existen se crean al mover. Solo en MySQL/MariaDB.
func LockStockRowsTx(tx *gorm.DB, keys []StockKey) error {
	if len(keys) == 0 || rowLockingDisabled() || !forUpdateSupported(tx) {
		return nil
	}
	targets := sortedTargets(keys)
	for _, t := range targets {
		row, found, err := readStockRow(tx, t, false)
		if err != nil {
			return err
		}
		if !found {
			continue
		}
		if _, err := lockStockRowByID(tx, t, row.ID); err != nil {
			return err
		}
	}
	return nil
}

// sortedTargets deduplica y ordena por (tipo, sucursal, entidad).
func sortedTargets(keys []StockKey) []stockTarget {
	seen := map[stockTarget]bool{}
	var out []stockTarget
	for _, k := range keys {
		t := k.target()
		if t.entityID == 0 || t.branchID == 0 || seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.byPresentation != b.byPresentation {
			return !a.byPresentation
		}
		if a.branchID != b.branchID {
			return a.branchID < b.branchID
		}
		return a.entityID < b.entityID
	})
	return out
}

// isLockConflict: deadlock (1213) o espera de bloqueo agotada (1205).
func isLockConflict(err error) bool {
	var me *mysqlerr.MySQLError
	if errors.As(err, &me) {
		return me.Number == 1213 || me.Number == 1205
	}
	return false
}

// runInventoryTx ejecuta una operación AUTOCONTENIDA de inventario en su transacción y la reintenta completa ante un deadlock o
// una espera agotada. NO usar en ventas, cobros, compras ni nada con dinero: su estado en memoria no admite repetirse.
// Si db ya está dentro de una transacción no reintenta (reintentar solo una parte sería incorrecto).
func runInventoryTx(db *gorm.DB, fn func(tx *gorm.DB) error) error {
	if _, nested := db.Statement.ConnPool.(gorm.TxCommitter); nested || rowLockingDisabled() {
		return db.Transaction(fn)
	}
	return retryOnLockConflict(func() error { return db.Transaction(fn) })
}

func retryOnLockConflict(op func() error) error {
	var err error
	for attempt := 1; attempt <= maxInventoryTxAttempts; attempt++ {
		if err = op(); err == nil || !isLockConflict(err) {
			return err
		}
		if attempt < maxInventoryTxAttempts {
			time.Sleep(time.Duration(attempt*40+rand.Intn(40)) * time.Millisecond)
		}
	}
	return err
}
