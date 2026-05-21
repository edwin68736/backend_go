package handler

import (
	"tukifac/config"

	"github.com/gofiber/fiber/v3"
)

// GET /api/superadmin/platform-domains — layout de dominios para el panel central (sin hardcode).
func PlatformDomainsAPI(c fiber.Ctx) error {
	cfg := config.AppConfig
	if cfg == nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "config no cargada"})
	}
	root := cfg.AppDomain
	return c.JSON(fiber.Map{
		"root_domain":            root,
		"api_public_url":         cfg.APIPublicURL,
		"central_frontend_url":   cfg.CentralFrontendURL,
		"tenant_frontend_url":    cfg.FrontendURL,
		"tenant_host_template":    "{slug}." + root,
		"tenant_url_template":    "https://{slug}." + root,
		"reserved_subdomains":    cfg.ReservedSubdomains,
	})
}
