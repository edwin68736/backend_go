package middleware

import (
	"errors"
	"strings"

	"tukifac/config"
)

var (
	ErrLegacyTokenNoTenantID = errors.New("token obsoleto: vuelva a iniciar sesión")
	ErrLegacyTokenNoVersion  = errors.New("token obsoleto: actualice la aplicación e inicie sesión de nuevo")
)

// validateTenantJWTClaims rechaza tokens sin aislamiento tenant completo (migración segura).
func validateTenantJWTClaims(claims *TenantClaims) error {
	if claims == nil {
		return errors.New("token inválido")
	}
	if claims.TenantID == 0 {
		return ErrLegacyTokenNoTenantID
	}
	if strings.TrimSpace(claims.TenantSlug) == "" || strings.TrimSpace(claims.TenantDB) == "" {
		return ErrLegacyTokenNoTenantID
	}
	if config.AppConfig != nil && config.AppConfig.IsProd() {
		if claims.TenantVersion < MinTenantJWTVersion {
			return ErrLegacyTokenNoVersion
		}
	}
	return nil
}

// CurrentTenantJWTVersion versión embebida en tokens nuevos.
func CurrentTenantJWTVersion() uint {
	return MinTenantJWTVersion
}
