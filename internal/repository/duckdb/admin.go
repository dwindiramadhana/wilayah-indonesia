package duckdb

import (
	"context"
	"database/sql"

	sharederrors "github.com/ilmimris/wilayah-indonesia/internal/shared/errors"
)

// AdminRepository executes administrative SQL statements against DuckDB.
type AdminRepository struct {
	db *sql.DB
}

// NewAdminRepository constructs an AdminRepository for the given connection.
func NewAdminRepository(db *sql.DB) *AdminRepository {
	return &AdminRepository{db: db}
}

// Exec runs the provided statement within the supplied context.
func (r *AdminRepository) Exec(ctx context.Context, sql string) error {
	if _, err := r.db.ExecContext(ctx, sql); err != nil {
		return sharederrors.Wrap(sharederrors.CodeDatabaseFailure, "failed to execute SQL", err)
	}
	return nil
}
