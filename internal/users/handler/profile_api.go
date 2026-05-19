package handler

import (
	"strings"

	"tukifac/internal/users/service"

	"github.com/gofiber/fiber/v3"
)

func currentUserID(c fiber.Ctx) uint {
	id, _ := c.Locals("user_id").(uint)
	return id
}

// GET /api/profile/me
func (h *UserHandler) GetMyProfileAPI(c fiber.Ctx) error {
	userID := currentUserID(c)
	if userID == 0 {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "No autorizado"})
	}

	db := tenantDB(c)
	u, err := service.NewUserService(db).GetByID(userID)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Usuario no encontrado"})
	}

	roleName := ""
	var role struct{ Name string }
	if err := db.Table("tenant_roles").Select("name").Where("id = ?", u.RoleID).First(&role).Error; err == nil {
		roleName = role.Name
	}

	branchName := ""
	if u.BranchID != nil {
		var branch struct{ Name string }
		if err := db.Table("tenant_branches").Select("name").Where("id = ?", *u.BranchID).First(&branch).Error; err == nil {
			branchName = branch.Name
		}
	}

	return c.JSON(fiber.Map{
		"data": fiber.Map{
			"id":          u.ID,
			"name":        u.Name,
			"email":       u.Email,
			"phone":       u.Phone,
			"role_id":     u.RoleID,
			"role_name":   roleName,
			"branch_id":   u.BranchID,
			"branch_name": branchName,
			"active":      u.Active,
			"created_at":  u.CreatedAt,
		},
	})
}

// PUT /api/profile/me
func (h *UserHandler) UpdateMyProfileAPI(c fiber.Ctx) error {
	userID := currentUserID(c)
	if userID == 0 {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "No autorizado"})
	}

	var input service.UpdateProfileInput
	if err := c.Bind().JSON(&input); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "JSON inválido"})
	}

	if err := service.NewUserService(tenantDB(c)).UpdateProfile(userID, input); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	u, err := service.NewUserService(tenantDB(c)).GetByID(userID)
	if err != nil {
		return c.JSON(fiber.Map{"success": true})
	}

	roleName := ""
	var role struct{ Name string }
	db := tenantDB(c)
	if err := db.Table("tenant_roles").Select("name").Where("id = ?", u.RoleID).First(&role).Error; err == nil {
		roleName = role.Name
	}

	return c.JSON(fiber.Map{
		"success": true,
		"data": fiber.Map{
			"id":        u.ID,
			"name":      u.Name,
			"email":     u.Email,
			"phone":     u.Phone,
			"role_id":   u.RoleID,
			"role_name": roleName,
			"active":    u.Active,
		},
		"user": fiber.Map{
			"id":    u.ID,
			"name":  u.Name,
			"email": u.Email,
			"role":  roleName,
		},
	})
}

// POST /api/profile/me/password
func (h *UserHandler) ChangeMyPasswordAPI(c fiber.Ctx) error {
	userID := currentUserID(c)
	if userID == 0 {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "No autorizado"})
	}

	var body struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := c.Bind().JSON(&body); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "JSON inválido"})
	}

	current := strings.TrimSpace(body.CurrentPassword)
	next := strings.TrimSpace(body.NewPassword)
	if err := service.NewUserService(tenantDB(c)).ChangePassword(userID, current, next); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"success": true})
}
