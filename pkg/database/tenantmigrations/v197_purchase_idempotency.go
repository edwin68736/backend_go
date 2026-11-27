package tenantmigrations

import (
	"fmt"

	"gorm.io/gorm"
)

type v197Purchase struct {
	IdempotencyKey *string `gorm:"column:idempotency_key;size:64"`
}

func (v197Purchase) TableName() string { return "tenant_purchases" }

const idxPurchaseUserIdempotency = "uk_tenant_purchases_user_idempotency"

// V197PurchaseIdempotencyKey (F3a) agrega tenant_purchases.idempotency_key y UNIQUE(user_id, idempotency_key), igual que V150 en
// ventas: un doble toque o un reintento por mala señal no crea otra compra ni duplica el stock. Aditiva (columna anulable; las
// compras existentes quedan en NULL y no chocan). Idempotente.
type V197PurchaseIdempotencyKey struct{}

func (V197PurchaseIdempotencyKey) Version() int { return 197 }
func (V197PurchaseIdempotencyKey) Name() string { return "purchase_idempotency_key" }

func (V197PurchaseIdempotencyKey) Up(db *gorm.DB) error {
	mig := db.Migrator()
	if !mig.HasTable("tenant_purchases") {
		return nil
	}
	if !mig.HasColumn(&v197Purchase{}, "IdempotencyKey") {
		if err := mig.AddColumn(&v197Purchase{}, "IdempotencyKey"); err != nil {
			return fmt.Errorf("add tenant_purchases.idempotency_key: %w", err)
		}
	}
	if !mig.HasIndex("tenant_purchases", idxPurchaseUserIdempotency) {
		if err := db.Exec(fmt.Sprintf(
			`CREATE UNIQUE INDEX %s ON tenant_purchases (user_id, idempotency_key)`, idxPurchaseUserIdempotency,
		)).Error; err != nil {
			return fmt.Errorf("crear %s: %w", idxPurchaseUserIdempotency, err)
		}
	}
	return nil
}
