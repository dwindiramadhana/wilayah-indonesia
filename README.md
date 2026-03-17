# Indonesian Regions Fuzzy Search API

A high-performance Go API for fuzzy searching Indonesian administrative regions. Supports both **DuckDB** (lightweight, embedded) and **PostgreSQL** (production-ready with native FTS and vector embeddings). This service provides fast and accurate search capabilities for Indonesian provinces, cities, districts, and subdistricts.

## Table of Contents

- [Features](#features)
- [API Usage](#api-usage)
  - [Search Endpoint](#search-endpoint)
  - [Health Check Endpoint](#health-check-endpoint)
- [Configuration](#configuration)
- [Quick Start](#quick-start)
  - [Prerequisites](#prerequisites)
  - [Using Makefile](#using-makefile)
  - [Manual Build and Run](#manual-build-and-run)
  - [Using Docker](#using-docker)
- [Deployment](#deployment)
  - [Building and Pushing Docker Image](#building-and-pushing-docker-image)
  - [Deploying to Cloud Providers](#deploying-to-cloud-providers)
- [Maintenance](#maintenance)
  - [Updating Administrative Data](#updating-administrative-data)
- [Makefile Commands](#makefile-commands)
- [Acknowledgements](#acknowledgements)
- [Project Structure](#project-structure)

## Features

- **DuckDB Backend**: Lightweight, embedded database for fast querying with zero external dependencies.
- **PostgreSQL Backend**: Production-ready with native full-text search (tsvector/tsquery), pgvector embeddings support, and fuzzystrmatch for Jaro-Winkler similarity.
- **BM25 Full-Text Search** (DuckDB): Utilizes DuckDB's `match_bm25` for fast and relevant full-text search across all administrative levels.
- **Native FTS** (PostgreSQL): Uses tsvector/tsquery with GIN indexes for optimized full-text search with ranking.
- **Fuzzy Search**: Employs the Jaro-Winkler similarity algorithm for typo-tolerant searches on specific administrative levels (province, city, district, subdistrict).
- **BPS Integration**: Optionally search and respond with official BPS (Badan Pusat Statistik) codes and names.
- **High Performance**: Powered by DuckDB or PostgreSQL with optimized indexes for fast querying of Indonesian administrative data.
- **Container Ready**: Docker and Docker Compose support for easy deployment.
- **Clean Architecture**: Delivery, use case, repository, and gateway layers are isolated to keep business rules portable.
- **Configurable**: Environment-based configuration for database backend, port, and ingestion data directory.

## Architecture Overview

The codebase follows a Clean Architecture layout:

- `cmd/api`, `cmd/ingestor` – binary entrypoints that delegate to internal bootstrappers.
- `internal/config` – central wiring for loggers, database connections (DuckDB/PostgreSQL), Fiber apps, and use cases.
- `internal/delivery/http` – Fiber controllers, routes, and middleware for the public API.
- `internal/delivery/worker` – CLI-facing delivery adapter that runs dataset refresh workflows.
- `internal/usecase` – business rules for region search and dataset ingestion.
- `internal/repository` – data-access implementations for DuckDB (`duckdb/`) and PostgreSQL (`postgres/`).
- `internal/gateway` – filesystem loader and SQL normalizer abstractions used by the ingestion flow.
- `internal/shared` – cross-cutting concerns such as error taxonomy.

## API Usage

### Search Endpoint

The general search endpoint supports both full-text and field-level fuzzy filters. Use any combination of the parameters below to narrow results.

- **BM25 Full-Text Search**: `q` uses DuckDB `match_bm25` over a combined `full_text` column.
- **Field Fuzzy Filters**: `subdistrict`, `district`, `city`, `province` use Jaro-Winkler (≥ 0.8).
- **Composable**: Combine parameters; scores are aggregated and results ordered by total score.
- **City matching**: `city` matches both "Kota {name}" and "Kabupaten {name}" automatically.
- **Performance**: Returns top 10 results.

```
GET /v1/search?q={query}&subdistrict={name}&district={name}&city={name}&province={name}
```

**Parameters:**
- `q` (optional): Full-text query (e.g., "bandung").
- `subdistrict` (optional): Fuzzy filter for subdistrict.
- `district` (optional): Fuzzy filter for district.
- `city` (optional): Fuzzy filter for city/regency (no need to prefix with Kota/Kabupaten).
- `province` (optional): Fuzzy filter for province.
- `limit` (optional): Maximum records to return (defaults to 10, capped at 100).
- `search_bps` (optional): When `true`, fuzzy comparisons use BPS (Badan Pusat Statistik) names.
- `include_bps` (optional): When `true`, the response adds BPS codes and names for each level.
- `include_scores` (optional): When `true`, the response adds the full-text score and per-field similarity scores.

**Example Requests:**
```bash
# Full-text only
curl "http://localhost:8080/v1/search?q=bandung"

# Combine full-text with province filter
curl "http://localhost:8080/v1/search?q=bandung&province=Jawa Barat"

# Field-only filters (no q)
curl "http://localhost:8080/v1/search?district=Cidadap&city=Bandung&province=Jawa Barat"

# Request BPS metadata and scoring
curl "http://localhost:8080/v1/search?q=bandung&include_bps=true&include_scores=true&limit=5"

# Search using BPS naming
curl "http://localhost:8080/v1/search?q=kemayoran&search_bps=true&include_bps=true"
```

**Example Response:**
```json
[
  {
    "id": "3273010001",
    "subdistrict": "Sukasari",
    "district": "Sukasari",
    "city": "Kota Bandung",
    "province": "Jawa Barat",
    "postal_code": "40151",
    "full_text": "40151 jawa barat kota bandung sukasari sukasari",
    "bps": {
      "subdistrict": {"code": "3273010001", "name": "Sukasari"},
      "district": {"code": "3273010", "name": "Sukasari"},
      "city": {"code": "3273", "name": "Bandung"},
      "province": {"code": "32", "name": "Jawa Barat"}
    },
    "scores": {
      "fts": 6.82,
      "subdistrict": 0.96,
      "district": 0.94,
      "city": 0.91,
      "province": 0.88
    }
  }
]
```

### Specific Search Endpoints

In addition to the general search endpoint, the API provides specific search endpoints for each administrative level. All use Jaro-Winkler fuzzy matching (≥ 0.8), order by similarity, and return up to 10 results by default. They also honour the shared `limit`, `include_bps`, and `include_scores` toggles.

- **District Search**: `/v1/search/district?q={district}&city={city}&province={province}&limit={n}&include_bps={bool}&include_scores={bool}`
  - `q` is required; `city` and `province` are optional narrowing filters.
  - `city` matches both Kota and Kabupaten prefixes automatically.

- **Subdistrict Search**: `/v1/search/subdistrict?q={subdistrict}&district={district}&city={city}&province={province}&limit={n}&include_bps={bool}&include_scores={bool}`
  - `q` is required; `district`, `city`, and `province` are optional narrowing filters.
  - `city` matches both Kota and Kabupaten prefixes automatically.

- **City Search**: `/v1/search/city?q={city}&limit={n}&include_bps={bool}&include_scores={bool}`
  - `q` is required; matches both Kota and Kabupaten.

- **Province Search**: `/v1/search/province?q={province}&limit={n}&include_bps={bool}&include_scores={bool}`
  - `q` is required.

#### District Search Endpoint

```
GET /v1/search/district?q={district}&city={city}&province={province}&limit={n}&include_bps={bool}&include_scores={bool}
```

**Parameters:**
- `q` (required): District name to match (e.g., "sukasari").
- `city` (optional): Narrow by city/regency (no need for Kota/Kabupaten).
- `province` (optional): Narrow by province.

**Example Requests:**
```bash
curl "http://localhost:8080/v1/search/district?q=Cidadap"
curl "http://localhost:8080/v1/search/district?q=Cidadap&city=Bandung&province=Jawa Barat"
```

#### Subdistrict Search Endpoint

```
GET /v1/search/subdistrict?q={subdistrict}&district={district}&city={city}&province={province}&limit={n}&include_bps={bool}&include_scores={bool}
```

**Parameters:**
- `q` (required): Subdistrict name (e.g., "sukasari").
- `district` (optional): Narrow by district.
- `city` (optional): Narrow by city/regency (Kota/Kabupaten handled automatically).
- `province` (optional): Narrow by province.

**Example Requests:**
```bash
curl "http://localhost:8080/v1/search/subdistrict?q=Sukasari"
curl "http://localhost:8080/v1/search/subdistrict?q=Sukasari&district=Sukasari&city=Bandung&province=Jawa Barat"
```

#### City Search Endpoint

```
GET /v1/search/city?q={query}&limit={n}&include_bps={bool}&include_scores={bool}
```

**Parameters:**
- `q` (required): Search query string (e.g., "bandung")

**Example Request:**
```bash
curl "http://localhost:8080/v1/search/city?q=bandung"
```

#### Province Search Endpoint

```
GET /v1/search/province?q={query}&limit={n}&include_bps={bool}&include_scores={bool}
```

**Parameters:**
- `q` (required): Search query string (e.g., "jawa")

**Example Request:**
```bash
curl "http://localhost:8080/v1/search/province?q=jawa"
```
#### Postal Code Search Endpoint

```
GET /v1/search/postal/{postalCode}?limit={n}&include_bps={bool}&include_scores={bool}
```

**Parameters:**
- `postalCode` (required): 5-digit postal code (e.g., "10110")

**Example Request:**
```bash
curl "http://localhost:8080/v1/search/postal/10110"
```

**Example Response:**
```json
[
  {
    "id": "3101010001",
    "subdistrict": "Kepulauan Seribu Utara",
    "district": "Kepulauan Seribu Utara",
    "city": "Kabupaten Kepulauan Seribu",
    "province": "DKI Jakarta",
    "postal_code": "10110",
    "full_text": "dki jakarta kabupaten kepulauan seribu kepulauan seribu utara kepulauan seribu utara"
  }
]
```

The postal code search endpoint:
- Takes a required `postalCode` path parameter containing a 5-digit postal code
- Returns a JSON array of matching regions with that postal code
- Performs an exact match on the postal code
- Limits results to 10 items
- Returns the same Region structure as other search endpoints
- Returns a 404 error if no regions are found for the provided postal code
- Returns a 400 error if the postal code is not a valid 5-digit number

### Health Check Endpoint

```
GET /healthz
```

**Example Request:**
```bash
curl "http://localhost:8080/healthz"
```

**Example Response:**
```json
{
  "status": "ok",
  "message": "Service is healthy"
}
```

## Configuration

The application can be configured using the following environment variables:

| Variable | Description | Default Value |
|----------|-------------|---------------|
| `DB_TYPE` | Database backend: `duckdb` or `postgres` | `duckdb` |
| `DATABASE_URL` | PostgreSQL connection string (required when `DB_TYPE=postgres`) | - |
| `PORT` | Port for the API server to listen on | `8080` |
| `DB_PATH` | Path to the DuckDB database file. The API opens it read-only; the ingestor opens it read-write. | `data/regions.duckdb` |
| `DATA_DIR` | Base directory containing SQL dumps used by the ingestor (`wilayah.sql`, `wilayah_kodepos.sql`, `bps_wilayah.sql`) | `data/` |

### PostgreSQL Connection String Format

When using PostgreSQL, set `DATABASE_URL` in the following format:

```
postgres://user:password@host:port/database?sslmode=disable
```

For local development with Docker Compose:

```
DATABASE_URL=postgres://postgres:postgres@localhost:5432/wilayah_indonesia?sslmode=disable
```

## Quick Start

### DuckDB (Lightweight, Default)

#### Prerequisites

- Go 1.21 or higher
- curl (for downloading data)
- Docker (optional, for containerized deployment)

#### Using Makefile

```bash
# Download the administrative data and prepare the database
make prepare-db

# Run the API server
make run
```

### PostgreSQL (Production-Ready with Native FTS)

#### Prerequisites

- Docker and Docker Compose
- Go 1.21 or higher (for local development)

#### Using Docker Compose (Recommended)

```bash
# Start PostgreSQL container
make postgres-up

# Run database migrations (creates extensions and tables)
make migrate

# Seed the database with administrative data
make seed

# The API is now running at http://localhost:8000
```

#### Local Development with PostgreSQL

```bash
# Start PostgreSQL (requires Docker)
docker compose up -d postgres

# Wait for PostgreSQL to be healthy, then run migrations
make migrate

# Seed the database
make seed

# Run API server with PostgreSQL backend
DB_TYPE=postgres DATABASE_URL=postgres://postgres:postgres@localhost:5432/wilayah_indonesia make run
```

### Manual Build and Run

### Manual Build and Run

#### DuckDB Setup

1. **Download the data:**
   ```bash
   curl -o data/wilayah.sql https://raw.githubusercontent.com/cahyadsn/wilayah/master/db/wilayah.sql
   ```

2. **Prepare the database:**
   ```bash
   go run ./cmd/ingestor/main.go
   ```

3. **Run the API server:**
   ```bash
   go run ./cmd/api/main.go
   ```

#### PostgreSQL Setup

1. **Start PostgreSQL and run migrations:**
   ```bash
   docker compose up -d postgres
   make migrate
   ```

2. **Seed the database:**
   ```bash
   DB_TYPE=postgres go run ./cmd/ingestor/main.go
   ```

3. **Run the API server:**
   ```bash
   DB_TYPE=postgres DATABASE_URL=postgres://postgres:postgres@localhost:5432/wilayah_indonesia go run ./cmd/api/main.go
   ```

### Using Docker

#### DuckDB (Legacy)

1. **Build the Docker image:**
   ```bash
   docker build -t regions-api .
   ```

2. **Run the container:**
   ```bash
   docker run -p 8080:8080 regions-api
   ```

#### PostgreSQL (Recommended for Production)

1. **Start PostgreSQL and run migrations:**
   ```bash
   docker compose up -d postgres
   make migrate
   make seed
   ```

2. **Build and run API with PostgreSQL backend:**
   ```bash
   DB_TYPE=postgres docker build -t regions-api --build-arg DB_TYPE=postgres .
   docker run -p 8000:8000 \
     -e DB_TYPE=postgres \
     -e DATABASE_URL=postgres://postgres:postgres@postgres:5432/wilayah_indonesia \
     regions-api
   ```

**Using Docker Compose (Simplest):**
```bash
# Development
docker compose up

# Production (uses docker-compose.prod.yml)
docker compose -f docker-compose.prod.yml up
```

## Deployment

### Building and Pushing Docker Image

To build and push the Docker image to a container registry:

```bash
# Build the image
docker build -t your-registry/regions-api:latest .

# Push to container registry
docker push your-registry/regions-api:latest
```

### Deploying to Cloud Providers

#### Fly.io

1. Install the Fly.io CLI
2. Create a fly.toml file:
   ```toml
   app = "regions-api"
   
   [build]
     dockerfile = "Dockerfile"
   
   [env]
     PORT = "8080"
   
   [[services]]
     internal_port = 8080
     protocol = "tcp"
   
     [[services.ports]]
       port = 80
       handlers = ["http"]
   
     [[services.ports]]
       port = 443
       handlers = ["tls", "http"]
   ```

3. Deploy:
   ```bash
   flyctl launch
   ```

#### Railway

1. Connect your GitHub repository to Railway
2. Set environment variables in Railway dashboard:
   - PORT: 8080
3. Railway will automatically build and deploy using the Dockerfile

#### DigitalOcean App Platform

1. Create a new app and connect your repository
2. Set environment variables:
   - PORT: 8080
3. Set the build command to:
   ```bash
   docker build -t regions-api .
   ```
4. Set the run command to:
   ```bash
   docker run -p $PORT:8080 regions-api
   ```

## Maintenance

### Updating Administrative Data

To update the regions database with new administrative data:

1. **Download the latest data:**
   ```bash
   make download-data
   ```

2. **Reprocess the data:**
   ```bash
   make ingest
   ```

   Or run the ingestor manually:
   ```bash
   go run ./cmd/ingestor/main.go
   ```

This process will:
- Download the latest `wilayah.sql` file
- Create a new `regions.duckdb` database
- Transform the hierarchical data into a denormalized table for efficient searching
- Clean up temporary tables to keep the database file small

## Makefile Commands

### DuckDB Commands

| Command | Description |
|---------|-------------|
| `make prepare-db` | Download data and run ingestor (recommended for first run) |
| `make run` | Run the API server on port 8080 |
| `make ingest` | Run the data ingestor |
| `make download-data` | Download the SQL data file |
| `make build` | Build the API binary |

### PostgreSQL Commands

| Command | Description |
|---------|-------------|
| `make postgres-up` | Start PostgreSQL container |
| `make postgres-down` | Stop PostgreSQL container |
| `make migrate` | Run all SQL migrations in order |
| `make seed` | Run ingestor to seed database with data |
| `make run-postgres` | Run API server with PostgreSQL backend |

### Docker Commands

| Command | Description |
|---------|-------------|
| `make docker-build` | Build Docker image (PostgreSQL target) |
| `make docker-run` | Run Docker container |
| `make docker-compose-up` | Start all services with Docker Compose |

### Utility Commands

| Command | Description |
|---------|-------------|
| `make test` | Run unit tests |
| `make test-integration` | Run PostgreSQL integration tests (requires PostgreSQL) |
| `make clean` | Clean build artifacts |
| `make deps` | Install dependencies |
| `make help` | Show help message |

## Acknowledgements

We would like to express our gratitude to [cahyadsn](https://github.com/cahyadsn) for contributing the Indonesian administrative regions data that powers this API. The data is sourced from the [wilayah](https://github.com/cahyadsn/wilayah) repository, which provides comprehensive and up-to-date information about Indonesian provinces, cities, districts, and subdistricts.

## Project Structure

```
.
├── cmd/
│   ├── api/          # Main application entrypoint
│   └── ingestor/     # Data ingestion script
├── data/
│   ├── regions.duckdb # DuckDB database file (generated)
│   └── *.sql         # Raw SQL data files (downloaded)
├── internal/
│   ├── config/       # Application bootstrapping and configuration
│   ├── delivery/
│   │   ├── http/     # HTTP handlers, routers, middleware
│   │   └── worker/   # CLI worker for ingestion workflows
│   ├── gateway/      # Filesystem loader, SQL normalizer
│   ├── model/        # Domain models and DTOs
│   ├── repository/
│   │   ├── duckdb/   # DuckDB repository implementations
│   │   └── postgres/ # PostgreSQL repository implementations
│   ├── shared/       # Cross-cutting concerns (errors, etc.)
│   └── usecase/      # Business logic (region search, ingestion)
├── migrations/       # PostgreSQL migration scripts
│   ├── 001_enable_extensions.sql
│   ├── 002_create_regions_table.sql
│   ├── 003_create_fts_indexes.sql
│   └── 004_add_vector_embeddings.sql
├── docker-compose.yml       # Development Docker Compose
├── docker-compose.prod.yml  # Production Docker Compose
├── Dockerfile        # Multi-stage Docker build
├── Makefile          # Build and run commands
├── go.mod            # Go module file
└── go.sum            # Go checksum file
```
