# Makefile for Indonesian Regions Fuzzy Search API

# Variables
BINARY=regions-api
MAIN_DIR=cmd/api
INGESTOR_DIR=cmd/ingestor
DATA_DIR=data
DB_FILE=$(DATA_DIR)/regions.duckdb
SQL_FILE=$(DATA_DIR)/wilayah.sql
KODEPOS_FILE=$(DATA_DIR)/wilayah_kodepos.sql
BPS_FILE=$(DATA_DIR)/bps_wilayah.sql
PERIODE?=latest

# PostgreSQL configuration
DB_TYPE?=postgres
DATABASE_URL?=postgres://postgres:postgres@localhost:5432/wilayah_indonesia?sslmode=disable
POSTGRES_IMAGE=pgvector/pgvector:pg16

# Default target
.PHONY: all
all: build

# Build the API binary
.PHONY: build
build:
	go build -o $(BINARY) ./$(MAIN_DIR)

# Run the API server
.PHONY: run
run:
	go run ./$(MAIN_DIR)

# Run the data ingestor
.PHONY: ingest
ingest:
	go run ./$(INGESTOR_DIR)
# Download the administrative data SQL file
.PHONY: download-data
download-data: download-admin-data download-kodepos-data fetch-bps

# Download the administrative data SQL file
.PHONY: download-admin-data
download-admin-data:
	curl -o $(SQL_FILE) https://raw.githubusercontent.com/cahyadsn/wilayah/master/db/wilayah.sql

# Download the postal code data SQL file
.PHONY: download-kodepos-data
download-kodepos-data:
	curl -o $(KODEPOS_FILE) https://raw.githubusercontent.com/cahyadsn/wilayah_kodepos/refs/heads/main/db/wilayah_kodepos.sql

# Prepare the database (download data and run ingestor)
.PHONY: prepare-db
prepare-db: download-data ingest


# Fetch wilayah data from BPS API and render SQL dump
.PHONY: fetch-bps
fetch-bps:
		curl -o $(BPS_FILE) https://raw.githubusercontent.com/ilmimris/wilayah-indonesia-bps/refs/heads/main/data/sql/bps_wilayah_2024_1.2025.sql

# Run tests
.PHONY: test
test:
	go test -v ./...

# Run integration tests (requires PostgreSQL to be running)
.PHONY: test-integration
test-integration:
	@echo "Running integration tests..."
	@go test -v -tags=integration ./internal/repository/postgres/...
# Clean build artifacts
.PHONY: clean
clean:
	rm -f $(BINARY)
	rm -f $(DB_FILE)
	rm -f $(SQL_FILE)
	rm -f $(KODEPOS_FILE)


# Install dependencies
.PHONY: deps
deps:
	go mod tidy

# ============================================
# PostgreSQL Targets
# ============================================

# Start PostgreSQL container
.PHONY: postgres-up
postgres-up:
	docker compose up -d postgres
	@echo "Waiting for PostgreSQL to be ready..."
	@sleep 3
	@until docker exec wilayah-indonesia-db pg_isready -U postgres > /dev/null 2>&1; do \
		echo "PostgreSQL is starting..."; \
		sleep 1; \
	done
	@echo "PostgreSQL is ready!"

# Stop PostgreSQL container
.PHONY: postgres-down
postgres-down:
	docker compose down postgres

# Restart PostgreSQL container
.PHONY: postgres-restart
postgres-restart: postgres-down postgres-up

# Run migrations
.PHONY: migrate
migrate:
	@echo "Running database migrations..."
	@for f in migrations/*.sql; do \
		echo "Executing $$f"; \
		docker exec -i wilayah-indonesia-db psql -U postgres -d wilayah_indonesia -f "/docker-entrypoint-initdb.d/$$(basename $$f)" 2>/dev/null || \
		docker exec -i wilayah-indonesia-db psql -U postgres -d wilayah_indonesia < "$$f"; \
	done
	@echo "Migrations complete!"

# Reset database (drop and recreate)
.PHONY: postgres-reset
postgres-reset:
	docker compose down -v postgres
	@echo "Database volume removed. Run 'make postgres-up' to recreate."

# Seed database with initial data
.PHONY: seed
seed:
	@echo "Seeding database..."
	@DB_TYPE=postgres DATABASE_URL=$(DATABASE_URL) go run ./$(INGESTOR_DIR)

# Prepare PostgreSQL database (migrate + seed)
.PHONY: prepare-postgres
prepare-postgres: postgres-up migrate seed

# ============================================
# DuckDB Targets (Legacy)
# ============================================

# Build Docker image
.PHONY: docker-build
docker-build:
	docker compose build api

# Run Docker container
.PHONY: docker-run
docker-run:
	docker compose up api

# Run all services (API + PostgreSQL)
.PHONY: docker-compose-up
docker-compose-up:
	docker compose up -d

# Stop all services
.PHONY: docker-compose-down
docker-compose-down:
	docker compose down

# ============================================
# Dual Backend Testing (DuckDB + PostgreSQL)
# ============================================

# Start both backends simultaneously for comparison testing
# DuckDB API runs on port 8081, PostgreSQL API runs on port 8001
.PHONY: dual-up
dual-up:
	@echo "Starting PostgreSQL database..."
	@docker compose -f docker-compose.dual.yml up -d postgres
	@echo "Waiting for PostgreSQL to be ready..."
	@sleep 5
	@echo "Starting PostgreSQL API on port 8001..."
	@docker compose -f docker-compose.dual.yml up -d api-postgres
	@echo "Starting DuckDB API on port 8081..."
	@docker compose -f docker-compose.dual.yml up -d api-duckdb
	@sleep 3
	@echo ""
	@echo "Services started:"
	@echo "  DuckDB API:     http://localhost:8081"
	@echo "  PostgreSQL API: http://localhost:8001"
	@echo ""
	@echo "Run 'make seed-duckdb' to seed the DuckDB database"

# Stop both backends
.PHONY: dual-down
dual-down:
	docker compose -f docker-compose.dual.yml down

# Restart both backends
.PHONY: dual-restart
dual-restart: dual-down dual-up

# Seed DuckDB database (run after dual-up)
.PHONY: seed-duckdb
seed-duckdb:
	@echo "Seeding DuckDB database..."
	@docker exec wilayah-indonesia-api-duckdb /app/regions-ingestor
	@echo "DuckDB seeding complete!"

# Compare results between backends
.PHONY: compare
compare:
	@echo "Running backend comparison..."
	@go run ./scripts/compare-backends/main.go $(QUERY)

# Full dual setup with seeding and comparison
.PHONY: dual-test
dual-test:
	@echo "=== Starting Dual Backend Test ==="
	@$(MAKE) dual-up
	@sleep 2
	@$(MAKE) seed-duckdb
	@echo ""
	@echo "=== Running Comparison ==="
	go run ./scripts/compare-backends/main.go $(or $(QUERY),bandung)

# Help
.PHONY: help
help:
	@echo "Available targets:"
	@echo ""
	@echo "  General:"
	@echo "  all          - Build the API binary (default)"
	@echo "  build        - Build the API binary"
	@echo "  run          - Run the API server"
	@echo "  test         - Run unit tests"
	@echo "  test-integration - Run PostgreSQL integration tests (requires PostgreSQL)"
	@echo "  clean        - Clean build artifacts and data files"
	@echo "  deps         - Install dependencies"
	@echo ""
	@echo "  DuckDB (Legacy):"
	@echo "  ingest       - Run the data ingestor"
	@echo "  download-data - Download all data files"
	@echo "  download-admin-data - Download administrative data file"
	@echo "  download-kodepos-data - Download postal code data file"
	@echo "  fetch-bps    - Crawl BPS API and emit SQL dump (PERIODE=$(PERIODE))"
	@echo "  prepare-db   - Download data and run ingestor"
	@echo ""
	@echo "  PostgreSQL:"
	@echo "  postgres-up       - Start PostgreSQL container"
	@echo "  postgres-down     - Stop PostgreSQL container"
	@echo "  postgres-restart  - Restart PostgreSQL container"
	@echo "  migrate           - Run database migrations"
	@echo "  seed              - Seed database with initial data"
	@echo "  prepare-postgres  - Start PostgreSQL, run migrations, and seed"
	@echo "  postgres-reset    - Drop and recreate PostgreSQL volume"
	@echo ""
	@echo "  Docker Compose:"
	@echo "  docker-build        - Build Docker image"
	@echo "  docker-run          - Run API container"
	@echo "  docker-compose-up   - Start all services (API + PostgreSQL)"
	@echo "  docker-compose-down - Stop all services"
	@echo ""
	@echo "  Dual Backend Testing:"
	@echo "  dual-up        - Start both DuckDB (8080) and PostgreSQL (8000) APIs"
	@echo "  dual-down      - Stop both backends"
	@echo "  dual-restart   - Restart both backends"
	@echo "  seed-duckdb    - Seed DuckDB database (after dual-up)"
	@echo "  compare        - Run comparison script (use QUERY=xxx to specify query)"
	@echo "  dual-test      - Full setup: start both, seed DuckDB, run comparison"
