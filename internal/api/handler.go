package api

import (
	"database/sql"
	"fmt"
	"log/slog"
	"strconv"

	"github.com/gofiber/fiber/v2"
	"github.com/ilmimris/wilayah-indonesia/pkg/service"
)

// Handler wraps the service to provide HTTP handlers.
type Handler struct {
	svc *service.Service
}

// New creates a new Handler instance with the provided service.
func New(svc *service.Service) *Handler {
	return &Handler{
		svc: svc,
	}
}

func parseBoolQueryParam(c *fiber.Ctx, key string) (bool, error) {
	value := c.Query(key)
	if value == "" {
		return false, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("invalid boolean value for '%s'", key)
	}
	return parsed, nil
}

func parseSearchOptions(c *fiber.Ctx) (service.SearchOptions, error) {
	var opts service.SearchOptions

	if value := c.Query("limit"); value != "" {
		limit, err := strconv.Atoi(value)
		if err != nil {
			return opts, fmt.Errorf("invalid integer value for 'limit'")
		}
		opts.Limit = limit
	}

	if parsed, err := parseBoolQueryParam(c, "search_bps"); err != nil {
		return opts, err
	} else {
		opts.SearchBPS = parsed
	}

	if parsed, err := parseBoolQueryParam(c, "include_bps"); err != nil {
		return opts, err
	} else {
		opts.IncludeBPS = parsed
	}

	if parsed, err := parseBoolQueryParam(c, "include_scores"); err != nil {
		return opts, err
	} else {
		opts.IncludeScores = parsed
	}

	return opts, nil
}

// SearchHandler handles the search endpoint
func (h *Handler) SearchHandler() fiber.Handler {
	return func(c *fiber.Ctx) error {
		opts, err := parseSearchOptions(c)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": err.Error(),
			})
		}

		// Extract query parameters
		searchQuery := service.SearchQuery{
			Query:       c.Query("q"),
			Subdistrict: c.Query("subdistrict"),
			District:    c.Query("district"),
			City:        c.Query("city"),
			Province:    c.Query("province"),
			Options:     opts,
		}

		// Validate that at least one query parameter is provided
		if searchQuery.Query == "" && searchQuery.Subdistrict == "" && searchQuery.District == "" && searchQuery.City == "" && searchQuery.Province == "" {
			slog.Warn("Search query parameter missing", "ip", c.IP())
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "At least one query parameter (q, subdistrict, district, city, province) is required",
			})
		}

		// Use the service to perform the search
		results, err := h.svc.Search(searchQuery)
		if err != nil {
			if service.IsError(err, service.ErrCodeInvalidInput) {
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
					"error": err.Error(),
				})
			}
			if service.IsError(err, service.ErrCodeDatabaseFailure) {
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
					"error": "Database query failed",
				})
			}
			// Default to internal server error for any other errors
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": err.Error(),
			})
		}

		// Return JSON response
		return c.JSON(results)
	}
}

// DistrictSearchHandler handles the district search endpoint
func (h *Handler) DistrictSearchHandler() fiber.Handler {
	return func(c *fiber.Ctx) error {
		// Extract and validate the q query parameter
		query := c.Query("q")
		if query == "" {
			slog.Warn("District search query parameter missing", "ip", c.IP())
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "Query parameter 'q' is required",
			})
		}

		// Optional narrowing filters
		city := c.Query("city")
		province := c.Query("province")

		// Use the service to perform the search
		opts, err := parseSearchOptions(c)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": err.Error(),
			})
		}

		results, err := h.svc.SearchByDistrict(query, city, province, opts)
		if err != nil {
			if service.IsError(err, service.ErrCodeInvalidInput) {
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
					"error": err.Error(),
				})
			}
			if service.IsError(err, service.ErrCodeDatabaseFailure) {
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
					"error": "Database query failed",
				})
			}
			// Default to internal server error for any other errors
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": err.Error(),
			})
		}

		// Return JSON response
		return c.JSON(results)
	}
}

// SubdistrictSearchHandler handles the subdistrict search endpoint
func (h *Handler) SubdistrictSearchHandler() fiber.Handler {
	return func(c *fiber.Ctx) error {
		// Extract and validate the q query parameter
		query := c.Query("q")
		if query == "" {
			slog.Warn("Subdistrict search query parameter missing", "ip", c.IP())
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "Query parameter 'q' is required",
			})
		}

		// Optional narrowing filters
		district := c.Query("district")
		city := c.Query("city")
		province := c.Query("province")

		// Use the service to perform the search
		opts, err := parseSearchOptions(c)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": err.Error(),
			})
		}

		results, err := h.svc.SearchBySubdistrict(query, district, city, province, opts)
		if err != nil {
			if service.IsError(err, service.ErrCodeInvalidInput) {
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
					"error": err.Error(),
				})
			}
			if service.IsError(err, service.ErrCodeDatabaseFailure) {
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
					"error": "Database query failed",
				})
			}
			// Default to internal server error for any other errors
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": err.Error(),
			})
		}

		// Return JSON response
		return c.JSON(results)
	}
}

// CitySearchHandler handles the city search endpoint
func (h *Handler) CitySearchHandler() fiber.Handler {
	return func(c *fiber.Ctx) error {
		// Extract and validate the q query parameter
		query := c.Query("q")
		if query == "" {
			slog.Warn("City search query parameter missing", "ip", c.IP())
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "Query parameter 'q' is required",
			})
		}

		opts, err := parseSearchOptions(c)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": err.Error(),
			})
		}

		// Use the service to perform the search
		results, err := h.svc.SearchByCity(query, opts)
		if err != nil {
			if service.IsError(err, service.ErrCodeInvalidInput) {
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
					"error": err.Error(),
				})
			}
			if service.IsError(err, service.ErrCodeDatabaseFailure) {
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
					"error": "Database query failed",
				})
			}
			// Default to internal server error for any other errors
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": err.Error(),
			})
		}

		// Return JSON response
		return c.JSON(results)
	}
}

// ProvinceSearchHandler handles the province search endpoint
func (h *Handler) ProvinceSearchHandler() fiber.Handler {
	return func(c *fiber.Ctx) error {
		// Extract and validate the q query parameter
		query := c.Query("q")
		if query == "" {
			slog.Warn("Province search query parameter missing", "ip", c.IP())
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "Query parameter 'q' is required",
			})
		}

		opts, err := parseSearchOptions(c)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": err.Error(),
			})
		}

		// Use the service to perform the search
		results, err := h.svc.SearchByProvince(query, opts)
		if err != nil {
			if service.IsError(err, service.ErrCodeInvalidInput) {
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
					"error": err.Error(),
				})
			}
			if service.IsError(err, service.ErrCodeDatabaseFailure) {
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
					"error": "Database query failed",
				})
			}
			// Default to internal server error for any other errors
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": err.Error(),
			})
		}

		// Return JSON response
		return c.JSON(results)
	}
}

// PostalCodeSearchHandler handles the postal code search endpoint
func (h *Handler) PostalCodeSearchHandler() fiber.Handler {
	return func(c *fiber.Ctx) error {
		// Extract and validate the postal code from path parameter
		postalCode := c.Params("postalCode")
		if postalCode == "" {
			slog.Warn("Postal code parameter missing", "ip", c.IP())
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "Postal code parameter is required",
			})
		}

		opts, err := parseSearchOptions(c)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": err.Error(),
			})
		}

		// Use the service to perform the search
		results, err := h.svc.SearchByPostalCode(postalCode, opts)
		if err != nil {
			if service.IsError(err, service.ErrCodeInvalidInput) {
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
					"error": err.Error(),
				})
			}
			if service.IsError(err, service.ErrCodeNotFound) {
				return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
					"error": err.Error(),
				})
			}
			if service.IsError(err, service.ErrCodeDatabaseFailure) {
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
					"error": "Database query failed",
				})
			}
			// Default to internal server error for any other errors
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": err.Error(),
			})
		}

		// Return JSON response
		return c.JSON(results)
	}
}

// Legacy handlers for backward compatibility
// These handlers maintain the original interface that accepts a database connection directly

// Region represents the JSON response structure for a region
type Region struct {
	ID          string `json:"id"`
	Subdistrict string `json:"subdistrict"`
	District    string `json:"district"`
	City        string `json:"city"`
	Province    string `json:"province"`
	PostalCode  string `json:"postal_code"`
	FullText    string `json:"full_text"`
}

// SearchHandlerLegacy handles the search endpoint (legacy)
func SearchHandlerLegacy(db *sql.DB) fiber.Handler {
	svc := service.New(db)
	handler := New(svc)
	return handler.SearchHandler()
}

// DistrictSearchHandlerLegacy handles the district search endpoint (legacy)
func DistrictSearchHandlerLegacy(db *sql.DB) fiber.Handler {
	svc := service.New(db)
	handler := New(svc)
	return handler.DistrictSearchHandler()
}

// SubdistrictSearchHandlerLegacy handles the subdistrict search endpoint (legacy)
func SubdistrictSearchHandlerLegacy(db *sql.DB) fiber.Handler {
	svc := service.New(db)
	handler := New(svc)
	return handler.SubdistrictSearchHandler()
}

// CitySearchHandlerLegacy handles the city search endpoint (legacy)
func CitySearchHandlerLegacy(db *sql.DB) fiber.Handler {
	svc := service.New(db)
	handler := New(svc)
	return handler.CitySearchHandler()
}

// ProvinceSearchHandlerLegacy handles the province search endpoint (legacy)
func ProvinceSearchHandlerLegacy(db *sql.DB) fiber.Handler {
	svc := service.New(db)
	handler := New(svc)
	return handler.ProvinceSearchHandler()
}

// PostalCodeSearchHandlerLegacy handles the postal code search endpoint (legacy)
func PostalCodeSearchHandlerLegacy(db *sql.DB) fiber.Handler {
	svc := service.New(db)
	handler := New(svc)
	return handler.PostalCodeSearchHandler()
}
