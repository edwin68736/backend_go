package handler

import (
	"errors"
	"strconv"
	"strings"
	"time"

	"tukifac/config"
	"tukifac/pkg/database"
	"tukifac/pkg/middleware"

	"github.com/gofiber/fiber/v3"
	"github.com/golang-jwt/jwt/v5"
	"gorm.io/gorm"
)

type AuthSAHandler struct{}

func NewAuthSAHandler() *AuthSAHandler { return &AuthSAHandler{} }

func saRequireSuperAdminRole(c fiber.Ctx) error {
	role, _ := c.Locals("sa_user_role").(string)
	if strings.TrimSpace(role) != "superadmin" {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "No autorizado"})
	}
	return nil
}

// POST /api/superadmin/login
func (h *AuthSAHandler) LoginAPI(c fiber.Ctx) error {
	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := c.Bind().JSON(&body); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "JSON inválido"})
	}
	if body.Email == "" || body.Password == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "email y password son requeridos"})
	}

	var user database.SuperAdminUser
	if err := database.CentralDB.Where("email = ?", body.Email).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Credenciales inválidas"})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error interno"})
	}

	if !user.CheckPassword(body.Password) {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Credenciales inválidas"})
	}

	claims := &middleware.SuperAdminClaims{
		UserID: user.ID,
		Email:  user.Email,
		Role:   user.Role,
		Type:   "superadmin",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(8 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(config.AppConfig.SAJWTSecret))
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error generando token"})
	}

	return c.JSON(fiber.Map{
		"token":      tokenString,
		"expires_in": 28800,
		"user": fiber.Map{
			"id":    user.ID,
			"email": user.Email,
			"role":  user.Role,
		},
	})
}

// GET /api/superadmin/users
func (h *AuthSAHandler) ListUsersAPI(c fiber.Ctx) error {
	var users []database.SuperAdminUser
	if err := database.CentralDB.Order("id asc").Find(&users).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error interno"})
	}

	out := make([]fiber.Map, 0, len(users))
	for _, u := range users {
		out = append(out, fiber.Map{
			"id":         u.ID,
			"name":       u.Name,
			"email":      u.Email,
			"role":       u.Role,
			"created_at": u.CreatedAt,
			"updated_at": u.UpdatedAt,
		})
	}
	return c.JSON(fiber.Map{"data": out})
}

// POST /api/superadmin/users
func (h *AuthSAHandler) CreateUserAPI(c fiber.Ctx) error {
	if err := saRequireSuperAdminRole(c); err != nil {
		return err
	}

	var body struct {
		Name     string `json:"name"`
		Email    string `json:"email"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	if err := c.Bind().JSON(&body); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "JSON inválido"})
	}

	name := strings.TrimSpace(body.Name)
	email := strings.TrimSpace(strings.ToLower(body.Email))
	password := strings.TrimSpace(body.Password)
	role := strings.TrimSpace(body.Role)
	if role == "" {
		role = "admin"
	}

	if name == "" || email == "" || password == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "name, email y password son requeridos"})
	}
	if len(password) < 8 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "La contraseña debe tener mínimo 8 caracteres"})
	}
	if role != "admin" && role != "superadmin" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "role debe ser admin o superadmin"})
	}

	var existing database.SuperAdminUser
	if err := database.CentralDB.Where("email = ?", email).First(&existing).Error; err == nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "El email ya está registrado"})
	} else if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error interno"})
	}

	user := &database.SuperAdminUser{
		Name:  name,
		Email: email,
		Role:  role,
	}
	if err := user.SetPassword(password); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error interno"})
	}
	if err := database.CentralDB.Create(user).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error interno"})
	}

	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"success": true,
		"data": fiber.Map{
			"id":         user.ID,
			"name":       user.Name,
			"email":      user.Email,
			"role":       user.Role,
			"created_at": user.CreatedAt,
		},
	})
}

// PUT /api/superadmin/users/:id
func (h *AuthSAHandler) UpdateUserAPI(c fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 32)
	if err != nil || id == 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "ID inválido"})
	}

	var body struct {
		Name  *string `json:"name"`
		Email *string `json:"email"`
		Role  *string `json:"role"`
	}
	if err := c.Bind().JSON(&body); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "JSON inválido"})
	}

	var user database.SuperAdminUser
	if err := database.CentralDB.First(&user, uint(id)).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Usuario no encontrado"})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error interno"})
	}

	role, _ := c.Locals("sa_user_role").(string)
	role = strings.TrimSpace(role)
	currentUserID, _ := c.Locals("sa_user_id").(uint)
	if role != "superadmin" {
		if currentUserID == 0 || uint(id) != currentUserID {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "No autorizado"})
		}
	}

	updates := map[string]interface{}{}
	if body.Name != nil {
		v := strings.TrimSpace(*body.Name)
		if v == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "name no puede ser vacío"})
		}
		updates["name"] = v
	}
	if body.Email != nil {
		v := strings.TrimSpace(strings.ToLower(*body.Email))
		if v == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "email no puede ser vacío"})
		}
		var existing database.SuperAdminUser
		if err := database.CentralDB.Where("email = ? AND id <> ?", v, user.ID).First(&existing).Error; err == nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "El email ya está registrado"})
		} else if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error interno"})
		}
		updates["email"] = v
	}
	if body.Role != nil {
		if role != "superadmin" {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "No autorizado"})
		}
		v := strings.TrimSpace(*body.Role)
		if v != "admin" && v != "superadmin" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "role debe ser admin o superadmin"})
		}
		updates["role"] = v
	}

	if len(updates) == 0 {
		return c.JSON(fiber.Map{"success": true})
	}
	if err := database.CentralDB.Model(&user).Updates(updates).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error interno"})
	}
	return c.JSON(fiber.Map{"success": true})
}

// POST /api/superadmin/users/:id/password
func (h *AuthSAHandler) ResetUserPasswordAPI(c fiber.Ctx) error {
	if err := saRequireSuperAdminRole(c); err != nil {
		return err
	}

	id, err := strconv.ParseUint(c.Params("id"), 10, 32)
	if err != nil || id == 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "ID inválido"})
	}

	var body struct {
		NewPassword string `json:"new_password"`
	}
	if err := c.Bind().JSON(&body); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "JSON inválido"})
	}

	newPassword := strings.TrimSpace(body.NewPassword)
	if newPassword == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "new_password es requerido"})
	}
	if len(newPassword) < 8 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "La nueva contraseña debe tener mínimo 8 caracteres"})
	}

	var user database.SuperAdminUser
	if err := database.CentralDB.First(&user, uint(id)).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Usuario no encontrado"})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error interno"})
	}

	if err := user.SetPassword(newPassword); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error interno"})
	}
	if err := database.CentralDB.Model(&user).Update("password", user.Password).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error interno"})
	}
	return c.JSON(fiber.Map{"success": true})
}

// POST /api/superadmin/me/password
func (h *AuthSAHandler) ChangeMyPasswordAPI(c fiber.Ctx) error {
	userIDAny := c.Locals("sa_user_id")
	userID, _ := userIDAny.(uint)
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

	currentPassword := strings.TrimSpace(body.CurrentPassword)
	newPassword := strings.TrimSpace(body.NewPassword)
	if currentPassword == "" || newPassword == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "current_password y new_password son requeridos"})
	}
	if len(newPassword) < 8 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "La nueva contraseña debe tener mínimo 8 caracteres"})
	}

	var user database.SuperAdminUser
	if err := database.CentralDB.First(&user, userID).Error; err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "No autorizado"})
	}
	if !user.CheckPassword(currentPassword) {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "La contraseña actual no es correcta"})
	}

	if err := user.SetPassword(newPassword); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error interno"})
	}
	if err := database.CentralDB.Model(&user).Update("password", user.Password).Error; err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Error interno"})
	}
	return c.JSON(fiber.Map{"success": true})
}
