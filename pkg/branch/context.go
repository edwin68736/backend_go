package branch

import (
	"errors"

	"tukifac/pkg/database"

	"github.com/gofiber/fiber/v3"
	"gorm.io/gorm"
)

// Nota: ActiveBranchID usa fiber.Ctx en helpers HTTP.

const (
	CodeSessionUpdated = "SESSION_UPDATED"
	CodeBranchRequired = "BRANCH_REQUIRED"
	CodeBranchForbidden = "BRANCH_FORBIDDEN"
)

// IsTenantAdmin indica si el rol puede cambiar sucursal y operar cross-branch en reportes.
func IsTenantAdmin(roleName string) bool {
	return roleName == "Administrador"
}

// ResolveHomeBranchID obtiene la sucursal base del usuario (home > branch_id legacy > principal).
func ResolveHomeBranchID(db *gorm.DB, u *database.TenantUser) (uint, error) {
	if u.HomeBranchID != nil && *u.HomeBranchID > 0 {
		return *u.HomeBranchID, nil
	}
	if u.BranchID != nil && *u.BranchID > 0 {
		return *u.BranchID, nil
	}
	main, err := GetMainBranchID(db)
	if err != nil {
		return 0, err
	}
	return main, nil
}

// GetMainBranchID retorna la sucursal principal activa o la primera activa.
func GetMainBranchID(db *gorm.DB) (uint, error) {
	var b database.TenantBranch
	if err := db.Where("is_main = ? AND active = ?", true, true).First(&b).Error; err == nil {
		return b.ID, nil
	}
	if err := db.Where("active = ?", true).Order("id ASC").First(&b).Error; err != nil {
		return 0, errors.New("no hay sucursal activa configurada")
	}
	return b.ID, nil
}

// BranchBrief datos mínimos de sucursal para API.
type BranchBrief struct {
	ID     uint   `json:"id"`
	Name   string `json:"name"`
	IsMain bool   `json:"is_main"`
}

// GetBranchBrief carga id y nombre de sucursal.
func GetBranchBrief(db *gorm.DB, id uint) (*BranchBrief, error) {
	if id == 0 {
		return nil, errors.New("sucursal inválida")
	}
	var b database.TenantBranch
	if err := db.First(&b, id).Error; err != nil {
		return nil, err
	}
	return &BranchBrief{ID: b.ID, Name: b.Name, IsMain: b.IsMain}, nil
}

// CanSwitchBranch según rol administrador tenant.
func CanSwitchBranch(roleName string, u *database.TenantUser) bool {
	if u != nil && u.CanSwitchBranch {
		return true
	}
	return IsTenantAdmin(roleName)
}

// ActiveBranchID desde Locals (inyectado por middleware).
func ActiveBranchID(c fiber.Ctx) uint {
	v, _ := c.Locals("active_branch_id").(uint)
	return v
}

// IsBranchAdmin desde Locals.
func IsBranchAdmin(c fiber.Ctx) bool {
	v, _ := c.Locals("is_branch_admin").(bool)
	return v
}

// ResolveWriteBranchID usa sucursal activa; admin puede enviar otra solo si can switch.
func ResolveWriteBranchID(c fiber.Ctx, requested uint) (uint, error) {
	active := ActiveBranchID(c)
	if active == 0 {
		return 0, errors.New("sucursal activa requerida")
	}
	if requested == 0 || requested == active {
		return active, nil
	}
	if IsBranchAdmin(c) {
		return requested, nil
	}
	return 0, errors.New("no puede operar en otra sucursal")
}

// ResolveReadBranchFilter para listados: 0 = activa; admin puede filtrar otra sucursal.
func ResolveReadBranchFilter(c fiber.Ctx, requested uint) uint {
	active := ActiveBranchID(c)
	if requested == 0 {
		return active
	}
	if IsBranchAdmin(c) {
		return requested
	}
	return active
}

// BumpSessionVersion incrementa versión al cambiar asignación de sucursal del usuario.
// No-op si el tenant aún no tiene la columna (rolling deploy / legacy).
func BumpSessionVersion(db *gorm.DB, userID uint) error {
	if !database.TenantBranchSessionVersionReady(db) {
		return nil
	}
	return db.Model(&database.TenantUser{}).Where("id = ?", userID).
		UpdateColumn("branch_session_version", gorm.Expr("branch_session_version + 1")).Error
}

// SyncUserBranchFields al crear/actualizar usuario desde branch_id legacy.
func SyncUserBranchFields(u *database.TenantUser, roleName string) {
	if u.HomeBranchID == nil && u.BranchID != nil && *u.BranchID > 0 {
		hb := *u.BranchID
		u.HomeBranchID = &hb
	}
	u.CanSwitchBranch = IsTenantAdmin(roleName)
}
