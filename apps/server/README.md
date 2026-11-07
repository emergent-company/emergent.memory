# Go Server Documentation

This document provides comprehensive guidance for developing, deploying, and debugging the Go server implementation.

## Table of Contents

1. [Getting Started](#getting-started)
2. [Project Structure](#project-structure)
3. [Development Workflow](#development-workflow)
4. [Building and Running](#building-and-running)
5. [Testing](#testing)
6. [Database Migrations](#database-migrations)
7. [Configuration](#configuration)
8. [API Documentation (Swagger/OpenAPI)](#api-documentation-swaggeropenapi)
9. [Debugging](#debugging)
10. [Deployment](#deployment)
11. [Troubleshooting](#troubleshooting)

## Getting Started

### Prerequisites

- Go 1.26.6+ — the version declared in `apps/server/go.mod` and the workspace `go.work` (installed at `/usr/local/go/bin/go`)
- PostgreSQL 17 or newer with the pgvector extension (the self-hosted installer deploys 18)
- Docker (for dependencies)

### Quick Start

```bash
# 1. Start dependencies (PostgreSQL 18 + pgvector, object store) from the
#    repository root. `storage-init` boots from the server image, so build it
#    once; see CONTRIBUTING.md for the full local setup (auth included):
#      docker compose -f deploy/self-hosted/docker-compose.local.yml build server
#      docker compose -f deploy/self-hosted/docker-compose.local.yml up -d db seaweedfs storage-init

# 2. Build the server and migrate the database once. Run the task from the
#    repository root: only the root Taskfile loads .env, and `migrate:up` takes
#    POSTGRES_HOST from it (`apps/server/Taskfile.yml` has no dotenv of its own).
/usr/local/go/bin/go build -C apps/server ./...
POSTGRES_PASSWORD=your-password task migrate:up

# 3. Run the server. STANDALONE_MODE means API-key auth, so /health does not
#    depend on a Zitadel instance a fresh checkout does not have.
cd apps/server
POSTGRES_PASSWORD=your-password STANDALONE_MODE=true STANDALONE_API_KEY=dev-key \
  /usr/local/go/bin/go run ./cmd/server

# 4. Verify it's running (503 means a critical component — database, storage or
#    auth — is not healthy yet; `storage-init` is one-shot and may need a moment)
curl http://localhost:3002/health
```

## Project Structure

```
apps/server/
├── cmd/
│   ├── server/           # Main server entry point
│   │   └── main.go       # fx.New() composition
│   └── migrate/          # Migration CLI tool
│       └── main.go
├── domain/               # Domain modules (fx.Module per domain)
│   ├── apitoken/         # API token authentication
│   ├── chat/             # AI chat with streaming
│   ├── chunk/            # Document chunks
│   ├── datasource/       # External data source sync
│   ├── document/         # Document management
│   ├── email/            # Email job queue
│   ├── extraction/       # Document extraction pipeline
│   ├── graph/            # Graph objects & relationships
│   ├── health/           # Health endpoints
│   ├── mcp/              # Model Context Protocol
│   ├── organization/     # Multi-tenant orgs
│   ├── project/          # Project management
│   ├── scheduler/        # Scheduled tasks
│   ├── search/           # Unified search
│   ├── storage/          # File storage (MinIO/S3)
│   └── user/             # User profiles
├── internal/
│   ├── config/           # Environment configuration
│   ├── database/         # Bun connection, fx lifecycle
│   ├── jobs/             # Job queue infrastructure
│   ├── migrate/          # Programmatic migration API
│   ├── server/           # Echo setup, middleware
│   └── testutil/         # Test utilities
├── migrations/           # Goose SQL migrations
├── pkg/
│   ├── adk/              # Google ADK-Go integration
│   ├── auth/             # Zitadel middleware
│   ├── xberg/            # Document parsing client
│   ├── mailgun/          # Email delivery
│   └── vertex/           # Vertex AI embeddings
└── tests/
    └── e2e/              # End-to-end tests
```

### Module Pattern

Each domain module follows this structure:

```
domain/example/
├── module.go      # fx.Module definition
├── entity.go      # Bun models
├── store.go       # Database access (repository)
├── service.go     # Business logic
├── handler.go     # HTTP handlers
├── routes.go      # Route registration
└── dto.go         # Request/response types
```

## Development Workflow

### Making Changes

1. **Edit code** - Make your changes in the appropriate domain module
2. **Hot reload** - The server automatically rebuilds on file changes (if using `air`)
3. **Run tests** - Verify your changes with E2E tests

### Using Air for Hot Reload

```bash
# Install air
go install github.com/air-verse/air@latest

# Run with hot reload
cd apps/server
air
```

### Code Formatting

```bash
# Format code
gofmt -w .

# Or use goimports
goimports -w .
```

### Linting

```bash
# Install golangci-lint
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

# Run linter
golangci-lint run
```

## Building and Running

### Development Mode

```bash
cd apps/server

# Run directly with go run
POSTGRES_PASSWORD=your-password /usr/local/go/bin/go run ./cmd/server

# Or build first
/usr/local/go/bin/go build -o ./bin/server ./cmd/server
POSTGRES_PASSWORD=your-password ./bin/server
```

### Production Build

```bash
# Build optimized binary
/usr/local/go/bin/go build -ldflags="-s -w" -o ./bin/server ./cmd/server

# Binary is ~36MB
ls -lh ./bin/server
```

### Using task targets

From the product repo root the `Taskfile.yml` drives the same flow:

```bash
task dev      # air hot reload, foreground (writes logs/server/)
task start    # build + run in the background (writes pids/server.pid)
task status   # health check; reads pids/server.pid, so use it after task start
task build    # local binary only — no swagger, no version ldflags
```

## Testing

### Running Tests

```bash
# From the repo root — the supported entry point
task test:integration

# Run a specific suite (CLI_ARGS are passed through to `go test`)
task test:integration -- -run TestAgentCompatSuite
```

The task expands to `cd apps/server && go test ./tests/integration/... -v -count=1`.
DB-backed tests need a reachable Postgres. Without one they skip locally; set
`REQUIRE_DB=1` (as CI does) to fail instead of skipping:

```bash
cd apps/server
REQUIRE_DB=1 TEST_DATABASE_URL='postgres://emergent:test@localhost:5432/emergent?sslmode=disable' \
  /usr/local/go/bin/go test ./tests/integration/... -v -count=1

# Race detector / coverage, same package set
REQUIRE_DB=1 /usr/local/go/bin/go test ./tests/integration/... -race -v
REQUIRE_DB=1 /usr/local/go/bin/go test ./tests/integration/... -cover
```

The API e2e suites are separate, live at `e2e/tests/api` (module
`github.com/emergent-company/emergent.memory/e2e`), and are driven by
`task test:e2e` from the repo root.

### Test Structure

Tests use [testify](https://github.com/stretchr/testify) suites:

```go
type ExampleSuite struct {
    testutil.BaseSuite
}

func (s *ExampleSuite) TestSomething() {
    // s.Client, s.ProjectID, s.OrgID are ready; the DB is transaction-isolated.
    resp := s.Client.GET("/api/something", testutil.WithAuth("e2e-test-user"))
    s.Equal(http.StatusOK, resp.StatusCode)
}

func TestExampleSuite(t *testing.T) {
    suite.Run(t, new(ExampleSuite))
}
```

### Base suite

`testutil.BaseSuite` (`internal/testutil/suite.go`) provides:

- Isolated test database per suite, with per-test transaction rollback
- `s.Client` — HTTP client for API requests; set `TEST_SERVER_URL` to target an
  already-running server instead of the in-process one
- Common fixtures (`s.ProjectID`, `s.OrgID`) and request helpers
  (`testutil.WithAuth`, `WithProjectID`, `WithBody`)
- Environment contract: a missing database skips the suite locally but fails it
  when `REQUIRE_DB=1`

## Database Migrations

### Creating a Migration

```bash
cd apps/server

# Using the CLI
POSTGRES_PASSWORD=your-password /usr/local/go/bin/go run ./cmd/migrate -c create add_new_feature

# Or manually create
touch migrations/00002_add_new_feature.sql
```

### Migration Format

```sql
-- +goose Up
-- +goose StatementBegin
CREATE TABLE kb.new_table (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS kb.new_table;
-- +goose StatementEnd
```

### Running Migrations

```bash
# Check status
POSTGRES_PASSWORD=your-password /usr/local/go/bin/go run ./cmd/migrate -c status

# Run all pending
POSTGRES_PASSWORD=your-password /usr/local/go/bin/go run ./cmd/migrate -c up

# Rollback last
POSTGRES_PASSWORD=your-password /usr/local/go/bin/go run ./cmd/migrate -c down
```

See `migrations/README.md` for detailed documentation.

## Configuration

### Environment Variables

| Variable                | Description             | Default           |
| ----------------------- | ----------------------- | ----------------- |
| `SERVER_PORT`           | HTTP server port        | 3002              |
| `DB_HOST`               | PostgreSQL host         | localhost         |
| `DB_PORT`               | PostgreSQL port         | 5432              |
| `POSTGRES_USER`         | Database user           | emergent          |
| `POSTGRES_PASSWORD`     | Database password       | (required)        |
| `POSTGRES_DATABASE`     | Database name           | emergent          |
| `ZITADEL_DOMAIN`        | Zitadel instance domain | (required)        |
| `ZITADEL_CLIENT_ID`     | OAuth client ID         | (required)        |
| `ZITADEL_CLIENT_SECRET` | OAuth client secret     | (required)        |
| `VERTEX_PROJECT_ID`     | Google Cloud project    | (required for AI) |
| `VERTEX_LOCATION`       | Vertex AI region        | us-central1       |
| `STORAGE_ENDPOINT`      | S3-compatible endpoint  | (dev: localhost:9000) |
| `STORAGE_ACCESS_KEY`    | Object-store access key | (required for storage) |
| `STORAGE_SECRET_KEY`    | Object-store secret key | (required for storage) |
| `STORAGE_PROVIDER`      | `seaweedfs` / `s3` / `minio` | unset (generic S3) |
| `STORAGE_BUCKET_DOCUMENTS` | Documents bucket     | documents         |
| `STORAGE_BUCKET_TEMP`   | Temporary-upload bucket | document-temp     |
| `STORAGE_REGION`        | Signing region          | us-east-1         |

### LLM provider environment variables

When no provider credentials are configured in the database, the server can
build an LLM client from environment variables. The four legacy providers keep
their historical variables:

| Provider       | Variables                                                                 |
| -------------- | ------------------------------------------------------------------------- |
| DeepSeek       | `DEEPSEEK_API_KEY`, `DEEPSEEK_MODEL` (routed, e.g. `deepseek/deepseek-v4-flash`) |
| OpenAI         | `OPENAI_API_KEY`, `OPENAI_BASE_URL` (optional), `OPENAI_MODEL`            |
| Google AI      | `GOOGLE_API_KEY`                                                          |
| Vertex AI      | `GCP_PROJECT_ID`, `VERTEX_AI_LOCATION`                                    |

Every other registered vendor is configured generically. The prefix is the
vendor's dialect upper-cased with `-` replaced by `_`:

```
<PREFIX>_API_KEY    # required to enable the vendor
<PREFIX>_BASE_URL   # optional; defaults to the vendor's registered endpoint
<PREFIX>_MODEL      # model id (bare, or routed as <vendor>/<model>)
```

Examples: `ANTHROPIC_API_KEY` / `ANTHROPIC_MODEL` (Anthropic),
`OPENROUTER_API_KEY` / `OPENROUTER_BASE_URL` / `OPENROUTER_MODEL`
(OpenRouter), and `AZURE_OPENAI_API_KEY` / `AZURE_OPENAI_BASE_URL` (Azure
OpenAI, whose registered endpoint contains a `{resource}` placeholder that the
env path does not substitute, so the concrete base URL is required). A vendor
is enabled only when its API key is set — a base URL or model alone never
enables it. Database provider configuration always takes precedence over env;
env is a fallback only when no stored credential exists.

### Loading from .env

The server loads environment variables from:

1. `apps/server/.env`
2. `apps/server/.env.local` (for secrets)
3. System environment

## API Documentation (Swagger/OpenAPI)

The API is documented using Swagger/OpenAPI annotations in handler files. The specification is automatically generated during builds.

### Quick Start

**Add annotations to new endpoints:**

```go
// Create creates a new resource
// @Summary      Create resource
// @Tags         resources
// @Accept       json
// @Produce      json
// @Param        request body CreateRequest true "Resource data"
// @Success      201 {object} Resource
// @Failure      400 {object} apperror.Error
// @Router       /api/resources [post]
// @Security     bearerAuth
func (h *Handler) Create(c echo.Context) error {
```

**Generate specification:**

```bash
cd apps/server
/root/go/bin/swag init -g cmd/server/main.go -o docs/swagger --parseDependency --parseInternal
```

**Check coverage:**

```bash
bash ./scripts/check-swagger-annotations.sh
```

### Documentation

The handler annotations are the only source for the spec, so the guards below
are the reference (paths from the repo root):

- `apps/server/scripts/check-swagger-annotations.sh` — every handler carries an
  `@Router`
- `scripts/preflight/swagger-raw-json.sh` — `json.RawMessage` fields carry a
  `swaggertype`, so a generated client does not decode them as `[]int`

### Generated Files

These are **build output, not source** — they are gitignored and regenerated
from the handler annotations (`task server:swagger`, the Makefile, both
Dockerfiles and the docs workflow all run the generator):

- `docs/swagger/swagger.json` - OpenAPI 2.0 specification (JSON)
- `docs/swagger/swagger.yaml` - OpenAPI 2.0 specification (YAML)
- `docs/swagger/docs.go` - generated Go spec registration (not compiled in)

The server serves `docs/swagger/swagger.json` from disk at runtime (see
`domain/devtools`), so generate them before running a binary built outside the
`task`/`make`/Docker paths.

### Automation

The OpenAPI spec is automatically:

- ✅ Generated by `task server:swagger`, incrementally (`status` skips it when
  no `.go` file is newer than the spec), and by `task build` / `task dev`
- ✅ Validated by pre-commit hook (blocks commits with missing annotations)
- ✅ Tracked by coverage script (reports annotation completeness)

**Current coverage:** 7/31 handler files (22.6%)

## Debugging

### Logging

```go
// Using zap logger (injected via fx)
func NewService(logger *zap.Logger) *Service {
    return &Service{
        logger: logger.Named("service"),
    }
}

func (s *Service) DoSomething() {
    s.logger.Info("doing something",
        zap.String("key", "value"),
        zap.Error(err),
    )
}
```

### Log Levels

Set via `LOG_LEVEL` environment variable:

- `debug` - Verbose debugging
- `info` - Normal operation (default)
- `warn` - Warnings only
- `error` - Errors only

### Debugging with Delve

```bash
# Install delve
go install github.com/go-delve/delve/cmd/dlv@latest

# Debug the server
dlv debug ./cmd/server

# Or attach to running process
dlv attach $(pgrep -f 'go run.*cmd/server')
```

### Common Debug Commands

```bash
# Check server health
curl http://localhost:3002/health

# Check readiness
curl http://localhost:3002/ready

# List routes (if enabled)
curl http://localhost:3002/debug/routes

# Check database connection
POSTGRES_PASSWORD=... psql -h localhost -U emergent -d emergent -c "SELECT 1"
```

## Deployment

### Docker Build

```dockerfile
# Dockerfile
FROM golang:1.24-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /server ./cmd/server

FROM alpine:3.19
RUN apk --no-cache add ca-certificates
COPY --from=builder /server /server
EXPOSE 3002
CMD ["/server"]
```

```bash
# Build
docker build -t emergent-server-go .

# Run
docker run -p 3002:3002 \
  -e POSTGRES_PASSWORD=... \
  -e ZITADEL_DOMAIN=... \
  emergent-server-go
```

### Kubernetes

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: server-go
spec:
  replicas: 3
  template:
    spec:
      containers:
        - name: server
          image: emergent-server-go:latest
          ports:
            - containerPort: 3002
          env:
            - name: POSTGRES_PASSWORD
              valueFrom:
                secretKeyRef:
                  name: db-secrets
                  key: password
          livenessProbe:
            httpGet:
              path: /health
              port: 3002
            initialDelaySeconds: 5
          readinessProbe:
            httpGet:
              path: /ready
              port: 3002
            initialDelaySeconds: 5
```

## Troubleshooting

### Server Won't Start

1. **Check database connection:**

   ```bash
   POSTGRES_PASSWORD=... psql -h localhost -U emergent -d emergent -c "SELECT 1"
   ```

2. **Check port availability:**

   ```bash
   lsof -i :3002
   ```

3. **Check environment variables:**
   ```bash
   env | grep -E "(POSTGRES|ZITADEL|SERVER)"
   ```

### Tests Failing

1. **Ensure test database is clean:**

   ```bash
   # Tests use their own schema isolation
   REQUIRE_DB=1 /usr/local/go/bin/go test ./tests/integration/... -run "TestAgentCompat" -v
   ```

2. **Check for port conflicts:**
   ```bash
   # Tests may use random ports
   netstat -tlnp | grep LISTEN
   ```

### Migration Issues

1. **Check migration status:**

   ```bash
   POSTGRES_PASSWORD=... /usr/local/go/bin/go run ./cmd/migrate -c status
   ```

2. **Verify goose_db_version table:**
   ```sql
   SELECT * FROM goose_db_version ORDER BY id;
   ```

### Performance Issues

1. **Enable profiling:**

   ```go
   import _ "net/http/pprof"
   // Access at http://localhost:3002/debug/pprof/
   ```

2. **Check database queries:**

   ```bash
   # Enable query logging in PostgreSQL
   ALTER SYSTEM SET log_statement = 'all';
   SELECT pg_reload_conf();
   ```

3. **Monitor goroutines:**
   ```bash
   curl http://localhost:3002/debug/pprof/goroutine?debug=1
   ```
