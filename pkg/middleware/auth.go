package middleware

import (
	"fmt"
	"strings"

	"tukifac/config"

	"github.com/gofiber/fiber/v3"
	"github.com/golang-jwt/jwt/v5"
)

// TenantClaims es el payload del JWT del tenant.
// Contiene todo lo necesario para autorizar el request sin consultar la BD central.
type TenantClaims struct {
	UserID      uint     `json:"user_id"`
	Email       string   `json:"email"`
	RoleID      uint     `json:"role_id"`
	RoleName    string   `json:"role_name"`
	TenantSlug  string   `json:"tenant_slug"`
	TenantDB    string   `json:"tenant_db"`
	TenantID    uint     `json:"tenant_id"`    // ID del tenant en BD central
	PlanID      uint     `json:"plan_id"`      // Plan activo al momento del login
	Modules     []string `json:"modules"`      // Módulos habilitados (fuente: BD central al login)
	Permissions []string `json:"permissions"`  // Permisos del rol en formato "module.action"
	RestaurantRole string   `json:"restaurant_role"` // Rol operativo en módulo restaurante: admin, vendedor, mozo, cocinero (vacío = sin acceso)
	Status      string   `json:"status"`        // Estado del tenant al momento del login
	Type        string   `json:"type"`         // "tenant"
	jwt.RegisteredClaims
}

// Claims para super admin
type SuperAdminClaims struct {
	UserID uint   `json:"user_id"`
	Email  string `json:"email"`
	Role   string `json:"role"`
	Type   string `json:"type"` // "superadmin"
	jwt.RegisteredClaims
}

// TenantAuthWeb protege rutas web del tenant (cookie token)
func TenantAuthWeb() fiber.Handler {
	return func(c fiber.Ctx) error {
		token := c.Cookies("token")
		if token == "" {
			return c.Redirect().To("/login")
		}

		claims := &TenantClaims{}
		t, err := jwt.ParseWithClaims(token, claims, func(t *jwt.Token) (interface{}, error) {
			return []byte(config.AppConfig.JWTSecret), nil
		})
		if err != nil || !t.Valid || claims.Type != "tenant" {
			c.ClearCookie("token")
			return c.Redirect().To("/login")
		}

		c.Locals("user_id", claims.UserID)
		c.Locals("user_email", claims.Email)
		c.Locals("user_role_id", claims.RoleID)
		c.Locals("user_role", claims.RoleName)
		c.Locals("user_name", claims.Email)
		c.Locals("permissions", claims.Permissions)
		c.Locals("is_dev", config.AppConfig.IsDev())
		return c.Next()
	}
}

// TenantAuthAPI protege rutas API del tenant.
// Acepta Bearer token en Authorization header O cookie "token".
// Valida: token válido, tipo "tenant", y status == "active" extraído del JWT.
// NO consulta la BD central en cada request — el JWT es la fuente de verdad durante la sesión.
func TenantAuthAPI() fiber.Handler {
	publicPaths := map[string]bool{
		"/api/login":            true,
		"/api/superadmin/login": true,
	}

	return func(c fiber.Ctx) error {
		if publicPaths[c.Path()] {
			return c.Next()
		}

		// 1. Bearer token del header Authorization
		tokenStr := ""
		authHeader := c.Get("Authorization")
		if authHeader != "" {
			parts := strings.Split(authHeader, " ")
			if len(parts) == 2 && parts[0] == "Bearer" && parts[1] != "null" && parts[1] != "" {
				tokenStr = parts[1]
			}
		}

		// 2. Fallback: cookie "token"
		if tokenStr == "" {
			tokenStr = c.Cookies("token")
		}

		if tokenStr == "" {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Token no proporcionado"})
		}

		claims := &TenantClaims{}
		t, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (interface{}, error) {
			return []byte(config.AppConfig.JWTSecret), nil
		})
		if err != nil || !t.Valid || claims.Type != "tenant" {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Token inválido o expirado"})
		}

		// Verificar estado del tenant desde el JWT (sin consultar BD central)
		if claims.Status != "active" {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
				"error":  "Cuenta suspendida. Contacte al administrador.",
				"status": claims.Status,
			})
		}

		// Inyectar datos en Locals para handlers y middleware de módulos
		c.Locals("user_id", claims.UserID)
		c.Locals("user_email", claims.Email)
		c.Locals("user_role_id", claims.RoleID)
		c.Locals("user_role", claims.RoleName)
		c.Locals("restaurant_role", claims.RestaurantRole)
		c.Locals("tenant_claims", claims) // acceso completo a claims para RequireModule
		c.Locals("permissions", claims.Permissions)
		return c.Next()
	}
}

// RequireModule verifica que el módulo requerido esté habilitado en el JWT del tenant.
// Usar después de TenantAuthAPI en rutas específicas.
//
// Ejemplo de uso:
//
//	api.Get("/billing/invoices", middleware.RequireModule("billing"), handler.ListInvoices)
func RequireModule(moduleKey string) fiber.Handler {
	return func(c fiber.Ctx) error {
		claims, ok := c.Locals("tenant_claims").(*TenantClaims)
		if !ok || claims == nil {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
				"error": "Sin contexto de autenticación",
			})
		}
		for _, m := range claims.Modules {
			if m == moduleKey {
				return c.Next()
			}
		}
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
			"error":  fmt.Sprintf("El módulo '%s' no está habilitado en tu plan", moduleKey),
			"module": moduleKey,
		})
	}
}

// SuperAdminAuthWeb protege rutas web del panel super admin (cookie sa_token)
func SuperAdminAuthWeb() fiber.Handler {
	return func(c fiber.Ctx) error {
		token := c.Cookies("sa_token")
		if token == "" {
			return c.Redirect().To("/superadmin/login")
		}

		claims := &SuperAdminClaims{}
		t, err := jwt.ParseWithClaims(token, claims, func(t *jwt.Token) (interface{}, error) {
			return []byte(config.AppConfig.SAJWTSecret), nil
		})
		if err != nil || !t.Valid || claims.Type != "superadmin" {
			c.ClearCookie("sa_token")
			return c.Redirect().To("/superadmin/login")
		}

		c.Locals("sa_user_id", claims.UserID)
		c.Locals("sa_user_email", claims.Email)
		c.Locals("sa_user_role", claims.Role)
		return c.Next()
	}
}

// SuperAdminAuthAPI protege rutas API del super admin (Bearer token)
func SuperAdminAuthAPI() fiber.Handler {
	return func(c fiber.Ctx) error {
		// El login del superadmin es público — no requiere token
		if c.Path() == "/api/superadmin/login" {
			return c.Next()
		}

		authHeader := c.Get("Authorization")
		if authHeader == "" {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Token no proporcionado"})
		}

		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || parts[0] != "Bearer" {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Formato de token inválido"})
		}

		claims := &SuperAdminClaims{}
		t, err := jwt.ParseWithClaims(parts[1], claims, func(t *jwt.Token) (interface{}, error) {
			return []byte(config.AppConfig.SAJWTSecret), nil
		})
		if err != nil || !t.Valid || claims.Type != "superadmin" {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Token inválido o expirado"})
		}

		c.Locals("sa_user_id", claims.UserID)
		c.Locals("sa_user_email", claims.Email)
		c.Locals("sa_user_role", claims.Role)
		return c.Next()
	}
}

// RequireRestaurantRole verifica que el usuario tenga uno de los roles operativos del restaurante.
// Roles: admin, vendedor, mozo, cocinero. Solo aplica en rutas del módulo restaurant.
func RequireRestaurantRole(allowedRoles ...string) fiber.Handler {
	allowed := make(map[string]struct{}, len(allowedRoles))
	for _, r := range allowedRoles {
		allowed[r] = struct{}{}
	}
	return func(c fiber.Ctx) error {
		claims, ok := c.Locals("tenant_claims").(*TenantClaims)
		if !ok || claims == nil {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Sin contexto de autenticación"})
		}
		role := claims.RestaurantRole
		if role == "" {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
				"error": "No tienes un rol asignado en el módulo restaurante. Contacta al administrador.",
			})
		}
		if _, ok := allowed[role]; !ok {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
				"error": "No tienes permiso para esta acción en el restaurante",
				"role":  role,
			})
		}
		return c.Next()
	}
}

// RequireRestaurantAdminOrTenantAdmin permite acceso si el usuario es admin de restaurante O administrador del tenant.
// Útil para gestionar roles de restaurante desde el panel tenant.
func RequireRestaurantAdminOrTenantAdmin() fiber.Handler {
	return func(c fiber.Ctx) error {
		claims, ok := c.Locals("tenant_claims").(*TenantClaims)
		if !ok || claims == nil {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Sin contexto de autenticación"})
		}
		if claims.RestaurantRole == "admin" {
			return c.Next()
		}
		if claims.RoleName == "Administrador" {
			return c.Next()
		}
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
			"error": "Se requiere ser administrador del restaurante o del tenant",
		})
	}
}

// RequireRole verifica que el usuario tenga uno de los roles especificados
func RequireRole(roles ...string) fiber.Handler {
	allowed := make(map[string]struct{}, len(roles))
	for _, r := range roles {
		allowed[r] = struct{}{}
	}
	return func(c fiber.Ctx) error {
		role, _ := c.Locals("user_role").(string)
		if len(allowed) == 0 {
			return c.Next()
		}
		if _, ok := allowed[role]; !ok {
			return c.Status(fiber.StatusForbidden).SendString("No tienes permisos para esta acción")
		}
		return c.Next()
	}
}
