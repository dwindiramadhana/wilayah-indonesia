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
	opts := config.Options{
		DBPath: os.Getenv("DB_PATH"),
		Port:   os.Getenv("PORT"),
		ReadOnly: true,
	}

	bootstrap, err := config.BootstrapHTTP(ctx, opts)
	if err != nil {
		slog.Error("Failed to bootstrap HTTP application", "error", err)
		os.Exit(1)
	}
	defer bootstrap.DB.Close()

	if bootstrap.Logger != nil {
		slog.SetDefault(bootstrap.Logger)
	}

	port := opts.Port
	if port == "" {
		port = "8080"
	}

	slog.Info("Server starting", "port", port)
	if err := bootstrap.App.Listen(":" + port); err != nil {
		slog.Error("Failed to start server", "error", err)
		os.Exit(1)
	}
}
