package postgres

import (
	"context"
	sharederrors "github.com/ilmimris/wilayah-indonesia/internal/shared/errors"
)

// AdminRepository executes administrative SQL statements against PostgreSQL.
type AdminRepository struct {
	pool *Pool
}

// NewAdminRepository constructs an AdminRepository for the given connection pool.
func NewAdminRepository(pool *Pool) *AdminRepository {
	return &AdminRepository{pool: pool}
}

// Exec runs the provided statement within the supplied context.
func (r *AdminRepository) Exec(ctx context.Context, sql string) error {
	_, err := r.pool.Exec(ctx, sql)
	if err != nil {
		return sharederrors.Wrap(sharederrors.CodeDatabaseFailure, "failed to execute SQL", err)
	}
	return nil
}
