package region

import (
	"context"
	"log/slog"
	"strings"

	"github.com/ilmimris/wilayah-indonesia/internal/entity"
	"github.com/ilmimris/wilayah-indonesia/internal/model"
	repository "github.com/ilmimris/wilayah-indonesia/internal/repository"
	sharederrors "github.com/ilmimris/wilayah-indonesia/internal/shared/errors"
	"github.com/ilmimris/wilayah-indonesia/internal/usecase/shared"
)

// RegionUseCase exposes search operations across administrative regions.
type RegionUseCase interface {
	Search(ctx context.Context, req model.SearchRequest) ([]model.RegionResponse, error)
	SearchByDistrict(ctx context.Context, district, city, province string, opts model.SearchOptions) ([]model.RegionResponse, error)
	SearchBySubdistrict(ctx context.Context, subdistrict, district, city, province string, opts model.SearchOptions) ([]model.RegionResponse, error)
	SearchByCity(ctx context.Context, city string, opts model.SearchOptions) ([]model.RegionResponse, error)
	SearchByProvince(ctx context.Context, province string, opts model.SearchOptions) ([]model.RegionResponse, error)
	SearchByPostalCode(ctx context.Context, postalCode string, opts model.SearchOptions) ([]model.RegionResponse, error)
}

// RegionUseCaseOptions configures behaviour of the use case implementation.
type RegionUseCaseOptions struct {
	Logger       *slog.Logger
	DefaultLimit int
	MaxLimit     int
}

type regionUseCase struct {
	repo         repository.RegionRepository
	normalizer   shared.OptionNormalizer
	logger       *slog.Logger
	capabilities repository.RegionRepositoryCapabilities
}

// New creates a RegionUseCase backed by the provided repository.
func New(ctx context.Context, repo repository.RegionRepository, opts RegionUseCaseOptions) (RegionUseCase, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	caps, err := repo.Capabilities(ctx)
	if err != nil {
		return nil, err
	}

	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}

	return &regionUseCase{
		repo:         repo,
		normalizer:   shared.LimitNormalizer{Default: opts.DefaultLimit, Max: opts.MaxLimit},
		logger:       logger,
		capabilities: caps,
	}, nil
}

func (uc *regionUseCase) Search(ctx context.Context, req model.SearchRequest) ([]model.RegionResponse, error) {
	sanitizedQuery := sanitizeFTSQuery(req.Query)
	if sanitizedQuery == "" && req.Subdistrict == "" && req.District == "" && req.City == "" && req.Province == "" {
		return nil, sharederrors.New(sharederrors.CodeInvalidInput, "at least one search parameter is required")
	}

	if err := uc.normalizer.Normalize(&req.Options); err != nil {
		return nil, err
	}

	if err := uc.validateDatasetOptions(req.Options); err != nil {
		return nil, err
	}

	params := repository.RegionSearchParams{
		Query:       sanitizedQuery,
		Subdistrict: req.Subdistrict,
		District:    req.District,
		City:        req.City,
		Province:    req.Province,
		Options: repository.RegionSearchOptions{
			Limit:         req.Options.Limit,
			SearchBPS:     req.Options.SearchBPS,
			IncludeBPS:    req.Options.IncludeBPS,
			IncludeScores: req.Options.IncludeScores,
		},
	}

	results, err := uc.repo.Search(ctx, params)
	if err != nil {
		return nil, err
	}

	return mapToResponses(results), nil
}

func (uc *regionUseCase) SearchByDistrict(ctx context.Context, district, city, province string, opts model.SearchOptions) ([]model.RegionResponse, error) {
	if district == "" {
		return nil, sharederrors.New(sharederrors.CodeInvalidInput, "query parameter is required")
	}
	return uc.Search(ctx, model.SearchRequest{District: district, City: city, Province: province, Options: opts})
}

func (uc *regionUseCase) SearchBySubdistrict(ctx context.Context, subdistrict, district, city, province string, opts model.SearchOptions) ([]model.RegionResponse, error) {
	if subdistrict == "" {
		return nil, sharederrors.New(sharederrors.CodeInvalidInput, "query parameter is required")
	}
	return uc.Search(ctx, model.SearchRequest{Subdistrict: subdistrict, District: district, City: city, Province: province, Options: opts})
}

func (uc *regionUseCase) SearchByCity(ctx context.Context, city string, opts model.SearchOptions) ([]model.RegionResponse, error) {
	if city == "" {
		return nil, sharederrors.New(sharederrors.CodeInvalidInput, "query parameter is required")
	}
	return uc.Search(ctx, model.SearchRequest{City: city, Options: opts})
}

func (uc *regionUseCase) SearchByProvince(ctx context.Context, province string, opts model.SearchOptions) ([]model.RegionResponse, error) {
	if province == "" {
		return nil, sharederrors.New(sharederrors.CodeInvalidInput, "query parameter is required")
	}
	return uc.Search(ctx, model.SearchRequest{Province: province, Options: opts})
}

func (uc *regionUseCase) SearchByPostalCode(ctx context.Context, postalCode string, opts model.SearchOptions) ([]model.RegionResponse, error) {
	if postalCode == "" {
		return nil, sharederrors.New(sharederrors.CodeInvalidInput, "postal code parameter is required")
	}

	if err := uc.normalizer.Normalize(&opts); err != nil {
		return nil, err
	}
	if opts.IncludeBPS && !uc.capabilities.HasBPSColumns {
		return nil, sharederrors.New(sharederrors.CodeInvalidInput, "BPS metadata requested but dataset is missing BPS columns; run 'make prepare-db'")
	}

	results, err := uc.repo.SearchByPostalCode(ctx, postalCode, repository.RegionSearchOptions{
		Limit:         opts.Limit,
		SearchBPS:     opts.SearchBPS,
		IncludeBPS:    opts.IncludeBPS,
		IncludeScores: opts.IncludeScores,
	})
	if err != nil {
		return nil, err
	}

	return mapToResponses(results), nil
}

func (uc *regionUseCase) validateDatasetOptions(opts model.SearchOptions) error {
	if opts.SearchBPS && !uc.capabilities.HasBPSColumns {
		return sharederrors.New(sharederrors.CodeInvalidInput, "BPS search requested but dataset is missing BPS columns; run 'make prepare-db'")
	}
	if opts.IncludeBPS && !uc.capabilities.HasBPSColumns {
		return sharederrors.New(sharederrors.CodeInvalidInput, "BPS metadata requested but dataset is missing BPS columns; run 'make prepare-db'")
	}
	if opts.SearchBPS && !uc.capabilities.HasBPSIndex {
		return sharederrors.New(sharederrors.CodeInvalidInput, "BPS search requested but dataset is missing BPS FTS index; run 'make prepare-db'")
	}
	return nil
}

func mapToResponses(items []entity.RegionWithScore) []model.RegionResponse {
	responses := make([]model.RegionResponse, 0, len(items))
	for _, item := range items {
		resp := model.RegionResponse{
			ID:          item.Region.ID,
			Subdistrict: item.Region.Subdistrict,
			District:    item.Region.District,
			City:        item.Region.City,
			Province:    item.Region.Province,
			PostalCode:  item.Region.PostalCode,
			FullText:    item.Region.FullText,
			BPS:         item.Region.BPS,
		}
		if item.Score != nil {
			resp.Scores = item.Score
		}
		responses = append(responses, resp)
	}
	return responses
}

func sanitizeFTSQuery(q string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case '\'', '"':
			return -1
		default:
			return r
		}
	}, q)
}
