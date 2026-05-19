package response

import "github.com/gofiber/fiber/v3"

type Response struct {
	Success bool        `json:"success"`
	Message string      `json:"message,omitempty"`
	Data    interface{} `json:"data,omitempty"`
	Error   string      `json:"error,omitempty"`
}

func OK(c fiber.Ctx, data interface{}) error {
	return c.JSON(Response{Success: true, Data: data})
}

func Created(c fiber.Ctx, data interface{}) error {
	return c.Status(fiber.StatusCreated).JSON(Response{Success: true, Data: data})
}

func Message(c fiber.Ctx, msg string) error {
	return c.JSON(Response{Success: true, Message: msg})
}

func BadRequest(c fiber.Ctx, msg string) error {
	return c.Status(fiber.StatusBadRequest).JSON(Response{Success: false, Error: msg})
}

func Unauthorized(c fiber.Ctx, msg string) error {
	return c.Status(fiber.StatusUnauthorized).JSON(Response{Success: false, Error: msg})
}

func Forbidden(c fiber.Ctx, msg string) error {
	return c.Status(fiber.StatusForbidden).JSON(Response{Success: false, Error: msg})
}

func NotFound(c fiber.Ctx, msg string) error {
	return c.Status(fiber.StatusNotFound).JSON(Response{Success: false, Error: msg})
}

func InternalError(c fiber.Ctx, msg string) error {
	return c.Status(fiber.StatusInternalServerError).JSON(Response{Success: false, Error: msg})
}
