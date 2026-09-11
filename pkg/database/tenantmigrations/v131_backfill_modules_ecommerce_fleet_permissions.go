package tenantmigrations

import (
	"errors"

	"tukifac/pkg/database"

	"gorm.io/gorm"
)

// V131BackfillModulesEcommerceFleetPermissions concede a todos los roles existentes los permisos
// modules.manage, ecommerce.{view,manage} y fleet.{view,manage}.
//
// internal/modules, internal/ecommerce e internal/fleet no exigían ningún permiso de rol (modules
// usaba el rol hardcodeado "Administrador"; ecommerce y fleet solo el módulo del plan). El catálogo
// ahora sí los define y las rutas ya los exigen (ver internal/{modules,ecommerce,fleet}/routes.go).
// Mismo criterio que V130: sin este backfill, el deploy le quitaría acceso a cualquier rol que hoy
// usa esas pantallas con normalidad — nunca se validó el permiso, así que casi ningún rol lo tenía
// marcado aunque lo necesitara.
type V131BackfillModulesEcommerceFleetPermissions struct{}

func (V131BackfillModulesEcommerceFleetPermissions) Version() int { return 131 }
func (V131BackfillModulesEcommerceFleetPermissions) Name() string {
	return "backfill_modules_ecommerce_fleet_permissions"
}

// v131ModuleActions permisos recién exigidos que se retro-conceden a todos los roles existentes.
var v131ModuleActions = [][2]string{
	{"modules", "manage"},
	{"ecommerce", "view"}, {"ecommerce", "manage"},
	{"fleet", "view"}, {"fleet", "manage"},
}

func (V131BackfillModulesEcommerceFleetPermissions) Up(db *gorm.DB) error {
	if !db.Migrator().HasTable(&database.TenantRole{}) || !db.Migrator().HasTable(&database.TenantPermission{}) {
		return nil
	}

	var permIDs []uint
	for _, ma := range v131ModuleActions {
		var perm database.TenantPermission
		err := db.Where("module = ? AND action = ?", ma[0], ma[1]).First(&perm).Error
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				// Catálogo aún no sembrado en este tenant (p. ej. provisioning en curso): nada
				// que retro-conceder todavía, RoleService.SeedPermissions lo completará.
				continue
			}
			return err
		}
		permIDs = append(permIDs, perm.ID)
	}
	if len(permIDs) == 0 {
		return nil
	}

	var roles []database.TenantRole
	if err := db.Find(&roles).Error; err != nil {
		return err
	}

	for _, role := range roles {
		var already []uint
		if err := db.Model(&database.TenantRolePermission{}).
			Where("role_id = ? AND permission_id IN ?", role.ID, permIDs).
			Pluck("permission_id", &already).Error; err != nil {
			return err
		}
		have := make(map[uint]struct{}, len(already))
		for _, id := range already {
			have[id] = struct{}{}
		}
		for _, pid := range permIDs {
			if _, ok := have[pid]; ok {
				continue
			}
			if err := db.Create(&database.TenantRolePermission{RoleID: role.ID, PermissionID: pid}).Error; err != nil {
				return err
			}
		}
	}
	return nil
}
