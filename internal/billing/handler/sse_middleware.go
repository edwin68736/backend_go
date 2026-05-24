package handler

import (
	"strings"

	"github.com/gofiber/fiber/v3"
)

// SSEAccessTokenMiddleware permite EventSource autenticado vía ?access_token= (HTTPS).
func SSEAccessTokenMiddleware(c fiber.Ctx) error {
	if strings.TrimSpace(c.Get("Authorization")) == "" {
		if token := strings.TrimSpace(c.Query("access_token")); token != "" {
			c.Request().Header.Set("Authorization", "Bearer "+token)
		}
	}
	return c.Next()
}
