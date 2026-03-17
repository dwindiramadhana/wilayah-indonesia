package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/ilmimris/wilayah-indonesia/internal/config"
)

func main() {
	bootstrapLogger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	slog.SetDefault(bootstrapLogger)

	ctx := context.Background()

	// Database configuration
	dbType := os.Getenv("DB_TYPE")
	if dbType == "" {
		dbType = "duckdb" // Default to DuckDB for backwards compatibility
	}

	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		if dbType == "duckdb" {
			dbPath = "md:regions"
		}
	}

	opts := config.Options{
		DBType:      dbType,
		DBPath:      dbPath,
		DatabaseURL: os.Getenv("DATABASE_URL"),
		Port:        os.Getenv("PORT"),
		ReadOnly:    dbType != "postgres", // PostgreSQL is always read-write
	}

	bootstrap, err := config.BootstrapHTTP(ctx, opts)
	if err != nil {
		slog.Error("Failed to bootstrap HTTP application", "error", err)
		os.Exit(1)
	}

	defer func() {
		if bootstrap.DB != nil {
			bootstrap.DB.Close()
		}
	}()

	if bootstrap.Logger != nil {
		slog.SetDefault(bootstrap.Logger)
	}

	port := opts.Port
	if port == "" {
		port = "8080"
	}

	slog.Info("Server starting", "port", port, "db_type", dbType)
	if err := bootstrap.App.Listen(":" + port); err != nil {
		slog.Error("Failed to start server", "error", err)
		os.Exit(1)
	}
}
