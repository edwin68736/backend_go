package database

import (
	"context"

	"tukifac/config"

	"gorm.io/gorm"
)

// WithQueryClass aplica timeout según tipo de operación. El caller debe defer cancel().
func WithQueryClass(db *gorm.DB, class config.QueryClass) (*gorm.DB, context.CancelFunc) {
	if db == nil {
		return nil, func() {}
	}
	ctx, cancel := config.AppConfig.DBContext(class)
	return db.WithContext(ctx), cancel
}
