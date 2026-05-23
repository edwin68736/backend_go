package middleware

import (
	"time"

	"tukifac/config"

	"github.com/golang-jwt/jwt/v5"
)

// BuildTenantToken emite JWT tenant; para PIN no incluye Permissions (liviano).
func BuildTenantToken(claims *TenantClaims, ttl time.Duration) (string, error) {
	if claims.RegisteredClaims.IssuedAt == nil {
		claims.RegisteredClaims.IssuedAt = jwt.NewNumericDate(time.Now())
	}
	if claims.RegisteredClaims.ExpiresAt == nil {
		claims.RegisteredClaims.ExpiresAt = jwt.NewNumericDate(time.Now().Add(ttl))
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(config.AppConfig.JWTSecret))
}

// PinSessionTTL duración sesión operativa por PIN.
const PinSessionTTL = 12 * time.Hour

// PasswordSessionTTL sesión administrativa estándar.
const PasswordSessionTTL = 24 * time.Hour
