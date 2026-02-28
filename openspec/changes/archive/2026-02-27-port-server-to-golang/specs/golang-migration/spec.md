## ADDED Requirements

### Requirement: Database Migration Ownership

During the transition period (Phases 0-5), TypeORM (NestJS) SHALL remain the single source of truth for database migrations.

The Go server SHALL NOT generate or apply database migrations until full cutover in Phase 6.

#### Scenario: Schema change during transition

- **GIVEN** a new column needs to be added to `kb.documents`
- **WHEN** a developer creates the migration
- **THEN** the migration SHALL be created using TypeORM (`npm run migration:generate`)
- **AND** the migration SHALL be applied using TypeORM (`npm run migration:run`)
- **AND** the corresponding Bun model in the Go server SHALL be updated to include the new field
- **AND** both changes SHALL be committed together

#### Scenario: Bun models match schema

- **WHEN** CI runs on a pull request
- **THEN** integration tests SHALL verify Bun models compile and query successfully
- **AND** the build SHALL fail if models are incompatible with the database schema

#### Scenario: Post-cutover migration ownership

- **GIVEN** Phase 6 cutover is complete
- **WHEN** a new migration is needed
- **THEN** Goose SHALL be used to create and apply migrations
- **AND** TypeORM migration tooling SHALL be retired

---

### Requirement: Go Server Project Structure

The Go server SHALL be organized as a separate application within the Nx monorepo at `apps/server-go/`.

The project SHALL follow standard Go project layout with:

- `cmd/server/` for the main entry point
- `internal/` for private application code
- `pkg/` for shared utilities (if needed externally)

#### Scenario: Project initialization

- **WHEN** the Go server project is created
- **THEN** it SHALL have a valid `go.mod` file with module path `emergent/server`
- **AND** it SHALL compile successfully with `go build ./...`

#### Scenario: Nx integration

- **WHEN** running `nx build server-go`
- **THEN** the Go server SHALL be built using a custom executor
- **AND** the output SHALL be placed in `dist/apps/server-go/`

---

### Requirement: API Contract Compatibility

The Go server SHALL maintain 100% API contract compatibility with the NestJS server for all migrated endpoints.

API responses SHALL match the same JSON structure, status codes, and error formats.

#### Scenario: Identical response format

- **GIVEN** an endpoint `/api/health` exists in both servers
- **WHEN** the same request is made to both servers
- **THEN** the response body structure SHALL be identical
- **AND** the HTTP status code SHALL be identical

#### Scenario: Error response format

- **WHEN** an error occurs in the Go server
- **THEN** the error response SHALL match the NestJS format:
  ```json
  {
    "statusCode": 400,
    "message": "...",
    "error": "Bad Request"
  }
  ```

#### Scenario: Contract test validation

- **WHEN** contract tests are executed against the Go server
- **THEN** 100% of tests SHALL pass before an endpoint is considered migrated

---

### Requirement: Strangler Fig Routing

Traffic routing between NestJS and Go servers SHALL be managed via Traefik path-based routing rules.

Each migrated endpoint SHALL be routed to the Go server while unmigrated endpoints continue to NestJS.

#### Scenario: Gradual migration

- **GIVEN** `/api/health` has been migrated to Go
- **AND** `/api/documents` has not been migrated
- **WHEN** a request is made to `/api/health`
- **THEN** it SHALL be routed to the Go server
- **AND** a request to `/api/documents` SHALL be routed to NestJS

#### Scenario: Rollback capability

- **WHEN** an issue is detected with a Go endpoint
- **THEN** the routing rule can be removed to redirect traffic back to NestJS
- **AND** rollback SHALL require only configuration changes (no code deployment)

---

### Requirement: Authentication Middleware

The Go server SHALL implement JWT-based authentication compatible with Zitadel.

The middleware SHALL validate tokens using OIDC discovery and JWKS rotation.

#### Scenario: Valid token authentication

- **GIVEN** a valid JWT issued by Zitadel
- **WHEN** a request includes the token in `Authorization: Bearer <token>` header
- **THEN** the request SHALL be authenticated
- **AND** user claims SHALL be available to handlers

#### Scenario: Invalid token rejection

- **WHEN** a request includes an invalid or expired JWT
- **THEN** the server SHALL respond with HTTP 401 Unauthorized
- **AND** the response body SHALL include error details

#### Scenario: Scope-based authorization

- **GIVEN** an endpoint requires scope `documents:read`
- **WHEN** a request is made with a token lacking that scope
- **THEN** the server SHALL respond with HTTP 403 Forbidden
- **AND** the response SHALL include `missing_scopes` array

---

### Requirement: Database Access with Bun ORM

Database access SHALL use Bun ORM with manually defined models for type-safe data access.

The pgx driver SHALL be used for PostgreSQL connections with pgvector support.

#### Scenario: Type-safe models

- **WHEN** a database entity is needed
- **THEN** a Go struct with Bun tags SHALL be created (e.g., `bun:"table:kb.documents"`)
- **AND** the struct SHALL include proper type mappings for columns
- **AND** relations SHALL be defined using Bun relation tags

#### Scenario: pgvector compatibility

- **GIVEN** a query using vector similarity search
- **WHEN** the query is executed via Bun with pgx driver
- **THEN** pgvector operations (cosine distance, etc.) SHALL work correctly via custom Vector type
- **AND** embedding vectors SHALL be properly serialized/deserialized

#### Scenario: Connection pooling

- **WHEN** the Go server starts
- **THEN** it SHALL establish a connection pool with configurable min/max connections
- **AND** connections SHALL be health-checked periodically

---

### Requirement: Observability Integration

The Go server SHALL emit OpenTelemetry traces, metrics, and logs compatible with the existing observability stack.

#### Scenario: Distributed tracing

- **WHEN** a request is processed by the Go server
- **THEN** a trace SHALL be created with the same trace ID propagation as NestJS
- **AND** traces SHALL appear in SigNoz/Langfuse

#### Scenario: HTTP metrics

- **WHEN** requests are processed
- **THEN** standard HTTP metrics SHALL be recorded (request count, latency histogram, error rate)
- **AND** metrics SHALL be exportable via OTLP

#### Scenario: Structured logging

- **WHEN** the Go server logs a message
- **THEN** logs SHALL be structured JSON format
- **AND** logs SHALL include trace ID, span ID, and request context

---

### Requirement: Performance Targets

The Go server SHALL meet defined performance targets before endpoints are migrated to production.

#### Scenario: Cold start time

- **WHEN** the Go server container is started
- **THEN** it SHALL be ready to serve requests within 2 seconds
- **AND** health checks SHALL pass immediately after startup

#### Scenario: API latency

- **WHEN** API endpoints are benchmarked under load
- **THEN** P99 latency SHALL be at least 40% lower than the equivalent NestJS endpoint

#### Scenario: Memory usage

- **WHEN** the Go server is running under normal load
- **THEN** memory usage SHALL be at least 50% lower than the NestJS server

#### Scenario: Container image size

- **WHEN** the Go server is built as a Docker image
- **THEN** the image size SHALL be less than 50MB (using distroless or scratch base)

---

### Requirement: Migration Phase Gates

Each migration phase SHALL have explicit go/no-go criteria before proceeding to the next phase.

#### Scenario: Phase 0 gate

- **WHEN** Phase 0 (Foundation) is complete
- **THEN** benchmark results SHALL demonstrate performance improvement potential
- **AND** the team SHALL approve proceeding to Phase 1

#### Scenario: Phase completion criteria

- **GIVEN** any phase is complete
- **WHEN** evaluation is performed
- **THEN** all contract tests for migrated endpoints SHALL pass
- **AND** no regressions SHALL be detected in monitoring
- **AND** documentation SHALL be updated

#### Scenario: Rollback execution

- **WHEN** a phase fails its go/no-go criteria
- **THEN** all traffic SHALL be routed back to NestJS for affected endpoints
- **AND** the issue SHALL be documented before retry
