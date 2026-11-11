package tenantmigrations

import (
	"gorm.io/gorm"
)

// V181PurchaseExchangeRate agrega tenant_purchases.exchange_rate (TC venta de las compras en USD).
// Compras existentes: moneda PEN y TC NULL — sin backfill. Idempotente.
type V181PurchaseExchangeRate struct{}

func (V181PurchaseExchangeRate) Version() int { return 181 }
func (V181PurchaseExchangeRate) Name() string { return "purchase_exchange_rate" }

func (V181PurchaseExchangeRate) Up(db *gorm.DB) error {
	if db.Migrator().HasColumn(&v180Purchase{}, "exchange_rate") {
		return nil
	}
	return db.Migrator().AddColumn(&v180Purchase{}, "ExchangeRate")
}
