package service

import (
	"errors"

	"tukifac/pkg/database"

	"gorm.io/gorm"
)

type RoleService struct {
	db *gorm.DB
}

func NewRoleService(db *gorm.DB) *RoleService {
	return &RoleService{db: db}
}

func (s *RoleService) List() ([]database.TenantRole, error) {
	var roles []database.TenantRole
	err := s.db.Order("name ASC").Find(&roles).Error
	return roles, err
}

func (s *RoleService) GetByID(id uint) (*database.TenantRole, error) {
	var role database.TenantRole
	if err := s.db.First(&role, id).Error; err != nil {
		return nil, err
	}
	return &role, nil
}

func (s *RoleService) Create(name, description string) (*database.TenantRole, error) {
	if name == "" {
		return nil, errors.New("el nombre del rol es requerido")
	}
	var existing database.TenantRole
	if err := s.db.Where("name = ?", name).First(&existing).Error; err == nil {
		return nil, errors.New("ya existe un rol con ese nombre")
	}
	role := &database.TenantRole{Name: name, Description: description}
	err := s.db.Create(role).Error
	return role, err
}

func (s *RoleService) Update(id uint, name, description string) error {
	return s.db.Model(&database.TenantRole{}).Where("id = ?", id).Updates(map[string]interface{}{
		"name":        name,
		"description": description,
	}).Error
}

func (s *RoleService) Delete(id uint) error {
	var role database.TenantRole
	if err := s.db.First(&role, id).Error; err != nil {
		return err
	}
	if role.IsSystem {
		return errors.New("no se puede eliminar un rol del sistema")
	}
	return s.db.Delete(&role).Error
}

// AllPermissions retorna todos los permisos disponibles.
func (s *RoleService) AllPermissions() ([]database.TenantPermission, error) {
	var perms []database.TenantPermission
	err := s.db.Order("module ASC, action ASC").Find(&perms).Error
	return perms, err
}

// RolePermissions retorna los IDs de permisos asignados a un rol.
func (s *RoleService) RolePermissions(roleID uint) ([]uint, error) {
	var rps []database.TenantRolePermission
	if err := s.db.Where("role_id = ?", roleID).Find(&rps).Error; err != nil {
		return nil, err
	}
	ids := make([]uint, len(rps))
	for i, rp := range rps {
		ids[i] = rp.PermissionID
	}
	return ids, nil
}

// SetRolePermissions reemplaza todos los permisos de un rol.
func (s *RoleService) SetRolePermissions(roleID uint, permissionIDs []uint) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("role_id = ?", roleID).Delete(&database.TenantRolePermission{}).Error; err != nil {
			return err
		}
		for _, pid := range permissionIDs {
			if err := tx.Create(&database.TenantRolePermission{
				RoleID: roleID, PermissionID: pid,
			}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// GetRolePermissionKeys devuelve los permisos del rol en formato "module.action" para el JWT.
func (s *RoleService) GetRolePermissionKeys(roleID uint) ([]string, error) {
	var perms []database.TenantPermission
	err := s.db.Table("tenant_permissions").
		Select("tenant_permissions.module, tenant_permissions.action").
		Joins("INNER JOIN tenant_role_permissions ON tenant_role_permissions.permission_id = tenant_permissions.id").
		Where("tenant_role_permissions.role_id = ?", roleID).
		Find(&perms).Error
	if err != nil {
		return nil, err
	}
	keys := make([]string, len(perms))
	for i, p := range perms {
		keys[i] = p.Module + "." + p.Action
	}
	return keys, nil
}

// SeedPermissions inserta los permisos base si no existen.
func (s *RoleService) SeedPermissions() error {
	var count int64
	s.db.Model(&database.TenantPermission{}).Count(&count)
	if count > 0 {
		return nil
	}

	perms := []database.TenantPermission{
		{Module: "dashboard", Action: "view", Label: "Ver dashboard"},
		{Module: "users", Action: "view", Label: "Ver usuarios"},
		{Module: "users", Action: "create", Label: "Crear usuarios"},
		{Module: "users", Action: "edit", Label: "Editar usuarios"},
		{Module: "users", Action: "delete", Label: "Eliminar usuarios"},
		{Module: "roles", Action: "view", Label: "Ver roles"},
		{Module: "roles", Action: "manage", Label: "Gestionar roles"},
		{Module: "company", Action: "view", Label: "Ver configuración de empresa"},
		{Module: "company", Action: "edit", Label: "Editar configuración de empresa"},
		{Module: "contacts", Action: "view", Label: "Ver contactos"},
		{Module: "contacts", Action: "create", Label: "Crear contactos"},
		{Module: "contacts", Action: "edit", Label: "Editar contactos"},
		{Module: "contacts", Action: "delete", Label: "Eliminar contactos"},
		{Module: "products", Action: "view", Label: "Ver productos"},
		{Module: "products", Action: "create", Label: "Crear productos"},
		{Module: "products", Action: "edit", Label: "Editar productos"},
		{Module: "products", Action: "delete", Label: "Eliminar productos"},
		{Module: "inventory", Action: "view", Label: "Ver inventario"},
		{Module: "inventory", Action: "manage", Label: "Gestionar inventario"},
		{Module: "sales", Action: "view", Label: "Ver ventas"},
		{Module: "sales", Action: "create", Label: "Crear ventas"},
		{Module: "sales", Action: "edit", Label: "Editar ventas"},
		{Module: "sales", Action: "delete", Label: "Eliminar ventas"},
		{Module: "sales", Action: "cancel", Label: "Anular ventas"},
		{Module: "sales", Action: "pos", Label: "Usar punto de venta"},
		{Module: "purchases", Action: "view", Label: "Ver compras"},
		{Module: "purchases", Action: "create", Label: "Crear compras"},
		{Module: "purchases", Action: "edit", Label: "Editar compras"},
		{Module: "purchases", Action: "delete", Label: "Eliminar compras"},
		{Module: "cashbank", Action: "view", Label: "Ver caja y bancos"},
		{Module: "cashbank", Action: "manage", Label: "Gestionar caja y bancos"},
		{Module: "cashbank", Action: "open", Label: "Abrir caja"},
		{Module: "cashbank", Action: "close", Label: "Cerrar caja"},
		{Module: "cashbank", Action: "movements", Label: "Movimientos de caja"},
		{Module: "reports", Action: "view", Label: "Ver reportes"},
		{Module: "billing", Action: "send", Label: "Enviar a SUNAT"},
		{Module: "memberships", Action: "view", Label: "Ver membresías"},
		{Module: "memberships", Action: "create", Label: "Crear membresías"},
		{Module: "memberships", Action: "edit", Label: "Editar membresías"},
		{Module: "memberships", Action: "delete", Label: "Eliminar membresías"},
		{Module: "memberships", Action: "generate_sale", Label: "Generar venta desde membresía"},
	}

	return s.db.Create(&perms).Error
}
