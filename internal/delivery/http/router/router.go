package router

import (
	"github.com/gofiber/fiber/v2"
	regiondelivery "github.com/ilmimris/wilayah-indonesia/internal/delivery/http/region"
)

// RegisterRegionRoutes registers all region endpoints beneath the provided router.
func RegisterRegionRoutes(app fiber.Router, controller *regiondelivery.Controller) {
	controller.Register(app)
}
