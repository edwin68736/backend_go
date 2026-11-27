package tenantmigrations

import "gorm.io/gorm"

// V193StockUniqueIndexes crea el índice UNIQUE de (producto, sucursal) y (presentación, sucursal) en los saldos de stock.
// Si el tenant tiene duplicados, NO crea el índice: lo registra en el log y termina sin error (no consolida ni modifica saldos).
// Como una migración versionada corre una sola vez, para crear el índice después de limpiar duplicados está el comando
// `stock-unique-index --apply`. Ver docs/recetas/F0a_DIAGNOSTICO_Y_PROPUESTA.md.
type V193StockUniqueIndexes struct{}

func (V193StockUniqueIndexes) Version() int { return 193 }
func (V193StockUniqueIndexes) Name() string { return "stock_unique_indexes" }

func (V193StockUniqueIndexes) Up(db *gorm.DB) error {
	statuses, err := EnsureStockUniqueIndexes(db, true)
	if err != nil {
		return err
	}
	logStockUniqueStatus(db, statuses)
	return nil
}
