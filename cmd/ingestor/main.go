package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/ilmimris/wilayah-indonesia/internal/config"
	"github.com/ilmimris/wilayah-indonesia/internal/gateway/filesystem"
	"github.com/ilmimris/wilayah-indonesia/internal/gateway/sqlnormalize"
	"github.com/ilmimris/wilayah-indonesia/internal/repository/postgres"
	ingestionusecase "github.com/ilmimris/wilayah-indonesia/internal/usecase/ingestion"
)

func main() {
	bootstrapLogger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	slog.SetDefault(bootstrapLogger)

	ctx := context.Background()

	// Command-line flags
	dbType := flag.String("type", "", "Database type: duckdb or postgres (default: from DB_TYPE env)")
	flag.Parse()

	dataDir := os.Getenv("DATA_DIR")
	paths := config.ResolveIngestionPaths(dataDir, config.IngestionPaths{})

	// Determine database type
	if *dbType == "" {
		*dbType = os.Getenv("DB_TYPE")
	}
	if *dbType == "" {
		*dbType = "duckdb" // Default to DuckDB for backwards compatibility
	}

	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		if *dbType == "duckdb" {
			dbPath = "md:regions"
		}
	}

	opts := config.Options{
		DBType:      *dbType,
		DBPath:      dbPath,
		DatabaseURL: os.Getenv("DATABASE_URL"),
		Ingestion:   paths,
		ReadOnly:    false,
	}

	bootstrap, err := config.BootstrapWorker(ctx, opts)
	if err != nil {
		slog.Error("Failed to bootstrap ingestion worker", "error", err)
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	defer func() {
		if bootstrap.DB != nil {
			bootstrap.DB.Close()
		}
		if bootstrap.PgPool != nil {
			bootstrap.PgPool.Close()
		}
	}()

	refreshOpts := ingestionusecase.RefreshOptions{
		WilayahSQLPath:    paths.WilayahSQL,
		PostalSQLPath:     paths.PostalSQL,
		BPSMappingSQLPath: paths.BPSMappingSQL,
	}

	slog.Info("Running ingestion workflow", "db_type", *dbType)

	// For PostgreSQL, use the PostgreSQL-specific ingestion usecase
	if *dbType == "postgres" || *dbType == "postgresql" {
		if bootstrap.PgPool == nil {
			slog.Error("PostgreSQL pool is nil")
			os.Exit(1)
		}
		loader := filesystem.FileLoader{}
		normalizer := sqlnormalize.MySQLStripper{}
		adminRepo := postgres.NewAdminRepository(bootstrap.PgPool)
		pgUc := postgres.NewIngestionUseCase(loader, normalizer, adminRepo, bootstrap.Logger)
		if err := pgUc.Refresh(ctx, refreshOpts); err != nil {
			slog.Error("PostgreSQL ingestion failed", "error", err)
			fmt.Fprintf(os.Stderr, "Ingestion failed: %v\n", err)
			os.Exit(1)
		}
	} else {
		// Use DuckDB ingestion
		if err := bootstrap.Runner.Run(ctx, refreshOpts); err != nil {
			slog.Error("Ingestion failed", "error", err)
			fmt.Fprintf(os.Stderr, "Ingestion failed: %v\n", err)
			os.Exit(1)
		}
	}

	slog.Info("Ingestion completed successfully")
}
