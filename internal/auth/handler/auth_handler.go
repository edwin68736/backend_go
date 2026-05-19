package handler

import (
	"errors"
	"time"

	"tukifac/config"
	"tukifac/internal/users/service"
	restsvc "tukifac/internal/restaurant/service"
	"tukifac/pkg/database"
	"tukifac/pkg/middleware"

	"github.com/gofiber/fiber/v3"
	"github.com/golang-jwt/jwt/v5"
	"gorm.io/gorm"
)

type AuthHandler struct{}

func NewAuthHandler() *AuthHandler {
	return &AuthHandler{}
}

func (h *AuthHandler) LoginPage(c fiber.Ctx) error {
	tenant, _ := c.Locals("tenant").(*database.Tenant)

	// Sin tenant en desarrollo → mostrar selector de empresas
	// Sin tenant en producción → redirigir a superadmin
	if tenant == nil {
		if config.AppConfig.IsDev() {
			return c.Redirect().To("/")
		}
		return c.Redirect().To("/superadmin/login")
	}

	return c.Render("auth/login", fiber.Map{
		"Title":      "Iniciar sesión",
		"TenantName": tenant.Name,
	})
}

func (h *AuthHandler) LoginSubmit(c fiber.Ctx) error {
	email := c.FormValue("email")
	password := c.FormValue("password")

	tenant, _ := c.Locals("tenant").(*database.Tenant)
	tenantDB, _ := c.Locals("tenantDB").(*gorm.DB)

	// Sin tenant → redirigir a inicio (dev) o superadmin (prod)
	if tenant == nil || tenantDB == nil {
		if config.AppConfig.IsDev() {
			return c.Redirect().To("/")
		}
		return c.Redirect().To("/superadmin/login")
	}

	tenantName := tenant.Name

	renderError := func(msg string) error {
		return c.Render("auth/login", fiber.Map{
			"Title":      "Iniciar sesión",
			"TenantName": tenantName,
			"Error":      msg,
			"Email":      email,
		})
	}

	if email == "" || password == "" {
		return renderError("Email y contraseña son requeridos")
	}

	var user database.TenantUser
	if err := tenantDB.Where("email = ? AND active = ?", email, true).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return renderError("Credenciales inválidas")
		}
		return renderError("Error interno")
	}

	if !user.CheckPassword(password) {
		return renderError("Credenciales inválidas")
	}

	// Obtener nombre del rol
	var role database.TenantRole
	tenantDB.First(&role, user.RoleID)

	tenantSlug := ""
	tenantDBName := ""
	if tenant != nil {
		tenantSlug = tenant.Slug
		tenantDBName = tenant.DBName
	}

	claims := &middleware.TenantClaims{
		UserID:     user.ID,
		Email:      user.Email,
		RoleID:     user.RoleID,
		RoleName:   role.Name,
		TenantSlug: tenantSlug,
		TenantDB:   tenantDBName,
		Type:       "tenant",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(config.AppConfig.JWTSecret))
	if err != nil {
		return renderError("Error generando sesión")
	}

	c.Cookie(&fiber.Cookie{
		Name:     "token",
		Value:    tokenString,
		Path:     "/",
		HTTPOnly: true,
		MaxAge:   24 * 3600,
		SameSite: "Lax",
	})

	return c.Redirect().To("/dashboard")
}

func (h *AuthHandler) Logout(c fiber.Ctx) error {
	c.ClearCookie("token")
	return c.Redirect().To("/login")
}

func (h *AuthHandler) LoginAPI(c fiber.Ctx) error {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := c.Bind().Body(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Datos inválidos"})
	}

	tenantDB, _ := c.Locals("tenantDB").(*gorm.DB)
	tenant, _ := c.Locals("tenant").(*database.Tenant)
	if tenantDB == nil || tenant == nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Contexto de empresa no encontrado"})
	}

	// REGLA CENTRAL: verificar estado del tenant antes de cualquier cosa.
	// La suspensión es siempre manual desde el panel central.
	if tenant.Status != "active" {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
			"error":  "Cuenta suspendida. Contacte al administrador del sistema.",
			"status": tenant.Status,
		})
	}

	var user database.TenantUser
	if err := tenantDB.Where("email = ? AND active = ?", req.Email, true).First(&user).Error; err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Credenciales inválidas"})
	}

	if !user.CheckPassword(req.Password) {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Credenciales inválidas"})
	}

	var role database.TenantRole
	tenantDB.First(&role, user.RoleID)

	// Permisos del rol en formato "module.action" para el JWT
	roleSvc := service.NewRoleService(tenantDB)
	permissionKeys, _ := roleSvc.GetRolePermissionKeys(user.RoleID)
	if permissionKeys == nil {
		permissionKeys = []string{}
	}

	// Consultar BD central UNA SOLA VEZ al login:
	// módulos habilitados del tenant
	enabledModules := make([]string, 0)
	var tms []database.TenantModule
	database.CentralDB.
		Where("tenant_id = ? AND enabled = ?", tenant.ID, true).
		Find(&tms)
	for _, m := range tms {
		enabledModules = append(enabledModules, m.ModuleKey)
	}

	// Plan activo (PlanID para referencia)
	var planID uint
	var subscriptionInfo fiber.Map
	var sub database.SaasSubscription
	if err := database.CentralDB.
		Where("tenant_id = ? AND status IN ('active','trial')", tenant.ID).
		Order("created_at desc").First(&sub).Error; err == nil {
		planID = sub.PlanID
		var plan database.SaasPlan
		database.CentralDB.First(&plan, sub.PlanID)
		subscriptionInfo = fiber.Map{
			"plan_name":  plan.Name,
			"status":     sub.Status,
			"end_date":   sub.EndDate,
			"start_date": sub.StartDate,
		}
	}

	// Rol operativo en módulo restaurante (solo si el tenant tiene el módulo)
	restaurantRole := ""
	hasRestaurant := false
	for _, m := range enabledModules {
		if m == "restaurant" {
			hasRestaurant = true
			break
		}
	}
	if hasRestaurant {
		restSvc := restsvc.New(tenantDB)
		restaurantRole, _ = restSvc.GetUserRestaurantRole(user.ID)
	}

	// Construir JWT con todos los permisos embebidos.
	// El middleware TenantAuthAPI NO consultará la BD central en requests posteriores.
	claims := &middleware.TenantClaims{
		UserID:         user.ID,
		Email:          user.Email,
		RoleID:         user.RoleID,
		RoleName:       role.Name,
		TenantSlug:     tenant.Slug,
		TenantDB:       tenant.DBName,
		TenantID:       tenant.ID,
		PlanID:         planID,
		Modules:        enabledModules,
		Permissions:    permissionKeys,
		RestaurantRole: restaurantRole,
		Status:         tenant.Status,
		Type:           "tenant",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(config.AppConfig.JWTSecret))
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error generando token"})
	}

	return c.JSON(fiber.Map{
		"token": tokenString,
		"user": fiber.Map{
			"id":               user.ID,
			"name":             user.Name,
			"email":            user.Email,
			"role":             role.Name,
			"restaurant_role":  restaurantRole,
		},
		"modules":      enabledModules,
		"permissions":  permissionKeys,
		"subscription": subscriptionInfo,
	})
}
