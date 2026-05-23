package handler

import (
	"strconv"

	"tukifac/pkg/database"
	"tukifac/pkg/saas/docusage"

	"github.com/gofiber/fiber/v3"
)

type Handler struct{}

func NewHandler() *Handler { return &Handler{} }

func (h *Handler) ListCatalogAPI(c fiber.Ctx) error {
	rows, err := docusage.ListCatalogAdmin()
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"packages": rows})
}

func (h *Handler) UpsertCatalogAPI(c fiber.Ctx) error {
	var in docusage.UpsertPackageInput
	if err := c.Bind().Body(&in); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Datos inválidos"})
	}
	if id, err := strconv.ParseUint(c.Params("id"), 10, 32); err == nil && id > 0 {
		in.ID = uint(id)
	}
	row, err := docusage.UpsertCatalogPackage(in)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(row)
}

func (h *Handler) DeleteCatalogAPI(c fiber.Ctx) error {
	id, _ := strconv.ParseUint(c.Params("id"), 10, 32)
	if err := database.CentralDB.Model(&database.SaasDocumentPackage{}).Where("id = ?", id).
		Update("is_active", false).Error; err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"success": true})
}

func (h *Handler) ListPendingAPI(c fiber.Ctx) error {
	rows, err := docusage.ListPendingPackages()
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	type rowView struct {
		database.SaasTenantDocumentPackage
		TenantName  string `json:"tenant_name"`
		PackageName string `json:"package_name"`
	}
	out := make([]rowView, 0, len(rows))
	for _, r := range rows {
		v := rowView{SaasTenantDocumentPackage: r}
		var t database.Tenant
		if database.CentralDB.First(&t, r.TenantID).Error == nil {
			v.TenantName = t.Name
		}
		var p database.SaasDocumentPackage
		if database.CentralDB.First(&p, r.PackageID).Error == nil {
			v.PackageName = p.Name
		}
		out = append(out, v)
	}
	return c.JSON(fiber.Map{"requests": out})
}

func (h *Handler) ApproveAPI(c fiber.Ctx) error {
	id, _ := strconv.ParseUint(c.Params("id"), 10, 32)
	adminID, _ := c.Locals("superadmin_id").(uint)
	var body struct {
		Notes string `json:"notes"`
	}
	_ = c.Bind().Body(&body)
	if err := docusage.ApproveTenantPackage(uint(id), adminID, body.Notes); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"success": true})
}

func (h *Handler) RejectAPI(c fiber.Ctx) error {
	id, _ := strconv.ParseUint(c.Params("id"), 10, 32)
	adminID, _ := c.Locals("superadmin_id").(uint)
	var body struct {
		Reason string `json:"reason"`
	}
	if err := c.Bind().Body(&body); err != nil || body.Reason == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "motivo requerido"})
	}
	if err := docusage.RejectTenantPackage(uint(id), adminID, body.Reason); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"success": true})
}
