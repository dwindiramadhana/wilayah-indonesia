package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Pool wraps pgxpool.Pool for easier mocking and testing.
type Pool struct {
	pool *pgxpool.Pool
}

// Rows wraps pgx.Rows for easier iteration.
type Rows struct {
	rows pgx.Rows
}

// NewPool creates a new PostgreSQL connection pool.
func NewPool(ctx context.Context, dsn string) (*Pool, error) {
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to parse database URL: %w", err)
	}

	// Configure connection pool
	config.MaxConns = 25
	config.MinConns = 5
	config.HealthCheckPeriod = config.HealthCheckPeriod

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create connection pool: %w", err)
	}

	// Verify connection
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return &Pool{pool: pool}, nil
}

// Query executes a query and returns rows.
func (p *Pool) Query(ctx context.Context, query string, args ...interface{}) (*Rows, error) {
	rows, err := p.pool.Query(ctx, query, convertArgs(args)...)
	if err != nil {
		return nil, err
	}
	return &Rows{rows: rows}, nil
}

// QueryRow executes a query that returns a single row.
func (p *Pool) QueryRow(ctx context.Context, query string, args ...interface{}) pgx.Row {
	return p.pool.QueryRow(ctx, query, convertArgs(args)...)
}

// Exec executes a query without returning rows.
func (p *Pool) Exec(ctx context.Context, query string, args ...interface{}) (int64, error) {
	result, err := p.pool.Exec(ctx, query, convertArgs(args)...)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}

// Close closes all connections in the pool.
func (p *Pool) Close() {
	p.pool.Close()
}

// Ping checks the connection to the database.
func (p *Pool) Ping(ctx context.Context) error {
	return p.pool.Ping(ctx)
}

// Next advances the rows iterator.
func (r *Rows) Next() bool {
	return r.rows.Next()
}

// Close closes the rows iterator.
func (r *Rows) Close() {
	r.rows.Close()
}

// Scan copies column values into dest.
func (r *Rows) Scan(dest ...interface{}) error {
	return r.rows.Scan(dest...)
}

// FieldDescriptions returns column metadata.
func (r *Rows) FieldDescriptions() []pgconn.FieldDescription {
	return r.rows.FieldDescriptions()
}

// Values returns the current row as a slice of values.
func (r *Rows) Values() ([]interface{}, error) {
	if !r.rows.Next() {
		return nil, r.rows.Err()
	}
	values, err := r.rows.Values()
	if err != nil {
		return nil, err
	}
	return values, nil
}

// Err returns any error that occurred during iteration.
func (r *Rows) Err() error {
	return r.rows.Err()
}

// convertArgs converts interface{} args to pgx compatible args.
// This is a no-op in most cases but provides a hook for type conversion if needed.
func convertArgs(args []interface{}) []interface{} {
	return args
}
