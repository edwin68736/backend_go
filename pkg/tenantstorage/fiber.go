package tenantstorage

import (
	"errors"
	"fmt"

	"tukifac/pkg/database"

	"github.com/gofiber/fiber/v3"
	"gorm.io/gorm"
)

// ResolveTenantRUC obtiene el RUC del tenant (central o configuración de empresa).
func ResolveTenantRUC(c fiber.Ctx) (string, error) {
	if r, ok := c.Locals("tenant_ruc").(string); ok && r != "" {
		return r, nil
	}
	if t, ok := c.Locals("tenant").(*database.Tenant); ok && t != nil {
		if r := SanitizeRUC(t.RUC); r != "" {
			c.Locals("tenant_ruc", r)
			return r, nil
		}
	}
	if db, ok := c.Locals("tenantDB").(*gorm.DB); ok && db != nil {
		var cfg database.TenantCompanyConfig
		if err := db.First(&cfg).Error; err == nil {
			if r := SanitizeRUC(cfg.RUC); r != "" {
				c.Locals("tenant_ruc", r)
				return r, nil
			}
		}
	}
	return "", errors.New("RUC de la empresa no configurado")
}

// TenantRUCFromID carga el RUC desde la BD central (panel superadmin).
func TenantRUCFromID(tenantID uint) (string, error) {
	var tenant database.Tenant
	if err := database.CentralDB.First(&tenant, tenantID).Error; err != nil {
		return "", fmt.Errorf("empresa no encontrada")
	}
	ruc := SanitizeRUC(tenant.RUC)
	if ruc == "" {
		return "", fmt.Errorf("la empresa no tiene RUC registrado")
	}
	return ruc, nil
}
