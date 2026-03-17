# Multi-stage Dockerfile for Indonesian Regions Fuzzy Search API
# Supports both DuckDB (legacy) and PostgreSQL (recommended) backends

# ============================================
# Stage 1: Builder
# ============================================
FROM golang:1.24.6-bookworm AS builder

ARG DB_TYPE=postgres

# Install build tools and PostgreSQL client for migrations
RUN apt-get update && apt-get install -y \
    build-essential \
    curl \
    && rm -rf /var/lib/apt/lists/*

# Set working directory
WORKDIR /app

# Copy go mod and sum files
COPY go.mod go.sum ./

# Download all dependencies
RUN go mod download

# Copy source code
COPY . .

# Build binaries based on DB_TYPE
RUN if [ "$DB_TYPE" = "postgres" ]; then \
        # Build API binary for PostgreSQL (no CGO needed) \
        go build -o regions-api ./cmd/api; \
        # Build ingestor binary \
        go build -o regions-ingestor ./cmd/ingestor; \
    else \
        # Build for DuckDB (CGO required) \
        DB_PATH="/app/data/regions.duckdb" make prepare-db; \
        CGO_ENABLED=1 GOOS=linux go build -ldflags="-w -s" -o regions-api ./cmd/api; \
        CGO_ENABLED=1 GOOS=linux go build -ldflags="-w -s" -o regions-ingestor ./cmd/ingestor; \
    fi

# ============================================
# Stage 2: Final (PostgreSQL)
# ============================================
FROM gcr.io/distroless/cc-debian12 AS postgres-final

# Set working directory
WORKDIR /app

# Copy the binary from builder stage
COPY --from=builder /app/regions-api .
COPY --from=builder /app/regions-ingestor .
COPY --from=builder /app/migrations ./migrations

# Expose port
EXPOSE 8000

# Default command (API server)
CMD ["/app/regions-api"]

# ============================================
# Stage 3: Final (DuckDB - Legacy)
# ============================================
FROM gcr.io/distroless/cc-debian12 AS duckdb-final

# Set working directory
WORKDIR /app

# Copy the binary from builder stage
COPY --from=builder /app/regions-api .
COPY --from=builder /app/regions-ingestor .

# Copy the database file
COPY --from=builder /app/data/regions.duckdb ./data/regions.duckdb

# Expose port
EXPOSE 8000

# Default command (API server)
CMD ["/app/regions-api"]

# Default target is postgres-final
FROM postgres-final
