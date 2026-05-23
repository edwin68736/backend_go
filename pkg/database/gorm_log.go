package database

import (
	"log"
	"os"
	"time"

	"gorm.io/gorm/logger"
)

// gormLogger logger compartido: Warn+ y sin ruido por ErrRecordNotFound esperado (find-or-create).
func gormLogger() logger.Interface {
	return logger.New(
		log.New(os.Stdout, "\r\n", log.LstdFlags),
		logger.Config{
			SlowThreshold:             time.Second,
			LogLevel:                  logger.Warn,
			IgnoreRecordNotFoundError: true,
			Colorful:                  true,
		},
	)
}
