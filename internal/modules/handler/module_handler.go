package handler

import (
	"tukifac/internal/modules/service"

	"github.com/gofiber/fiber/v3"
	"gorm.io/gorm"
)

type ModuleHandler struct{}

func NewModuleHandler() *ModuleHandler { return &ModuleHandler{} }

func db(c fiber.Ctx) *gorm.DB {
	v, _ := c.Locals("tenantDB").(*gorm.DB)
	return v
}
func email(c fiber.Ctx) string {
	v, _ := c.Locals("user_email").(string)
	return v
}

func (h *ModuleHandler) ListPage(c fiber.Ctx) error {
	svc := service.NewModuleService(db(c))
	modules, _ := svc.List()
	return c.Render("modules/index", fiber.Map{
		"Title":     "Módulos Externos",
		"UserEmail": email(c),
		"Modules":   modules,
		"Success":   c.Query("success"),
	}, "layouts/base")
}

func (h *ModuleHandler) RegisterForm(c fiber.Ctx) error {
	svc := service.NewModuleService(db(c))
	_, err := svc.Register(
		c.FormValue("module_key"),
		c.FormValue("name"),
		c.FormValue("base_url"),
		c.FormValue("api_key"),
	)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).SendString(err.Error())
	}
	return c.Redirect().To("/modules?success=registered")
}

func (h *ModuleHandler) ToggleAPI(c fiber.Ctx) error {
	svc := service.NewModuleService(db(c))
	var body struct {
		Enabled bool `json:"enabled"`
	}
	c.Bind().Body(&body)
	if err := svc.SetEnabled(c.Params("key"), body.Enabled); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"success": true})
}

// ForwardAPI hace de proxy hacia un módulo externo.
func (h *ModuleHandler) ForwardAPI(c fiber.Ctx) error {
	svc := service.NewModuleService(db(c))
	moduleKey := c.Params("key")
	path := "/" + c.Params("*")

	body := c.Body()
	respBody, statusCode, err := svc.Forward(moduleKey, path, c.Method(), body, map[string]string{})
	if err != nil {
		return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": err.Error()})
	}

	c.Set("Content-Type", "application/json")
	return c.Status(statusCode).Send(respBody)
}

func (h *ModuleHandler) PingAPI(c fiber.Ctx) error {
	svc := service.NewModuleService(db(c))
	if err := svc.Ping(c.Params("key")); err != nil {
		return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"success": true, "message": "Módulo respondiendo correctamente"})
}
