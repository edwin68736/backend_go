package handler

import (
	"time"

	"tukifac/config"
	"tukifac/pkg/branch"
	"tukifac/pkg/database"
	"tukifac/pkg/middleware"

	"github.com/gofiber/fiber/v3"
	"github.com/golang-jwt/jwt/v5"
	"gorm.io/gorm"
)

type SessionHandler struct{}

func NewSessionHandler() *SessionHandler {
	return &SessionHandler{}
}

func sessionTenantDB(c fiber.Ctx) *gorm.DB {
	v, _ := c.Locals("tenantDB").(*gorm.DB)
	return v
}

// GET /api/session/capabilities — feature flags según schema del tenant.
func (h *SessionHandler) GetCapabilities(c fiber.Ctx) error {
	tdb := sessionTenantDB(c)
	if tdb == nil {
		return c.Status(400).JSON(fiber.Map{"error": "Sin contexto"})
	}
	ver := database.CurrentSchemaVersion(tdb)
	return c.JSON(fiber.Map{
		"schema_version": ver,
		"features":       database.TenantFeatureFlags(tdb),
	})
}

// GET /api/session/context
func (h *SessionHandler) GetContext(c fiber.Ctx) error {
	tdb := sessionTenantDB(c)
	claims, _ := c.Locals("tenant_claims").(*middleware.TenantClaims)
	if tdb == nil || claims == nil {
		return c.Status(400).JSON(fiber.Map{"error": "Sin contexto"})
	}
	activeID := branch.ActiveBranchID(c)
	if activeID == 0 {
		activeID = claims.ActiveBranchID
	}
	brief, _ := branch.GetBranchBrief(tdb, activeID)
	user, _, _ := database.LoadTenantUserForBranch(tdb, claims.UserID)
	canSwitch := false
	sessionVer := uint(0)
	if user != nil {
		canSwitch = branch.CanSwitchBranch(claims.RoleName, user)
		if database.TenantBranchSessionVersionReady(tdb) {
			sessionVer = user.BranchSessionVersion
		}
	}
	return c.JSON(fiber.Map{
		"active_branch":          brief,
		"can_switch_branch":      canSwitch,
		"branch_session_version": sessionVer,
	})
}

// POST /api/session/switch-branch
func (h *SessionHandler) SwitchBranch(c fiber.Ctx) error {
	tdb := sessionTenantDB(c)
	claims, _ := c.Locals("tenant_claims").(*middleware.TenantClaims)
	if tdb == nil || claims == nil {
		return c.Status(400).JSON(fiber.Map{"error": "Sin contexto"})
	}
	var body struct {
		BranchID uint `json:"branch_id"`
	}
	if err := c.Bind().JSON(&body); err != nil || body.BranchID == 0 {
		return c.Status(400).JSON(fiber.Map{"error": "branch_id requerido"})
	}

	user, legacy, err := database.LoadTenantUserForBranch(tdb, claims.UserID)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "Usuario no encontrado"})
	}
	if legacy || !database.TenantBranchMultiSchemaReady(tdb) {
		return c.Status(503).JSON(fiber.Map{
			"error":   "Migración de sucursales pendiente",
			"message": "Ejecute migrate en el tenant antes de cambiar sucursal",
		})
	}
	if !branch.CanSwitchBranch(claims.RoleName, user) {
		return c.Status(403).JSON(fiber.Map{"error": "No tiene permiso para cambiar de sucursal"})
	}

	var b database.TenantBranch
	if err := tdb.Where("id = ? AND active = ?", body.BranchID, true).First(&b).Error; err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "Sucursal no válida"})
	}

	tokenString, brief, err := issueTokenWithBranch(tdb, claims, user, b.ID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{
		"token":             tokenString,
		"active_branch":     brief,
		"can_switch_branch": true,
	})
}

func branchSessionVersionForToken(tdb *gorm.DB, user *database.TenantUser) uint {
	if user == nil || !database.TenantBranchSessionVersionReady(tdb) {
		return 0
	}
	return user.BranchSessionVersion
}

func issueTokenWithBranch(tdb *gorm.DB, old *middleware.TenantClaims, user *database.TenantUser, activeBranchID uint) (string, *branch.BranchBrief, error) {
	brief, err := branch.GetBranchBrief(tdb, activeBranchID)
	if err != nil {
		return "", nil, err
	}
	claims := &middleware.TenantClaims{
		UserID:               old.UserID,
		Email:                old.Email,
		RoleID:               old.RoleID,
		RoleName:             old.RoleName,
		TenantSlug:           old.TenantSlug,
		TenantDB:             old.TenantDB,
		TenantID:             old.TenantID,
		PlanID:               old.PlanID,
		Modules:              old.Modules,
		Permissions:          old.Permissions,
		EmployeeType:         old.EmployeeType,
		AuthMethod:           old.AuthMethod,
		PermVer:              old.PermVer,
		StaffID:              old.StaffID,
		Status:               old.Status,
		Type:                 "tenant",
		ActiveBranchID:       activeBranchID,
		BranchSessionVersion: branchSessionVersionForToken(tdb, user),
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(config.AppConfig.JWTSecret))
	return tokenString, brief, err
}
