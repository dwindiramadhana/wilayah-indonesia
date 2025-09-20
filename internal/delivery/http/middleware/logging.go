package middleware

import (
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/logger"
)

// RequestLogger applies Fiber's logger middleware with default settings.
func RequestLogger() fiber.Handler {
	return logger.New()
}
