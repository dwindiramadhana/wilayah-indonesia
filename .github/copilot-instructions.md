# Copilot Instructions for wilayah-api

## Architecture Overview

This is a Clean Architecture Go API for fuzzy searching Indonesian administrative regions. The codebase is actively migrating from a monolithic service layer toward explicit delivery, use case, repository, and gateway layers.

**Dual-Backend Design**: The API supports both **DuckDB** (lightweight, embedded, default) and **PostgreSQL** (production FTS with pgvector). Backend selection is runtime via `DB_TYPE` env var; DuckDB uses in-memory `md:regions` by default, PostgreSQL requires a live `DATABASE_URL`.

**Current State**: Core use cases (`internal/usecase/region`, `internal/usecase/ingestion`), repositories (`internal/repository/{duckdb,postgres}`), and HTTP delivery (`internal/delivery/http`) are implemented. Entity models live in `internal/entity`, transport models in `internal/model`, and shared error taxonomy in `internal/shared/errors`.

## Project Structure & Key Directories

- **`cmd/api`** – HTTP server entrypoint; delegates all wiring to `internal/config.BootstrapHTTP`.
- **`cmd/ingestor`** – Data refresh CLI; loads SQL dumps and rebuilds DuckDB FTS indexes.
- **`internal/config`** – Bootstrapper functions (`BootstrapHTTP`, `BootstrapWorker`) that construct logger, database connections, Fiber app, repositories, and use cases from environment options.
- **`internal/usecase/{region,ingestion}`** – Business logic orchestrating repositories and gateways; region searches return `[]entity.RegionWithScore`, ingestion refreshes data and indexes.
- **`internal/repository/{region.go,duckdb/,postgres/}`** – Data-access abstractions; both backends implement `RegionRepository` interface with search methods accepting `RegionSearchParams`.
- **`internal/delivery/http/{router,region/,middleware}`** – Fiber controllers that parse query params into `model.SearchRequest` and call use cases; routes registered centrally in `router.Register`.
- **`internal/delivery/worker/ingestor`** – CLI runner for the ingestion use case; maps flags to structured options.
- **`internal/entity`** – Persistence models (`Region`, `RegionWithScore`, `RegionBPS`) mirroring DuckDB/PostgreSQL columns.
- **`internal/model`** – Transport contracts (`SearchRequest`, `RegionResponse`, `SearchOptions`) and error envelopes.
- **`internal/gateway/{filesystem,sqlnormalize}`** – Outbound adapters; filesystem loader for SQL dumps, SQL normalizer for MySQL→DuckDB compatibility.
- **`data/`** – SQL dumps downloaded during `make prepare-db` (wilayah.sql, wilayah_kodepos.sql, bps_wilayah.sql).

## Critical Workflows

### Local Development: Running the API

```bash
# Build the binary
make build

# Run with DuckDB (default, in-memory "md:regions")
make run

# Or with PostgreSQL (requires DATABASE_URL set)
DB_TYPE=postgres DATABASE_URL="postgres://..." go run ./cmd/api

# Prepare DuckDB with downloaded datasets
make download-data    # fetch SQL files
make prepare-db       # ingest data into DuckDB

# Test the API
go test -v ./...
```

### Database Refresh Workflow

```bash
# Download BPS data (latest period)
make fetch-bps

# Load all data into DuckDB and rebuild indexes
make ingest

# Or manually:
go run ./cmd/ingestor
```

**Implementation Detail**: The ingestor uses `internal/gateway/filesystem.Loader` to read SQL files, `internal/gateway/sqlnormalize.Normalizer` to remove MySQL-specific syntax, and the DuckDB repository's admin methods to execute schema setup and FTS index creation.

### Testing

```bash
# Unit + integration tests
make test              # or: go test -v ./...

# Integration tests only (requires PostgreSQL)
make test-integration  # go test -v -tags=integration ./internal/repository/postgres/...
```

Tests follow a pattern: define request/params, invoke use case, assert results and error codes. For repository tests, DuckDB uses in-memory fixtures; PostgreSQL tests are tagged `integration`.

## Key Patterns & Conventions

### Error Handling: Domain-Driven Error Codes

All errors flow through `internal/shared/errors` package:

```go
// Define errors with domain codes, not HTTP statuses
err := sharederrors.New(sharederrors.CodeInvalidInput, "query required")

// Controllers map domain codes to HTTP response via helper
return mapUseCaseError(ctx, err)  // CodeInvalidInput → 400, CodeNotFound → 404, etc.
```

This decouples business logic from transport. Error codes live in `internal/shared/errors/errors.go`.

### Repository Design: Capabilities-Aware Queries

Repositories expose `Capabilities(ctx)` to allow use cases to adapt at runtime:

```go
caps, _ := repo.Capabilities(ctx)
if caps.HasFTSIndex {
    // DuckDB: use BM25 scoring
} else {
    // PostgreSQL: use tsvector ranking
}
```

Search methods accept `RegionSearchParams` bundling filters and options; backends return `[]entity.RegionWithScore` with optional BPS metadata and scores based on flags.

### Use Case Logic: Option Normalization & Validation

Use cases normalize input via `shared.OptionNormalizer` (bounds-check `Limit`, apply defaults) and validate dataset availability (e.g., `validateDatasetOptions` checks if BPS columns exist before returning BPS data):

```go
if err := uc.normalizer.Normalize(&req.Options); err != nil {
    return nil, err
}
if err := uc.validateDatasetOptions(req.Options); err != nil {
    return nil, err
}
```

### HTTP Delivery: Thin Controllers

Controllers parse Fiber query params into domain request objects and invoke use cases. Error mapping is centralized:

```go
request := model.SearchRequest{
    Query: ctx.Query("q"),
    Options: parseSearchOptions(ctx),
}
results, err := c.uc.Search(ctx.Context(), request)
if err != nil {
    return mapUseCaseError(ctx, err)  // translates domain → HTTP
}
return ctx.JSON(results)
```

### Configuration & Bootstrap

The `internal/config` package centrally wires dependencies:

```go
opts := config.Options{
    DBType:      "duckdb",   // or "postgres"
    DBPath:      "data/regions.duckdb",
    DatabaseURL: "",         // only for PostgreSQL
    Port:        "8080",
}
bootstrap, _ := config.BootstrapHTTP(ctx, opts)
bootstrap.App.Listen(":" + opts.Port)
```

Environment variables override defaults; see `cmd/api/main.go` for the entry point pattern.

## Search Endpoint API

**`GET /v1/search`** – Composite search combining FTS (`q` param) and field-level fuzzy filters (Jaro-Winkler ≥ 0.8):

- `q` – Full-text query (optional; uses DuckDB BM25 or PostgreSQL tsvector).
- `subdistrict`, `district`, `city`, `province` – Fuzzy filters (optional).
- `limit` (default 10, max 100), `search_bps`, `include_bps`, `include_scores` – Options.

Field-level endpoints filter by hierarchy:
- `/v1/search/province?q=...`
- `/v1/search/city?city=...&province=...`
- `/v1/search/district?district=...&city=...&province=...`
- `/v1/search/subdistrict?subdistrict=...&district=...&city=...&province=...`
- `/v1/search/postal/:postalCode`

Response is `[]RegionResponse` with all administrative levels, postal code, and optional BPS metadata + scores.

## Developer Notes

- **Idiomatic Go**: Use tabs, `gofmt`, and `goimports`. Follow standard `testing` package; table-driven test cases in `*_test.go` files alongside code.
- **Context Propagation**: All I/O-bound functions accept `context.Context` as first parameter; use it for timeouts and cancellation (ingestion workflows may be long-running).
- **Interface-Based Dependencies**: Repositories, gateways, and normalizers are injected; this aids testability and runtime backend switching.
- **Structured Logging**: Use `log/slog` with `"error"`, `"context"`, `"duration"` attributes for observability in production.
- **DuckDB Quirks**: In-memory mode (`md:regions`) is stateless; data is lost on restart. For dev, download datasets with `make download-data` and run ingestor. Foreign key checks are disabled by default in DuckDB.
- **PostgreSQL Integration**: Requires a live database and `golang-migrate` for schema setup. Integration tests are tagged and run conditionally.

## Common Commands

```bash
make build               # Compile binary
make run                 # go run ./cmd/api
make ingest              # go run ./cmd/ingestor
make test                # go test -v ./...
make download-data       # Fetch SQL files
make prepare-db          # download-data + ingest
make docker-build        # Build Docker image
make docker-run          # Run image with docker-compose
make fetch-bps           # Download BPS dataset
```

See `Makefile` for all targets and configurations (e.g., `DB_TYPE`, `DATABASE_URL`, `PERIODE` for BPS fetches).
