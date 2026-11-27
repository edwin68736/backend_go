package service

import (
	"errors"
	"strings"

	"tukifac/pkg/database"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Protección contra compras repetidas (F3a). Solo actúa cuando el llamador la pide; quien no envía clave se comporta como siempre.

// ErrIdempotentReplay: esa clave ya registró una compra; se devuelve la existente.
var ErrIdempotentReplay = errors.New("la compra de este intento ya fue registrada")

// ErrDuplicateDocument: el proveedor ya tiene una compra vigente con esa serie y número.
var ErrDuplicateDocument = errors.New("ya hay una compra registrada de ese proveedor con la misma serie y número")

const maxIdempotencyKeyLen = 64

const idempotencyColumnProbe = "col:tenant_purchases.idempotency_key"

// IdempotencyReady: la BD del tenant tiene la columna de V197.
func IdempotencyReady(db *gorm.DB) bool {
	return database.CachedSchemaProbe(db, idempotencyColumnProbe, func() bool {
		return db.Migrator().HasColumn("tenant_purchases", "idempotency_key")
	})
}

func normalizeIdempotencyKey(raw string) string {
	k := strings.TrimSpace(raw)
	if len(k) > maxIdempotencyKeyLen {
		return ""
	}
	return k
}

func findByIdempotencyKey(db *gorm.DB, userID uint, key string) *database.TenantPurchase {
	if key == "" || !IdempotencyReady(db) {
		return nil
	}
	var rows []database.TenantPurchase
	if err := db.Where("user_id = ? AND idempotency_key = ?", userID, key).Limit(1).Find(&rows).Error; err != nil || len(rows) == 0 {
		return nil
	}
	return &rows[0]
}

// ensureNewDocument bloquea al proveedor y comprueba que no tenga ya esa serie y número. El bloqueo serializa dos registros
// simultáneos del mismo comprobante.
func ensureNewDocument(tx *gorm.DB, contactID *uint, series, number string) error {
	if contactID == nil || *contactID == 0 {
		return errors.New("indica el proveedor")
	}
	q := tx.Model(&database.TenantContact{}).Select("id").Where("id = ?", *contactID)
	if tx.Dialector.Name() == "mysql" {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var ids []uint
	if err := q.Find(&ids).Error; err != nil || len(ids) == 0 {
		return errors.New("proveedor no encontrado")
	}
	var n int64
	if err := tx.Model(&database.TenantPurchase{}).
		Where("contact_id = ? AND series = ? AND number = ? AND status <> ?", *contactID, series, number, StatusCancelled).
		Count(&n).Error; err != nil {
		return err
	}
	if n > 0 {
		return ErrDuplicateDocument
	}
	return nil
}
