package ubigeo

import (
	"tukifac/pkg/database"

	"github.com/gofiber/fiber/v3"
	"gorm.io/gorm"
)

// CentralHandler sirve ubigeo desde la BD central (panel superadmin).
type CentralHandler struct{ db *gorm.DB }

func NewCentralHandler() *CentralHandler {
	return &CentralHandler{db: database.CentralDB}
}

// GET /api/superadmin/ubigeo/regiones
func (h *CentralHandler) RegionesAPI(c fiber.Ctx) error {
	var list []database.UbiRegion
	if err := h.db.Order("id ASC").Find(&list).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"data": list})
}

// GET /api/superadmin/ubigeo/provincias?region_id=150000
func (h *CentralHandler) ProvinciasAPI(c fiber.Ctx) error {
	regionID := c.Query("region_id")
	if regionID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "region_id es requerido"})
	}
	var list []database.UbiProvincia
	if err := h.db.Where("region_id = ?", regionID).Order("id ASC").Find(&list).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"data": list})
}

// GET /api/superadmin/ubigeo/distritos?provincia_id=150101
func (h *CentralHandler) DistritosAPI(c fiber.Ctx) error {
	provinciaID := c.Query("provincia_id")
	if provinciaID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "provincia_id es requerido"})
	}
	var list []database.UbiDistrito
	if err := h.db.Where("provincia_id = ?", provinciaID).Order("id ASC").Find(&list).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"data": list})
}

// TenantHandler sirve ubigeo desde la BD del tenant (panel tenant).
type TenantHandler struct{}

func NewTenantHandler() *TenantHandler { return &TenantHandler{} }

func db(c fiber.Ctx) *gorm.DB {
	v, _ := c.Locals("tenantDB").(*gorm.DB)
	return v
}

// GET /api/ubigeo/regiones
func (h *TenantHandler) RegionesAPI(c fiber.Ctx) error {
	d := db(c)
	if d == nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "sin contexto tenant"})
	}
	var list []database.UbiRegion
	if err := d.Order("id ASC").Find(&list).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"data": list})
}

// GET /api/ubigeo/provincias?region_id=150000
func (h *TenantHandler) ProvinciasAPI(c fiber.Ctx) error {
	d := db(c)
	if d == nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "sin contexto tenant"})
	}
	regionID := c.Query("region_id")
	if regionID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "region_id es requerido"})
	}
	var list []database.UbiProvincia
	if err := d.Where("region_id = ?", regionID).Order("id ASC").Find(&list).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"data": list})
}

// GET /api/ubigeo/distritos?provincia_id=150101
func (h *TenantHandler) DistritosAPI(c fiber.Ctx) error {
	d := db(c)
	if d == nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "sin contexto tenant"})
	}
	provinciaID := c.Query("provincia_id")
	if provinciaID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "provincia_id es requerido"})
	}
	var list []database.UbiDistrito
	if err := d.Where("provincia_id = ?", provinciaID).Order("id ASC").Find(&list).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"data": list})
}
