package shared

import (
	"github.com/ilmimris/wilayah-indonesia/internal/model"
	sharederrors "github.com/ilmimris/wilayah-indonesia/internal/shared/errors"
)

const (
	defaultSearchLimit = 10
	maxSearchLimit     = 100
)

// OptionNormalizer enforces limits and defaults on search options.
type OptionNormalizer interface {
	Normalize(opts *model.SearchOptions) error
}

// LimitNormalizer applies upper/lower bounds to SearchOptions.
type LimitNormalizer struct {
	Default int
	Max     int
}

// Normalize sets defaults and validates the provided search options.
func (n LimitNormalizer) Normalize(opts *model.SearchOptions) error {
	defaultLimit := n.Default
	if defaultLimit == 0 {
		defaultLimit = defaultSearchLimit
	}
	maxLimit := n.Max
	if maxLimit == 0 {
		maxLimit = maxSearchLimit
	}

	if opts.Limit == 0 {
		opts.Limit = defaultLimit
	}
	if opts.Limit < 0 {
		return sharederrors.New(sharederrors.CodeInvalidInput, "limit must be a positive integer")
	}
	if opts.Limit > maxLimit {
		opts.Limit = maxLimit
	}
	return nil
}
