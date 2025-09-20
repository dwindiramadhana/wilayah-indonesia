# Repository Guidelines

## Project Structure & Module Organization
Source entrypoints live under `cmd/`: `cmd/api` runs the HTTP server and `cmd/ingestor` refreshes DuckDB data. Route handlers, middleware, and HTTP logic stay in `internal/api`, while reusable business rules sit in `pkg/service`. Generated and downloaded datasets belong in `data/`, K8s assets in `helm-chart/`, and load tests in `load-test.js`. Keep public artifacts inside `regions-api/` and update top-level docs in the repo root.

## Build, Test, and Development Commands
Run `make build` to compile the API binary, or `make run` to execute `go run ./cmd/api` during local development. Use `make fetch-bps` (defaults to `PERIODE=latest`) to crawl the BPS API, listing periodes with `python scripts/fetch_bps_wilayah.py --list-periodes` or adding `--verbose` for detailed logs when you need the exact snapshot. Refresh DuckDB artifacts with `make prepare-db`, execute unit suites via `make test` (`go test -v ./...`), and pair `make docker-build` with `make docker-run` for container validation.

## Coding Style & Naming Conventions
Follow idiomatic Go: tabs for indentation, `gofmt` or `go fmt ./...` before committing, and prefer `goimports` for import grouping. Exported identifiers use PascalCase, internals use lowerCamelCase, and filenames reflect their concern (e.g., `service_test.go`). Keep handlers slim and move shared logic into `pkg/service` to maintain separation between transport and domain layers.

## Testing Guidelines
Write tests in `*_test.go` files beside the code they exercise, using Go's standard `testing` package with table-driven cases. Extend `pkg/service/service_test.go` when adjusting region-matching behavior and cover both exact and fuzzy queries. Ensure `go test ./...` remains green before opening a pull request.

## Commit & Pull Request Guidelines
Use Conventional Commits (`feat:`, `docs:`, optional scopes) written in the imperative mood, squashing WIP history. Pull requests should describe the change, list the `make` targets or scripts executed, and link relevant issues. Include response samples or screenshots when API output changes and flag any data migrations so reviewers can run `make prepare-db`.
