package handler

import (
	"errors"
	"time"

	"tukifac/config"
	"tukifac/internal/users/service"
	reststaff "tukifac/internal/restaurant/staff"
	"tukifac/pkg/branch"
	"tukifac/pkg/database"
	"tukifac/pkg/middleware"
	"tukifac/pkg/saas"

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
	var tenantID uint
	if tenant != nil {
		tenantSlug = tenant.Slug
		tenantDBName = tenant.DBName
		tenantID = tenant.ID
	}

	claims := &middleware.TenantClaims{
		UserID:        user.ID,
		Email:         user.Email,
		RoleID:        user.RoleID,
		RoleName:      role.Name,
		TenantSlug:    tenantSlug,
		TenantDB:      tenantDBName,
		TenantID:      tenantID,
		TenantVersion: middleware.CurrentTenantJWTVersion(),
		Type:          "tenant",
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

	// Suspendidos/bloqueados pueden iniciar sesión para ver deuda y contactar soporte.
	subscriptionBlocked := tenant.Status != "active"

	user, legacyBranch, err := database.LoadTenantUserForBranchByEmail(tenantDB, req.Email)
	if err != nil {
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

	var planID uint
	subView, _ := saas.GetTenantView(tenant.ID)
	planID = subView.PlanID
	subscriptionInfo := fiber.Map{
		"plan_name":             subView.PlanName,
		"status":                subView.Status,
		"tenant_status":         subView.TenantStatus,
		"end_date":              subView.EndDate,
		"start_date":            subView.StartDate,
		"days_until_expiry":     subView.DaysUntilExpiry,
		"can_operate":           subView.CanOperate,
		"is_blocked":            subView.IsBlocked,
		"strike_count":          subView.StrikeCount,
		"can_submit_payment":    subView.CanSubmitPayment,
		"provisional_until":     subView.ProvisionalUntil,
		"support_message":       subView.SupportMessage,
		"show_renewal_banner":   subView.ShowRenewalBanner,
		"show_suspended_banner": subView.ShowSuspendedBanner,
		"pending_amount":        subView.PendingAmount,
		"reconnection_fee":      subView.ReconnectionFee,
		"portal_url":            subView.PortalURL,
	}

	hasRestaurant := false
	for _, m := range enabledModules {
		if m == "restaurant" {
			hasRestaurant = true
			break
		}
	}
	employeeType := ""
	staffID := uint(0)
	permVer := uint(0)
	if hasRestaurant {
		staffSvc := reststaff.New(tenantDB)
		permVer, _ = staffSvc.GetPermCacheVersion()
		if st, err := staffSvc.GetStaffByUserID(user.ID); err == nil && st.IsActive {
			employeeType = st.EmployeeType
			staffID = st.ID
		}
	}

	if !legacyBranch {
		branch.SyncUserBranchFields(user, role.Name)
		if user.HomeBranchID == nil || (user.HomeBranchID != nil && *user.HomeBranchID == 0) {
			if hid, err := branch.ResolveHomeBranchID(tenantDB, user); err == nil {
				hb := hid
				user.HomeBranchID = &hb
				user.BranchID = &hb
				_ = database.PersistUserBranchFieldsOnLogin(tenantDB, user.ID, hb, branch.CanSwitchBranch(role.Name, user))
			}
		}
	}

	activeBranchID, err := branch.ResolveHomeBranchID(tenantDB, user)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}
	canSwitch := branch.CanSwitchBranch(role.Name, user)
	activeBrief, _ := branch.GetBranchBrief(tenantDB, activeBranchID)

	sessionVersion := uint(0)
	if !legacyBranch {
		sessionVersion = user.BranchSessionVersion
	}

	claims := &middleware.TenantClaims{
		UserID:               user.ID,
		Email:                user.Email,
		RoleID:               user.RoleID,
		RoleName:             role.Name,
		TenantSlug:           tenant.Slug,
		TenantDB:             tenant.DBName,
		TenantID:             tenant.ID,
		TenantVersion:        middleware.CurrentTenantJWTVersion(),
		PlanID:               planID,
		Modules:              enabledModules,
		Permissions:          permissionKeys,
		EmployeeType:         employeeType,
		AuthMethod:           "pwd",
		PermVer:              permVer,
		StaffID:              staffID,
		Status:               tenant.Status,
		Type:                 "tenant",
		ActiveBranchID:       activeBranchID,
		BranchSessionVersion: sessionVersion,
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

	restPerms := []string(nil)
	if hasRestaurant && employeeType != "" {
		restPerms, _ = reststaff.New(tenantDB).ResolvePermissionKeys(tenant.Slug, tenant.ID, user.ID, permVer)
	}

	return c.JSON(fiber.Map{
		"token": tokenString,
		"user": fiber.Map{
			"id":                user.ID,
			"name":              user.Name,
			"email":             user.Email,
			"role":              role.Name,
			"employee_type":     employeeType,
			"staff_id":          staffID,
			"home_branch_id":    activeBranchID,
			"can_switch_branch": canSwitch,
		},
		"active_branch":         activeBrief,
		"can_switch_branch":     canSwitch,
		"modules":               enabledModules,
		"permissions":            permissionKeys,
		"restaurant_permissions": restPerms,
		"subscription":           subscriptionInfo,
		"subscription_blocked":   subscriptionBlocked,
	})
}
