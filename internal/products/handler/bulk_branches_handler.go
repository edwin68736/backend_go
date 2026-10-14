package handler

import (
	"tukifac/pkg/productbranch"

	"github.com/gofiber/fiber/v3"
	"gorm.io/gorm"
)

// BulkBranchesAPI PATCH /api/products/bulk-branches — asigna sucursales a varios productos.
//
//   - mode "set": reemplaza las sucursales de cada producto ([] = disponible en todas).
//   - mode "add": agrega las sucursales sin quitar las que ya tenía (un producto que estaba en
//     "todas" queda restringido a las indicadas).
func (h *ProductHandler) BulkBranchesAPI(c fiber.Ctx) error {
	var body struct {
		ProductIDs []uint `json:"product_ids"`
		BranchIDs  []uint `json:"branch_ids"`
		Mode       string `json:"mode"`
	}
	if err := c.Bind().JSON(&body); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "JSON inválido"})
	}
	if len(body.ProductIDs) == 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "se requiere al menos un producto"})
	}
	if body.Mode != "set" && body.Mode != "add" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "mode debe ser 'set' o 'add'"})
	}
	if body.Mode == "add" && len(body.BranchIDs) == 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "indica al menos una sucursal"})
	}
	if err := productbranch.ValidateBranchIDs(db(c), body.BranchIDs); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}
	err := db(c).Transaction(func(tx *gorm.DB) error {
		for _, pid := range body.ProductIDs {
			if pid == 0 {
				continue
			}
			var err error
			if body.Mode == "set" {
				err = productbranch.Set(tx, pid, body.BranchIDs)
			} else {
				err = productbranch.Add(tx, pid, body.BranchIDs)
			}
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"success": true, "updated": len(body.ProductIDs)})
}
