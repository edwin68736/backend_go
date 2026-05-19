package middleware

import (
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
)

const requestIDHeader = "X-Request-ID"
const requestIDLocal = "request_id"

// RequestID asigna/propaga X-Request-ID para trazabilidad en logs y soporte.
func RequestID() fiber.Handler {
	return func(c fiber.Ctx) error {
		id := c.Get(requestIDHeader)
		if id == "" {
			id = uuid.NewString()
		}
		c.Locals(requestIDLocal, id)
		c.Set(requestIDHeader, id)
		return c.Next()
	}
}

// GetRequestID devuelve el ID del request actual.
func GetRequestID(c fiber.Ctx) string {
	if v, ok := c.Locals(requestIDLocal).(string); ok {
		return v
	}
	return ""
}
