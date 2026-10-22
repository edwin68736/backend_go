package tenantmigrations

import (
	"tukifac/pkg/database"

	"gorm.io/gorm"
)

// V165POSStockAndProductCostPermissions agrega dos permisos a las acciones de información rápida
// de las tarjetas de producto del POS:
//
//   - pos.view_stock: botón «Ver stock» (GET /inventory/stock/:productId). Se reparte a TODOS los
//     roles existentes para que nadie lo pierda el día del deploy (antes el endpoint exigía
//     inventory.view o, desde el commit anterior, nada); el dueño puede quitárselo a un rol después.
//   - products.view_cost: ver y editar el costo (purchase_price). Sin él el servidor omite el costo
//     de las respuestas y no deja que una edición lo sobrescriba. Se reparte solo a quien ya lo
//     manejaba: Administrador, roles que crean/editan/eliminan/gestionan productos, roles con
//     cualquier permiso de compras y roles que crean documentos/ajustes de inventario. Los roles nuevos nacen sin él.
//
// Idempotente. Catálogo espejo: internal/users/service/role_service.go (SeedPermissions).
type V165POSStockAndProductCostPermissions struct{}

func (V165POSStockAndProductCostPermissions) Version() int { return 165 }
func (V165POSStockAndProductCostPermissions) Name() string {
	return "pos_stock_and_product_cost_permissions"
}

func (V165POSStockAndProductCostPermissions) Up(db *gorm.DB) error {
	if !db.Migrator().HasTable(&database.TenantRole{}) || !db.Migrator().HasTable(&database.TenantPermission{}) ||
		!db.Migrator().HasTable(&database.TenantRolePermission{}) {
		return nil
	}

	var stockPerm, costPerm database.TenantPermission
	if err := db.Where(database.TenantPermission{Module: "pos", Action: "view_stock"}).
		Attrs(database.TenantPermission{Label: "Ver stock por sucursal en el punto de venta"}).
		FirstOrCreate(&stockPerm).Error; err != nil {
		return err
	}
	if err := db.Where(database.TenantPermission{Module: "products", Action: "view_cost"}).
		Attrs(database.TenantPermission{Label: "Ver y editar el costo de los productos"}).
		FirstOrCreate(&costPerm).Error; err != nil {
		return err
	}

	var roles []database.TenantRole
	if err := db.Find(&roles).Error; err != nil {
		return err
	}

	for _, role := range roles {
		if err := grantIfMissing(db, role.ID, stockPerm.ID); err != nil {
			return err
		}

		manages, err := v165RoleHandlesCost(db, role)
		if err != nil {
			return err
		}
		if manages {
			if err := grantIfMissing(db, role.ID, costPerm.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

// v165RoleHandlesCost: Administrador, o un rol que ya gestiona productos, compras o ingresos de
// inventario (esas pantallas usan el costo).
func v165RoleHandlesCost(db *gorm.DB, role database.TenantRole) (bool, error) {
	if role.Name == "Administrador" {
		return true, nil
	}
	var n int64
	err := db.Model(&database.TenantRolePermission{}).
		Joins("JOIN tenant_permissions ON tenant_permissions.id = tenant_role_permissions.permission_id").
		Where("tenant_role_permissions.role_id = ?", role.ID).
		Where("(tenant_permissions.module = ? AND tenant_permissions.action IN ?) OR (tenant_permissions.module = ? AND tenant_permissions.action IN ?) OR tenant_permissions.module = ?",
			"products", []string{"create", "edit", "delete", "manage"},
			// Los documentos de ingreso/ajuste de inventario precargan el costo del producto.
			"inventory", []string{"manage", "create_document", "adjust", "import_adjustment"},
			"purchases").
		Count(&n).Error
	return n > 0, err
}

func grantIfMissing(db *gorm.DB, roleID, permID uint) error {
	var has int64
	if err := db.Model(&database.TenantRolePermission{}).
		Where("role_id = ? AND permission_id = ?", roleID, permID).Count(&has).Error; err != nil {
		return err
	}
	if has > 0 {
		return nil
	}
	return db.Create(&database.TenantRolePermission{RoleID: roleID, PermissionID: permID}).Error
}
