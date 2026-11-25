package service

// F0b: lo que se cobra de una mesa se cobra una sola vez. BillTable arma las líneas con comandas leídas FUERA de su transacción; aquí,
// ya con la sesión bloqueada, se vuelven a leer (bloqueadas) y se rechaza el cobro si algo cambió. Todo el cobro se revierte.

import (
	"errors"
	"math"
	"time"

	"tukifac/pkg/database"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// errBillStale: otra operación cobró, anuló o agregó mientras se cobraba.
var errBillStale = errors.New("La mesa cambió o ya se cobró. Actualiza e inténtalo de nuevo.")

// billAfterReadHook: solo para pruebas. Se ejecuta entre la lectura de las comandas y la transacción del cobro, para reproducir de
// forma determinista lo que otra caja hace en ese intervalo. nil en producción.
var billAfterReadHook func()

// verifyBillableTx comprueba, con las filas bloqueadas, que las comandas a cobrar siguen como se leyeron:
//   - existen, son de esta sesión y no están cobradas ni anuladas;
//   - una línea completa conserva su cantidad y precio; una parcial todavía deja un resto;
//   - si el cobro cierra la mesa, no hay comandas pendientes fuera de la selección (el cierre las borraría sin cobrarlas).
func verifyBillableTx(tx *gorm.DB, sessionID uint, comandas []database.TenantComanda, partialQty map[uint]float64, closing bool) error {
	ids := make([]uint, 0, len(comandas))
	for _, c := range comandas {
		ids = append(ids, c.ID)
	}
	var rows []database.TenantComanda
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id IN ?", ids).Order("id").Find(&rows).Error; err != nil {
		return err
	}
	current := make(map[uint]database.TenantComanda, len(rows))
	for _, r := range rows {
		current[r.ID] = r
	}
	for _, c := range comandas {
		now, ok := current[c.ID]
		if !ok || now.SessionID != sessionID || now.BilledAt != nil || now.CancelledAt != nil || math.Abs(now.UnitPrice-c.UnitPrice) > 0.005 {
			return errBillStale
		}
		if billed, partial := partialQty[c.ID]; partial {
			if now.Quantity-billed <= comandaQtyEpsilon {
				return errBillStale
			}
		} else if math.Abs(now.Quantity-c.Quantity) > comandaQtyEpsilon {
			return errBillStale
		}
	}
	if !closing {
		return nil
	}
	var others []uint
	if err := tx.Model(&database.TenantComanda{}).Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("session_id = ? AND cancelled_at IS NULL AND billed_at IS NULL AND id NOT IN ?", sessionID, ids).
		Pluck("id", &others).Error; err != nil {
		return err
	}
	if len(others) > 0 {
		return errBillStale
	}
	return nil
}

// markBilledTx marca las comandas cobradas SOLO si siguen sin cobrar ni anular.
func markBilledTx(tx *gorm.DB, ids []uint, when time.Time) error {
	res := tx.Model(&database.TenantComanda{}).
		Where("id IN ? AND billed_at IS NULL AND cancelled_at IS NULL", ids).
		Update("billed_at", when)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected != int64(len(ids)) {
		return errBillStale
	}
	return nil
}
