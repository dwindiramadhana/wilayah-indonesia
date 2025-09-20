package region

import (
	"log/slog"
	"runtime/debug"
	"strconv"

	"github.com/gofiber/fiber/v2"
	"github.com/ilmimris/wilayah-indonesia/internal/model"
	sharederrors "github.com/ilmimris/wilayah-indonesia/internal/shared/errors"
	regionusecase "github.com/ilmimris/wilayah-indonesia/internal/usecase/region"
)

// Controller wires HTTP handlers to the region use case.
type Controller struct {
	uc regionusecase.RegionUseCase
}

// NewController creates a region controller.
func NewController(uc regionusecase.RegionUseCase) *Controller {
	return &Controller{uc: uc}
}

// Register binds all region routes under the provided router.
func (c *Controller) Register(router fiber.Router) {
	router.Get("/search", c.handleSearch)
	router.Get("/search/district", c.handleSearchByDistrict)
	router.Get("/search/subdistrict", c.handleSearchBySubdistrict)
	router.Get("/search/city", c.handleSearchByCity)
	router.Get("/search/province", c.handleSearchByProvince)
	router.Get("/search/postal/:postalCode", c.handleSearchByPostalCode)
}

func (c *Controller) handleSearch(ctx *fiber.Ctx) error {
	options, err := parseSearchOptions(ctx)
	if err != nil {
		return errorResponse(ctx, fiber.StatusBadRequest, err)
	}
	request := model.SearchRequest{
		Query:       ctx.Query("q"),
		Subdistrict: ctx.Query("subdistrict"),
		District:    ctx.Query("district"),
		City:        ctx.Query("city"),
		Province:    ctx.Query("province"),
		Options:     options,
	}
	results, err := c.uc.Search(ctx.Context(), request)
	if err != nil {
		return mapUseCaseError(ctx, err)
	}
	return ctx.JSON(results)
}

func (c *Controller) handleSearchByDistrict(ctx *fiber.Ctx) error {
	options, err := parseSearchOptions(ctx)
	if err != nil {
		return errorResponse(ctx, fiber.StatusBadRequest, err)
	}
	results, err := c.uc.SearchByDistrict(ctx.Context(), ctx.Query("q"), ctx.Query("city"), ctx.Query("province"), options)
	if err != nil {
		return mapUseCaseError(ctx, err)
	}
	return ctx.JSON(results)
}

func (c *Controller) handleSearchBySubdistrict(ctx *fiber.Ctx) error {
	options, err := parseSearchOptions(ctx)
	if err != nil {
		return errorResponse(ctx, fiber.StatusBadRequest, err)
	}
	results, err := c.uc.SearchBySubdistrict(ctx.Context(), ctx.Query("q"), ctx.Query("district"), ctx.Query("city"), ctx.Query("province"), options)
	if err != nil {
		return mapUseCaseError(ctx, err)
	}
	return ctx.JSON(results)
}

func (c *Controller) handleSearchByCity(ctx *fiber.Ctx) error {
	options, err := parseSearchOptions(ctx)
	if err != nil {
		return errorResponse(ctx, fiber.StatusBadRequest, err)
	}
	results, err := c.uc.SearchByCity(ctx.Context(), ctx.Query("q"), options)
	if err != nil {
		return mapUseCaseError(ctx, err)
	}
	return ctx.JSON(results)
}

func (c *Controller) handleSearchByProvince(ctx *fiber.Ctx) error {
	options, err := parseSearchOptions(ctx)
	if err != nil {
		return errorResponse(ctx, fiber.StatusBadRequest, err)
	}
	results, err := c.uc.SearchByProvince(ctx.Context(), ctx.Query("q"), options)
	if err != nil {
		return mapUseCaseError(ctx, err)
	}
	return ctx.JSON(results)
}

func (c *Controller) handleSearchByPostalCode(ctx *fiber.Ctx) error {
	options, err := parseSearchOptions(ctx)
	if err != nil {
		return errorResponse(ctx, fiber.StatusBadRequest, err)
	}
	results, err := c.uc.SearchByPostalCode(ctx.Context(), ctx.Params("postalCode"), options)
	if err != nil {
		return mapUseCaseError(ctx, err)
	}
	return ctx.JSON(results)
}

func parseSearchOptions(ctx *fiber.Ctx) (model.SearchOptions, error) {
	var opts model.SearchOptions
	if limitRaw := ctx.Query("limit"); limitRaw != "" {
		limit, err := strconv.Atoi(limitRaw)
		if err != nil {
			return opts, fiber.NewError(fiber.StatusBadRequest, "invalid integer value for 'limit'")
		}
		opts.Limit = limit
	}

	if parsed, err := parseBoolQuery(ctx, "search_bps"); err != nil {
		return opts, err
	} else {
		opts.SearchBPS = parsed
	}
	if parsed, err := parseBoolQuery(ctx, "include_bps"); err != nil {
		return opts, err
	} else {
		opts.IncludeBPS = parsed
	}
	if parsed, err := parseBoolQuery(ctx, "include_scores"); err != nil {
		return opts, err
	} else {
		opts.IncludeScores = parsed
	}

	return opts, nil
}

func parseBoolQuery(ctx *fiber.Ctx, key string) (bool, error) {
	value := ctx.Query(key)
	if value == "" {
		return false, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fiber.NewError(fiber.StatusBadRequest, "invalid boolean value for '"+key+"'")
	}
	return parsed, nil
}

func mapUseCaseError(ctx *fiber.Ctx, err error) error {
	switch {
	case sharederrors.Is(err, sharederrors.CodeInvalidInput):
		return errorResponse(ctx, fiber.StatusBadRequest, err)
	case sharederrors.Is(err, sharederrors.CodeDatabaseFailure):
		logInternalError("database failure", err)
		return errorResponse(ctx, fiber.StatusInternalServerError, fiber.NewError(fiber.StatusInternalServerError, "Database query failed"))
	default:
		logInternalError("internal server error", err)
		return errorResponse(ctx, fiber.StatusInternalServerError, err)
	}
}

func errorResponse(ctx *fiber.Ctx, status int, err error) error {
	return ctx.Status(status).JSON(model.ErrorResponse{Error: err.Error()})
}

func logInternalError(message string, err error) {
	slog.Error(message, "error", err, "stack", string(debug.Stack()))
}
