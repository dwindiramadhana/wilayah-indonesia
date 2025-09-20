package ingestor

import (
	"context"

	ingestionusecase "github.com/ilmimris/wilayah-indonesia/internal/usecase/ingestion"
)

// Runner bridges command-line entrypoints with the ingestion use case.
type Runner struct {
	uc ingestionusecase.UseCase
}

// NewRunner creates a new Runner instance.
func NewRunner(uc ingestionusecase.UseCase) *Runner {
	return &Runner{uc: uc}
}

// Run triggers the refresh workflow.
func (r *Runner) Run(ctx context.Context, opts ingestionusecase.RefreshOptions) error {
	return r.uc.Refresh(ctx, opts)
}
