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

- Go 1.24+ (installed at `/usr/local/go/bin/go`)
- PostgreSQL 17 with pgvector extension
- Docker (for dependencies)

### Quick Start

```bash
# 1. Ensure dependencies are running
pnpm run workspace:status

# 2. Build the server
cd apps/server-go
/usr/local/go/bin/go build ./...

# 3. Run the server
POSTGRES_PASSWORD=your-password /usr/local/go/bin/go run ./cmd/server

# 4. Verify it's running
curl http://localhost:3002/health
```

## Project Structure

```
apps/server-go/
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
│   ├── kreuzberg/        # Document parsing client
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
cd apps/server-go
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
cd apps/server-go

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

### Using workspace-cli

```bash
# Set environment variable to use Go server
export SERVER_IMPLEMENTATION=go

# Start with workspace-cli
pnpm run workspace:start

# Check status
pnpm run workspace:status
```

## Testing

### Running Tests

```bash
cd apps/server-go

# Run all E2E tests
POSTGRES_PASSWORD=your-password /usr/local/go/bin/go test ./tests/e2e/... -v -count=1

# Run specific test suite
POSTGRES_PASSWORD=your-password /usr/local/go/bin/go test ./tests/e2e/... -run "TestHealthSuite" -v

# Run with race detection
POSTGRES_PASSWORD=your-password /usr/local/go/bin/go test ./tests/e2e/... -race -v

# Run with coverage
POSTGRES_PASSWORD=your-password /usr/local/go/bin/go test ./tests/e2e/... -cover
```

### Test Structure

Tests use [testify](https://github.com/stretchr/testify) suites:

```go
type ExampleSuite struct {
    suite.Suite
    ctx *testutil.E2EContext
}

func (s *ExampleSuite) SetupSuite() {
    s.ctx = testutil.NewE2EContext(s.T())
}

func (s *ExampleSuite) TearDownSuite() {
    s.ctx.Cleanup()
}

func (s *ExampleSuite) TestSomething() {
    // Test code
}

func TestExampleSuite(t *testing.T) {
    suite.Run(t, new(ExampleSuite))
}
```

### E2E Context

The `testutil.E2EContext` provides:

- Database connection with test isolation
- HTTP client for API requests
- Authentication helpers
- Cleanup utilities

## Database Migrations

### Creating a Migration

```bash
cd apps/server-go

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
| `MINIO_ENDPOINT`        | MinIO/S3 endpoint       | localhost:9000    |
| `MINIO_ACCESS_KEY`      | MinIO access key        | (required)        |
| `MINIO_SECRET_KEY`      | MinIO secret key        | (required)        |

### Loading from .env

The server loads environment variables from:

1. `apps/server-go/.env`
2. `apps/server-go/.env.local` (for secrets)
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
cd apps/server-go
/root/go/bin/swag init -g cmd/server/main.go -o docs/swagger --parseDependency --parseInternal
```

**Check coverage:**

```bash
bash ./scripts/check-swagger-annotations.sh
```

### Documentation

- **[Quick Start Guide](docs/SWAGGER_QUICK_START.md)** - Copy-paste templates and 5-minute intro
- **[Complete Guide](docs/SWAGGER_ANNOTATIONS.md)** - Comprehensive annotation reference and patterns

### Generated Files

- `docs/swagger/swagger.json` - OpenAPI 2.0 specification (JSON)
- `docs/swagger/swagger.yaml` - OpenAPI 2.0 specification (YAML)
- `docs/swagger/docs.go` - Go embeddings for serving spec

### Automation

The OpenAPI spec is automatically:

- ✅ Generated on every build (`make build`, `nx run server-go:build`)
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
   POSTGRES_PASSWORD=... /usr/local/go/bin/go test ./tests/e2e/... -run "TestHealth" -v
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
