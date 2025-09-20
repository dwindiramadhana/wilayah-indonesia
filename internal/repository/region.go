package repository

import (
	"context"

	"github.com/ilmimris/wilayah-indonesia/internal/entity"
)

// RegionSearchParams describes filters and options used across region queries.
type RegionSearchParams struct {
	Query       string
	Subdistrict string
	District    string
	City        string
	Province    string
	Options     RegionSearchOptions
}

// RegionSearchOptions tune repository lookups and enrichment needs.
type RegionSearchOptions struct {
	Limit         int
	SearchBPS     bool
	IncludeBPS    bool
	IncludeScores bool
}

// RegionRepository exposes read operations for region data.
type RegionRepository interface {
	Search(ctx context.Context, params RegionSearchParams) ([]entity.RegionWithScore, error)
	SearchByDistrict(ctx context.Context, params RegionSearchParams) ([]entity.RegionWithScore, error)
	SearchBySubdistrict(ctx context.Context, params RegionSearchParams) ([]entity.RegionWithScore, error)
	SearchByCity(ctx context.Context, params RegionSearchParams) ([]entity.RegionWithScore, error)
	SearchByProvince(ctx context.Context, params RegionSearchParams) ([]entity.RegionWithScore, error)
	SearchByPostalCode(ctx context.Context, postalCode string, opts RegionSearchOptions) ([]entity.RegionWithScore, error)
	Capabilities(ctx context.Context) (RegionRepositoryCapabilities, error)
}

// RegionRepositoryCapabilities describes optional features surfaced by the data store.
type RegionRepositoryCapabilities struct {
	HasBPSColumns bool
	HasBPSIndex   bool
}
