package handler

import (
	"strconv"
	"strings"

	"tukifac/internal/users/service"

	"github.com/gofiber/fiber/v3"
)

type RoleHandler struct{}

func NewRoleHandler() *RoleHandler { return &RoleHandler{} }

func (h *RoleHandler) ListPage(c fiber.Ctx) error {
	db := tenantDB(c)
	roleSvc := service.NewRoleService(db)
	roles, _ := roleSvc.List()
	return c.Render("users/roles", fiber.Map{
		"Title":     "Roles y Permisos",
		"UserEmail": userEmail(c),
		"Roles":     roles,
		"Success":   c.Query("success"),
	}, "layouts/base")
}

func (h *RoleHandler) NewPage(c fiber.Ctx) error {
	db := tenantDB(c)
	perms, _ := service.NewRoleService(db).AllPermissions()
	return c.Render("users/role_form", fiber.Map{
		"Title":       "Nuevo Rol",
		"UserEmail":   userEmail(c),
		"IsEdit":      false,
		"Permissions": perms,
	}, "layouts/base")
}

func (h *RoleHandler) CreateForm(c fiber.Ctx) error {
	db := tenantDB(c)
	roleSvc := service.NewRoleService(db)
	name := c.FormValue("name")
	desc := c.FormValue("description")

	role, err := roleSvc.Create(name, desc)
	if err != nil {
		perms, _ := roleSvc.AllPermissions()
		return c.Render("users/role_form", fiber.Map{
			"Title":       "Nuevo Rol",
			"UserEmail":   userEmail(c),
			"IsEdit":      false,
			"Error":       err.Error(),
			"Permissions": perms,
		}, "layouts/base")
	}

	// Asignar permisos
	permIDs := parsePermissionIDs(c.FormValue("permission_ids"))
	if len(permIDs) > 0 {
		roleSvc.SetRolePermissions(role.ID, permIDs)
	}

	return c.Redirect().To("/roles?success=created")
}

func (h *RoleHandler) EditPage(c fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 32)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).SendString("ID inválido")
	}
	db := tenantDB(c)
	roleSvc := service.NewRoleService(db)
	role, err := roleSvc.GetByID(uint(id))
	if err != nil {
		return c.Status(fiber.StatusNotFound).SendString("Rol no encontrado")
	}
	perms, _ := roleSvc.AllPermissions()
	assigned, _ := roleSvc.RolePermissions(uint(id))

	assignedMap := make(map[uint]bool)
	for _, pid := range assigned {
		assignedMap[pid] = true
	}

	return c.Render("users/role_form", fiber.Map{
		"Title":       "Editar Rol",
		"UserEmail":   userEmail(c),
		"IsEdit":      true,
		"Role":        role,
		"Permissions": perms,
		"AssignedMap": assignedMap,
	}, "layouts/base")
}

func (h *RoleHandler) UpdateForm(c fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 32)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).SendString("ID inválido")
	}
	db := tenantDB(c)
	roleSvc := service.NewRoleService(db)

	if err := roleSvc.Update(uint(id), c.FormValue("name"), c.FormValue("description")); err != nil {
		return c.Status(fiber.StatusBadRequest).SendString(err.Error())
	}

	permIDs := parsePermissionIDs(c.FormValue("permission_ids"))
	roleSvc.SetRolePermissions(uint(id), permIDs)

	return c.Redirect().To("/roles?success=updated")
}

func (h *RoleHandler) DeleteForm(c fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 32)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).SendString("ID inválido")
	}
	if err := service.NewRoleService(tenantDB(c)).Delete(uint(id)); err != nil {
		return c.Status(fiber.StatusBadRequest).SendString(err.Error())
	}
	return c.Redirect().To("/roles?success=deleted")
}

func parsePermissionIDs(raw string) []uint {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	ids := make([]uint, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if v, err := strconv.ParseUint(p, 10, 32); err == nil {
			ids = append(ids, uint(v))
		}
	}
	return ids
}
