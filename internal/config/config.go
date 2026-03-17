package config

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/ilmimris/wilayah-indonesia/internal/delivery/http/middleware"
	regiondelivery "github.com/ilmimris/wilayah-indonesia/internal/delivery/http/region"
	"github.com/ilmimris/wilayah-indonesia/internal/delivery/http/router"
	workerdelivery "github.com/ilmimris/wilayah-indonesia/internal/delivery/worker/ingestor"
	"github.com/ilmimris/wilayah-indonesia/internal/gateway/filesystem"
	"github.com/ilmimris/wilayah-indonesia/internal/gateway/sqlnormalize"
	"github.com/ilmimris/wilayah-indonesia/internal/model"
	"github.com/ilmimris/wilayah-indonesia/internal/repository"
	"github.com/ilmimris/wilayah-indonesia/internal/repository/duckdb"
	"github.com/ilmimris/wilayah-indonesia/internal/repository/postgres"
	sharederrors "github.com/ilmimris/wilayah-indonesia/internal/shared/errors"
	ingestionusecase "github.com/ilmimris/wilayah-indonesia/internal/usecase/ingestion"
	regionusecase "github.com/ilmimris/wilayah-indonesia/internal/usecase/region"

	_ "github.com/duckdb/duckdb-go/v2"
)

// Options groups runtime configuration flags consumed by bootstrap routines.
type Options struct {
	DBType      string // "duckdb" or "postgres"
	DBPath      string // For DuckDB
	DatabaseURL string // For PostgreSQL
	Port        string
	Features    FeatureFlags
	Ingestion   IngestionPaths
	ReadOnly    bool
}

// FeatureFlags exposes optional toggles used across the application.
type FeatureFlags struct {
	EnableBPSSearch bool
	IncludeScores   bool
}

// IngestionPaths enumerates filesystem paths required for dataset refresh.
type IngestionPaths struct {
	WilayahSQL    string
	PostalSQL     string
	BPSMappingSQL string
}

// HTTPBootstrap bundles HTTP-specific components produced by BootstrapHTTP.
type HTTPBootstrap struct {
	App    *fiber.App
	DB     *sql.DB
	Logger *slog.Logger
}

// WorkerBootstrap bundles components needed for the ingestion worker.
type WorkerBootstrap struct {
	Logger  *slog.Logger
	DB      *sql.DB
	PgPool  *postgres.Pool
	Runner  *workerdelivery.Runner
	UseCase ingestionusecase.UseCase
}

// NewLogger constructs a slog.Logger placeholder for subsequent wiring phases.
func NewLogger() (*slog.Logger, error) {
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})), nil
}

// NewDuckDB creates a DuckDB connection; currently returns ErrNotImplemented.
func NewDuckDB(ctx context.Context, opts Options) (*sql.DB, error) {
	path := opts.DBPath
	if path == "" {
		path = "data/regions.duckdb"
	}
	isMotherDuck := isMotherDuckPath(path)
	if isMotherDuck {
		ensureMotherDuckToken(path)
	}
	connStr := path
	if opts.ReadOnly {
		connStr = path + "?access_mode=read_only"
	}
	conn, err := sql.Open("duckdb", connStr)
	if err != nil {
		return nil, sharederrors.Wrap(sharederrors.CodeDatabaseFailure, "failed to open database", err)
	}
	if isMotherDuck {
		if err := useMotherDuckDatabase(ctx, conn, path); err != nil {
			slog.Warn("Failed to select MotherDuck database", "db_path", path, "error", err)
		}
	}
	return conn, nil
}

// NewPostgresPool creates a PostgreSQL connection pool.
func NewPostgresPool(ctx context.Context, opts Options) (*postgres.Pool, error) {
	dsn := opts.DatabaseURL
	if dsn == "" {
		dsn = os.Getenv("DATABASE_URL")
	}
	if dsn == "" {
		return nil, sharederrors.Wrap(sharederrors.CodeConfiguration, "DATABASE_URL or DatabaseURL is required for PostgreSQL", nil)
	}

	pool, err := postgres.NewPool(ctx, dsn)
	if err != nil {
		return nil, sharederrors.Wrap(sharederrors.CodeDatabaseFailure, "failed to create PostgreSQL pool", err)
	}

	return pool, nil
}

func ensureMotherDuckToken(path string) {
	token, ok := os.LookupEnv("MOTHERDUCK_TOKEN")
	if !ok || token == "" {
		slog.Warn("MOTHERDUCK_TOKEN not set; MotherDuck connection may fail", "db_path", path)
		return
	}

	if _, exists := os.LookupEnv("motherduck_token"); exists {
		return
	}

	if err := os.Setenv("motherduck_token", token); err != nil {
		slog.Warn("Failed to propagate MotherDuck token for DuckDB driver", "error", err)
	}
}

func isMotherDuckPath(path string) bool {
	return strings.HasPrefix(path, "md:") || strings.HasPrefix(path, "motherduck:")
}

func useMotherDuckDatabase(ctx context.Context, db *sql.DB, path string) error {
	target := motherDuckDatabaseName(path)
	if target == "" || target == "main" {
		return nil
	}
	stmt := fmt.Sprintf("USE %s;", quoteIdentifier(target))
	if _, err := db.ExecContext(ctx, stmt); err != nil {
		return err
	}
	return nil
}

func normalizeMotherDuckPath(path string) string {
	if path == "" {
		return ""
	}
	if idx := strings.Index(path, "?"); idx != -1 {
		path = path[:idx]
	}
	switch {
	case strings.HasPrefix(path, "motherduck:"):
		path = strings.TrimPrefix(path, "motherduck:")
	case strings.HasPrefix(path, "md:"):
		path = strings.TrimPrefix(path, "md:")
	default:
		return ""
	}
	return path
}

func quoteIdentifier(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func motherDuckDatabaseName(path string) string {
	normalized := normalizeMotherDuckPath(path)
	if normalized == "" {
		return ""
	}
	if idx := strings.Index(normalized, "/"); idx != -1 {
		return normalized[:idx]
	}
	return normalized
}

// NewFiber returns a basic Fiber app without middleware, ready for configuration.
func NewFiber() (*fiber.App, error) {
	app := fiber.New()
	return app, nil
}

// BootstrapHTTP wires HTTP components; supports both DuckDB and PostgreSQL backends.
func BootstrapHTTP(ctx context.Context, opts Options) (HTTPBootstrap, error) {
	logger, err := NewLogger()
	if err != nil {
		return HTTPBootstrap{}, err
	}

	var db *sql.DB
	var pgPool *postgres.Pool

	// Default to DuckDB if not specified
	dbType := opts.DBType
	if dbType == "" {
		dbType = "duckdb"
	}

	switch dbType {
	case "postgres", "postgresql":
		pgPool, err = NewPostgresPool(ctx, opts)
		if err != nil {
			return HTTPBootstrap{}, err
		}
		// Wrap sql.DB for compatibility
		db = nil // PostgreSQL uses pgPool directly
	case "duckdb":
		fallthrough
	default:
		if !isMotherDuckPath(opts.DBPath) {
			opts.ReadOnly = true
		} else {
			opts.ReadOnly = false
		}
		db, err = NewDuckDB(ctx, opts)
		if err != nil {
			return HTTPBootstrap{}, err
		}
	}

	var repo repository.RegionRepository
	if pgPool != nil {
		repo = postgres.NewRegionRepository(pgPool)
	} else {
		repo = duckdb.NewRegionRepository(db)
	}

	uc, err := regionusecase.New(ctx, repo, regionusecase.RegionUseCaseOptions{Logger: logger})
	if err != nil {
		if db != nil {
			db.Close()
		}
		if pgPool != nil {
			pgPool.Close()
		}
		return HTTPBootstrap{}, err
	}

	app, err := NewFiber()
	if err != nil {
		if db != nil {
			db.Close()
		}
		if pgPool != nil {
			pgPool.Close()
		}
		return HTTPBootstrap{}, err
	}

	app.Use(middleware.RequestLogger())
	app.Use(recover.New())

	controller := regiondelivery.NewController(uc)
	apiGroup := app.Group("/v1")
	router.RegisterRegionRoutes(apiGroup, controller)

	app.Get("/healthz", func(c *fiber.Ctx) error {
		ctx, cancel := context.WithTimeout(c.Context(), 2*time.Second)
		defer cancel()
		var pingErr error
		if pgPool != nil {
			pingErr = pgPool.Ping(ctx)
		} else {
			pingErr = db.PingContext(ctx)
		}
		if pingErr != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(model.ErrorResponse{Error: "Database connection failed"})
		}
		return c.JSON(fiber.Map{"status": "ok", "message": "Service is healthy"})
	})

	return HTTPBootstrap{App: app, DB: db, Logger: logger}, nil
}

// BootstrapWorker wires dependencies for the ingestion worker binary.
func BootstrapWorker(ctx context.Context, opts Options) (WorkerBootstrap, error) {
	opts.ReadOnly = false
	logger, err := NewLogger()
	if err != nil {
		return WorkerBootstrap{}, err
	}

	// Default to DuckDB if not specified
	dbType := opts.DBType
	if dbType == "" {
		dbType = "duckdb"
	}

	var db *sql.DB
	var pgPool *postgres.Pool
	var adminRepo ingestionusecase.AdminRepository

	switch dbType {
	case "postgres", "postgresql":
		pgPool, err = NewPostgresPool(ctx, opts)
		if err != nil {
			return WorkerBootstrap{}, err
		}
		adminRepo = postgres.NewAdminRepository(pgPool)
	default:
		db, err = NewDuckDB(ctx, opts)
		if err != nil {
			return WorkerBootstrap{}, err
		}
		adminRepo = duckdb.NewAdminRepository(db)
	}

	loader := filesystem.FileLoader{}
	normalizer := sqlnormalize.MySQLStripper{}
	uc := ingestionusecase.New(loader, normalizer, adminRepo, ingestionusecase.Options{Logger: logger})
	runner := workerdelivery.NewRunner(uc)

	return WorkerBootstrap{
		Logger:  logger,
		DB:      db,
		PgPool:  pgPool,
		Runner:  runner,
		UseCase: uc,
	}, nil
}

// ResolveIngestionPaths populates default paths when not explicitly provided.
func ResolveIngestionPaths(base string, paths IngestionPaths) IngestionPaths {
	if base == "" {
		base = "data"
	}
	withDefault := func(value, name string) string {
		if value != "" {
			return value
		}
		return filepath.Join(base, name)
	}
	return IngestionPaths{
		WilayahSQL:    withDefault(paths.WilayahSQL, "wilayah.sql"),
		PostalSQL:     withDefault(paths.PostalSQL, "wilayah_kodepos.sql"),
		BPSMappingSQL: withDefault(paths.BPSMappingSQL, "bps_wilayah.sql"),
	}
}
