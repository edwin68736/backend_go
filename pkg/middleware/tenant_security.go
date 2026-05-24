package middleware

import (
	"log/slog"

	"tukifac/pkg/database"
	"tukifac/pkg/logger"

	"github.com/gofiber/fiber/v3"
)

// logTenantSecurityViolation registra intentos de cross-tenant (auditoría producción).
func logTenantSecurityViolation(c fiber.Ctx, reason string, attrs ...any) {
	args := []any{
		slog.String("event", "tenant_security_violation"),
		slog.String("reason", reason),
		slog.String("request_id", GetRequestID(c)),
		slog.String("path", c.Path()),
		slog.String("method", c.Method()),
		slog.String("host", c.Hostname()),
		slog.String("subdomain", localString(c, "tenant_subdomain_slug")),
		slog.String("header_tenant_slug", c.Get("X-Tenant-Slug")),
	}
	if slug, ok := c.Locals("tenant_slug").(string); ok {
		args = append(args, slog.String("resolved_slug", slug))
	}
	if tenant, ok := c.Locals("tenant").(*database.Tenant); ok && tenant != nil {
		args = append(args,
			slog.Uint64("resolved_tenant_id", uint64(tenant.ID)),
			slog.String("resolved_db", tenant.DBName),
		)
	}
	if claims, ok := c.Locals("tenant_claims").(*TenantClaims); ok && claims != nil {
		args = append(args,
			slog.String("jwt_tenant_slug", claims.TenantSlug),
			slog.String("jwt_tenant_db", claims.TenantDB),
			slog.Uint64("jwt_tenant_id", uint64(claims.TenantID)),
		)
	}
	args = append(args, slog.Bool("blocked", true))
	args = append(args, attrs...)
	logger.L.Error("tenant_security_violation", args...)
}

func localString(c fiber.Ctx, key string) string {
	if v, ok := c.Locals(key).(string); ok {
		return v
	}
	return ""
}

func tenantSecurityForbidden(c fiber.Ctx, reason string) error {
	logTenantSecurityViolation(c, reason)
	return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
		"error": "Acceso denegado: contexto de empresa no válido",
		"code":  "TENANT_ISOLATION_VIOLATION",
		"reason": reason,
	})
}
