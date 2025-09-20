# Project Guide

## Overview
- Domain: user, contact, and address management with optional event streaming.
- Executables: `cmd/web` exposes the HTTP API; `cmd/worker` hosts Kafka consumers.
- Layering: classic Clean Architecture (Delivery → Use Case → Repository → External systems) as drawn in `architecture.png`.
- Tech: Go 1.21+, Fiber, GORM (MySQL), Viper, Logrus, Sarama, Kafka (optional), go-playground/validator.

## Repository Layout
- `cmd/` – entrypoints. `cmd/web/main.go` boots the HTTP server, `cmd/worker/main.go` attaches consumers.
- `internal/config/` – constructors for infrastructure (Viper config loader, Fiber app, database, logger, validator, Kafka clients) plus the `Bootstrap` wiring function.
- `internal/delivery/` – inbound adapters. HTTP controllers & routes under `internal/delivery/http`, Kafka consumers under `internal/delivery/messaging`.
- `internal/usecase/` – application business rules orchestrating transactions, validation, repositories, and producers.
- `internal/repository/` – data access abstractions on top of GORM.
- `internal/entity/` – persistence models (GORM entities) that represent database tables.
- `internal/model/` – transport models: request/response payloads, events, converters, pagination helpers, and auth context.
- `internal/gateway/` – outbound adapters (Kafka producers encapsulated in generic helpers).
- `api/` – OpenAPI 3.0 specification (`api-spec.json`).
- `db/migrations/` – SQL migrations for MySQL schema.
- `test/` – black-box integration-style tests exercising the HTTP API against real infrastructure components.

## Application Wiring
1. `cmd/web/main.go` loads configuration with `config.NewViper()`, then instantiates logger, database, validator, Fiber app, and optional Kafka producer before invoking `config.Bootstrap(...)`.
2. `internal/config/app.go` creates repositories, Kafka producers (when enabled), use cases, HTTP controllers, and middleware, then registers routes via `internal/delivery/http/route`.
3. Controllers parse requests, call use cases, and return `model.WebResponse` objects. Validation is centralized through `validator/v10` instances passed to each use case.
4. Use cases start GORM transactions, call repositories, and emit events through Kafka producers when configured. Transactions are committed before events are sent to keep side effects consistent.

## HTTP Delivery
- Routing lives in `internal/delivery/http/route`, mapping REST endpoints to controllers and applying the auth middleware.
- Controllers live in `internal/delivery/http` and only orchestrate request parsing, invoking use cases, and shaping responses.
- `internal/delivery/http/middleware/auth_middleware.go` verifies the `Authorization` token by calling the user use case and injects the authenticated user into the request context.
- Error handling is unified via `config.NewFiber` which sets a JSON error handler.

## Kafka Delivery
- `cmd/worker/main.go` spins three consumer goroutines (users, contacts, addresses). Each uses `config.NewKafkaConsumerGroup` and `messaging.ConsumeTopic` to manage Sarama loops.
- Consumer handlers (`internal/delivery/messaging/*.go`) unmarshal events and currently log them with TODOs for future business logic.
- Producers (`internal/gateway/messaging/*.go`) wrap a generic `Producer` that serializes models and sends keyed messages. Topics default to `users`, `contacts`, and `addresses`.

## Use Cases & Domain Logic
- User workflows (`internal/usecase/user_usecase.go`) cover register, login, current user lookup, logout, update, and token verification. Password hashing uses bcrypt. Every method enforces validation and commits within a transaction.
- Contact workflows (`internal/usecase/contact_usecase.go`) implement CRUD + search with pagination and filtering helpers in the repository.
- Address workflows (`internal/usecase/address_usecase.go`) depend on both the contact and address repositories to enforce ownership boundaries.
- Event conversion helpers in `internal/model/converter` translate entities to API responses or Kafka payloads.

## Persistence Layer
- Repositories embed a generic `Repository[T]` that provides CRUD helpers. Custom queries live alongside (e.g., `FindByToken`, `Search`, `FindByIdAndContactId`).
- Entities in `internal/entity` map 1:1 to tables declared in the SQL migrations. Timestamps use `autoCreateTime` / `autoUpdateTime` at millisecond precision.
- Transactions: use cases call `db.WithContext(ctx).Begin()` and defer rollbacks; they must `tx.Commit()` to persist work.

## Configuration
- Primary configuration file: `config.json`. Sections: `app`, `web`, `log`, `database`, `kafka`.
- Viper search paths (`internal/config/viper.go`) allow running binaries from `cmd/*` or repository root.
- Kafka producer is optional (`kafka.producer.enabled`). When disabled, producers are `nil` and use cases log that events are skipped.
- MySQL DSN is built dynamically; ensure credentials and database exist before running the app.

## Database & Migrations
1. Install `golang-migrate`. Example command (update DSN as needed):
   ```bash
   migrate -database "mysql://root:@tcp(localhost:3306)/golang_clean_architecture?charset=utf8mb4&parseTime=True&loc=Local" -path db/migrations up
   ```
2. Migrations create `users`, `contacts`, `addresses` tables with foreign keys.
3. To generate new migrations, use `migrate create -ext sql -dir db/migrations add_table_xyz`.

## HTTP API & Manual Testing
- The OpenAPI file (`api/api-spec.json`) documents the REST contract. Import it into Swagger UI, Stoplight, or Insomnia for exploration.
- `test/manual.http` (VS Code REST Client format) contains ready-made requests. Parameters (`{{token}}`, `{{contactId}}`, etc.) resolve via `test/http-client.env.json`.

## Testing Strategy
- `go test ./test/...` runs integration-style tests. The suite boots real configuration (`test/init.go`) and hits the application via Fiber’s in-memory test server.
- Tests assume a reachable MySQL instance specified in `config.json`. They truncate tables between cases through helpers.
- No mocks are used; this keeps parity with production wiring but requires local dependencies.

## Local Development Workflow
1. Start dependencies:
   - MySQL with the schema from `db/migrations`.
   - Kafka/ZooKeeper if you plan to enable producers or run the worker service.
2. Export any environment overrides or edit `config.json` for local credentials.
3. Apply migrations (see above).
4. Run API server: `go run cmd/web/main.go`.
5. (Optional) Run background consumers: `go run cmd/worker/main.go`.
6. Execute tests: `go test -v ./test`.
7. Use the manual HTTP scripts or the OpenAPI spec for manual verification.

## Extending the System
- New endpoint pattern:
  1. Define request/response structs in `internal/model`.
  2. Add entity changes & migrations if persistence is required.
  3. Extend repository interfaces with required queries.
  4. Implement business logic in a new or existing use case.
  5. Create a controller method and route entry.
  6. Add converter helpers and, if applicable, Kafka events.
  7. Cover with tests under `test/` and update the OpenAPI spec.
- New Kafka topic: add a producer wrapper in `internal/gateway/messaging`, configure `Bootstrap` to construct it, and create a consumer handler plus goroutine in `cmd/worker/main.go`.

## Observability & Error Handling
- Logging uses Logrus JSON formatter; log level is configured via `log.level` (int) in `config.json`.
- Fiber error handler wraps errors into `{ "errors": "message" }` JSON responses.
- Kafka components surface errors through logs; consumer groups run in dedicated goroutines with graceful shutdown triggered by OS signals.

## Common Troubleshooting
- “Failed to connect database”: verify MySQL is running and credentials match `config.json`.
- “Kafka producer is disabled”: set `"kafka": { "producer": { "enabled": true }}` and ensure brokers are reachable.
- Tests failing because of stale data: they should clean tables automatically, but you can call `ClearAll()` manually inside new tests.

## Reference Assets
- Architectural diagram: `architecture.png` (editable source `architecture.excalidraw`).
- License, README, and this guide live at repository root for quick onboarding.
