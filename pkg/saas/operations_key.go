package saas

import (
	"errors"
	"strings"

	"tukifac/pkg/database"

	"golang.org/x/crypto/bcrypt"
)

var (
	ErrOperationsKeyNotConfigured = errors.New("clave de operaciones no configurada: defínala en Ajustes SaaS")
	ErrOperationsKeyInvalid       = errors.New("clave de operaciones incorrecta")
	ErrOperationsKeyTooShort      = errors.New("la clave de operaciones debe tener al menos 8 caracteres")
)

// OperationsKeyConfigured indica si hay clave definida en saas_platform_settings.
func OperationsKeyConfigured() bool {
	if database.CentralDB == nil {
		return false
	}
	var row database.SaasPlatformSettings
	if database.CentralDB.Select("operations_key_hash").First(&row, 1).Error != nil {
		return false
	}
	return strings.TrimSpace(row.OperationsKeyHash) != ""
}

// SetOperationsKey define o cambia la clave (bcrypt). Si ya existe, currentKey es obligatorio.
func SetOperationsKey(newKey, currentKey string) error {
	newKey = strings.TrimSpace(newKey)
	if len(newKey) < 8 {
		return ErrOperationsKeyTooShort
	}
	if database.CentralDB == nil {
		return errors.New("BD central no disponible")
	}
	var row database.SaasPlatformSettings
	if err := database.CentralDB.First(&row, 1).Error; err != nil {
		return err
	}
	if row.OperationsKeyHash != "" {
		if strings.TrimSpace(currentKey) == "" {
			return errors.New("indique la clave de operaciones actual para cambiarla")
		}
		if err := bcrypt.CompareHashAndPassword([]byte(row.OperationsKeyHash), []byte(currentKey)); err != nil {
			return ErrOperationsKeyInvalid
		}
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(newKey), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	return database.CentralDB.Model(&row).Update("operations_key_hash", string(hash)).Error
}

// VerifyOperationsKey valida la clave antes de operaciones destructivas.
func VerifyOperationsKey(key string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return ErrOperationsKeyInvalid
	}
	if database.CentralDB == nil {
		return errors.New("BD central no disponible")
	}
	var row database.SaasPlatformSettings
	if err := database.CentralDB.Select("operations_key_hash").First(&row, 1).Error; err != nil {
		return ErrOperationsKeyNotConfigured
	}
	if row.OperationsKeyHash == "" {
		return ErrOperationsKeyNotConfigured
	}
	if err := bcrypt.CompareHashAndPassword([]byte(row.OperationsKeyHash), []byte(key)); err != nil {
		return ErrOperationsKeyInvalid
	}
	return nil
}
