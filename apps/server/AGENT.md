# Go Server - AI Agent Guide

## Overview

This is the Go backend server for the Emergent platform. It uses:

- **Echo** - HTTP framework
- **Bun** - ORM with pgx driver for PostgreSQL
- **fx** - Uber's dependency injection framework
- **Zitadel** - OAuth2/OIDC authentication
- **Google ADK-Go** - AI extraction pipeline orchestration

**Status**: **Production Ready**. There is no server-local e2e suite. The integration suite lives in `apps/server/tests/integration` (17 files, run in-process against a test DB). HTTP API e2e suites live in the separate `e2e/tests-api` Go module (run via the root `task test:e2e`); CLI e2e lives in the `e2e/` module (run with `runlog test`).

## Quick Commands (Tasks CLI)

The `cmd/tasks` CLI provides common development commands:

```bash
cd apps/server

# Build - ALWAYS run after code changes to catch compile errors
go run ./cmd/tasks build           # Build all packages (fast ~2s)

# Health check - verify server is running
go run ./cmd/tasks health          # Quick check
go run ./cmd/tasks health -v       # Verbose (show response bodies)
go run ./cmd/tasks health -v -db   # Also check database connectivity

# Run tests (from repo root)
task test:integration                # Integration tests (apps/server/tests/integration)
task test:e2e                        # API e2e suites (e2e/tests-api)
go run ./cmd/tasks test:unit         # Unit tests only

# NOTE: `cmd/tasks test:e2e` is stale — it targets the removed apps/server/tests/e2e/.
# Use the root `task test:e2e` target instead.

# Lint and format
go run ./cmd/tasks lint            # Run golangci-lint
go run ./cmd/tasks fmt             # Run gofmt

# Database
go run ./cmd/tasks db:status       # Check database connectivity and info
```

### ⚠️ IMPORTANT: Build-First Workflow

**ALWAYS run build after making code changes to catch compilation errors early:**

```bash
# After ANY code change:
go run ./cmd/tasks build    # or: go build ./...

# Only after build passes:
go run ./cmd/tasks health   # Verify server is up
go run ./cmd/tasks test:e2e # Run tests
```

This catches issues like:

- Missing imports
- Type mismatches
- Wrong function signatures
- Unused variables

Build takes ~2 seconds vs tests taking 30+ seconds. Always build first!

## Project Structure

```
apps/server/
├── cmd/
│   ├── server/           # Main server entry point
│   │   └── main.go       # fx.New() composition root
│   ├── migrate/          # Migration CLI tool
│   └── tasks/            # Development tasks CLI (health, test, build)
├── domain/               # Business logic modules — one package per domain (48)
│   ├── agentcompat/
│   ├── agents/
│   ├── apitoken/
│   ├── authinfo/
│   ├── autoprovision/
│   ├── backups/
│   ├── blueprints/
│   ├── branches/
│   ├── chat/
│   ├── chunking/
│   ├── chunks/
│   ├── devtools/
│   ├── discoveryjobs/
│   ├── docs/
│   ├── documents/
│   ├── email/
│   ├── embeddingpolicies/
│   ├── events/
│   ├── extraction/
│   ├── graph/
│   ├── health/
│   ├── invites/
│   ├── journal/
│   ├── mcp/
│   ├── mcpregistry/
│   ├── mcprelay/
│   ├── modelconfig/
│   ├── monitoring/
│   ├── notifications/
│   ├── orgs/
│   ├── projects/
│   ├── provider/
│   ├── sandbox/
│   ├── sandboximages/
│   ├── scheduler/
│   ├── schemaregistry/
│   ├── schemas/
│   ├── search/
│   ├── sessiontodos/
│   ├── skills/
│   ├── standalone/
│   ├── superadmin/
│   ├── tasks/
│   ├── tracing/
│   ├── useraccess/
│   ├── useractivity/
│   ├── userprofile/
│   └── users/
├── internal/             # Private packages
│   ├── config/           # Environment configuration
│   ├── database/         # Bun + pgx database setup
│   ├── jobs/             # Job queue base patterns
│   ├── migrate/          # Goose migration API
│   ├── server/           # Echo HTTP server setup
│   ├── storage/          # MinIO/S3 storage client
│   ├── testutil/         # Test DB/server/token helpers
│   └── version/          # Build version info
├── migrations/           # Goose SQL migrations
├── pkg/                  # Public packages
│   ├── acpslug/
│   ├── adk/              # Google ADK-Go agents (extraction)
│   ├── apperror/         # Application error types
│   ├── auth/             # Authentication (JWT/API-token middleware)
│   ├── crypto/
│   ├── embeddings/       # Vertex AI embeddings
│   ├── encryption/
│   ├── httputil/
│   ├── kreuzberg/        # Document parsing client
│   ├── llm/
│   ├── logger/           # Structured logging
│   ├── mathutil/
│   ├── pgutils/
│   ├── sdk/
│   ├── sse/
│   ├── syshealth/
│   ├── textsplitter/
│   ├── tracing/
│   └── whisper/
└── tests/
    └── integration/      # Service + DB integration tests (17 files, in-process)
```

## Key Patterns

### 1. fx Module Pattern

Every domain module follows this structure:

```go
// domain/example/module.go
package example

import "go.uber.org/fx"

var Module = fx.Module("example",
    fx.Provide(NewStore),     // Data access (repository)
    fx.Provide(NewService),   // Business logic
    fx.Provide(NewHandler),   // HTTP handlers
    fx.Invoke(RegisterRoutes), // Route registration
)
```

**Dependency flow**: Store → Service → Handler → Routes

**Adding a new module**:

1. Create `domain/<name>/` directory
2. Create `entity.go` (Bun model)
3. Create `store.go` (data access)
4. Create `service.go` (business logic)
5. Create `handler.go` (HTTP handlers)
6. Create `routes.go` (route registration)
7. Create `module.go` (fx wiring)
8. Add module to `cmd/server/main.go`

### 2. Entity/Model Pattern (Bun ORM)

```go
// domain/documents/entity.go
package documents

import (
    "time"
    "github.com/uptrace/bun"
)

type Document struct {
    bun.BaseModel `bun:"table:kb.documents,alias:d"`  // Schema-qualified!

    ID          string     `bun:"id,pk"`
    ProjectID   string     `bun:"project_id"`
    Title       string     `bun:"title"`
    Content     string     `bun:"content"`
    CreatedAt   time.Time  `bun:"created_at"`
    UpdatedAt   time.Time  `bun:"updated_at"`
}
```

**Important**: Always use schema-qualified table names (`kb.documents`, `core.user_profiles`).

### 3. Store Pattern (Repository)

```go
// domain/documents/store.go
type Store struct {
    db  *bun.DB
    log *slog.Logger
}

func NewStore(db *bun.DB, log *slog.Logger) *Store {
    return &Store{db: db, log: log.With(logger.Scope("documents.store"))}
}

func (s *Store) GetByID(ctx context.Context, projectID, id string) (*Document, error) {
    var doc Document
    err := s.db.NewSelect().
        Model(&doc).
        Where("project_id = ?", projectID).
        Where("id = ?", id).
        Scan(ctx)
    if err != nil {
        if err == sql.ErrNoRows {
            return nil, apperror.ErrNotFound  // Use app errors!
        }
        return nil, apperror.ErrDatabase.WithInternal(err)
    }
    return &doc, nil
}
```

### 4. Handler Pattern

```go
// domain/documents/handler.go
type Handler struct {
    svc *Service
}

func NewHandler(svc *Service) *Handler {
    return &Handler{svc: svc}
}

func (h *Handler) GetDocument(c echo.Context) error {
    user := auth.GetUser(c)  // Get authenticated user from context
    id := c.Param("id")

    doc, err := h.svc.GetByID(c.Request().Context(), user.ProjectID, id)
    if err != nil {
        return err  // apperror.Error types are handled automatically
    }
    return c.JSON(http.StatusOK, doc)
}
```

### 5. Route Registration Pattern

```go
// domain/documents/routes.go
func RegisterRoutes(e *echo.Echo, h *Handler, authMw *auth.Middleware) {
    g := e.Group("/api/documents")
    g.Use(authMw.RequireAuth())
    g.Use(authMw.RequireScopes("documents:read"))
    g.Use(authMw.RequireProjectID())

    g.GET("", h.ListDocuments)
    g.GET("/:id", h.GetDocument)
    g.POST("", h.CreateDocument)
    g.PATCH("/:id", h.UpdateDocument)
    g.DELETE("/:id", h.DeleteDocument)
}
```

### 6. Error Handling Pattern

Use `pkg/apperror` for all application errors:

```go
import "github.com/emergent-company/emergent.memory/pkg/apperror"

// Predefined errors
return apperror.ErrNotFound                              // 404
return apperror.ErrBadRequest                            // 400
return apperror.ErrUnauthorized                          // 401
return apperror.ErrForbidden                             // 403

// With context
return apperror.ErrNotFound.WithMessage("Document not found")
return apperror.ErrDatabase.WithInternal(err)

// Custom error
return apperror.New(http.StatusConflict, "duplicate_key", "Resource already exists")
```

The `apperror.HTTPErrorHandler` automatically converts these to JSON:

```json
{
  "error": {
    "code": "not_found",
    "message": "Document not found"
  }
}
```

### 7. Authentication Flow

```go
// In handler - get authenticated user
user := auth.GetUser(c)
user.ID         // User UUID
user.Sub        // Zitadel subject
user.Email      // User email
user.Scopes     // []string of granted scopes
user.ProjectID  // From X-Project-ID header
user.OrgID      // From user profile
```

**Middleware chain**:

1. `RequireAuth()` - Validates token (JWT or API token)
2. `RequireScopes("scope1", "scope2")` - Checks user has required scopes
3. `RequireProjectID()` - Ensures X-Project-ID header is present

### 8. Job Queue Pattern

Background jobs use PostgreSQL-backed queues with `FOR UPDATE SKIP LOCKED`:

```go
// domain/extraction/jobs_service.go
type JobsService struct {
    db  *bun.DB
    log *slog.Logger
}

func (s *JobsService) Dequeue(ctx context.Context) (*Job, error) {
    var job Job
    err := s.db.NewRaw(`
        SELECT * FROM kb.chunk_embedding_jobs
        WHERE status = 'pending'
        ORDER BY created_at
        LIMIT 1
        FOR UPDATE SKIP LOCKED
    `).Scan(ctx, &job)
    // ...
}
```

**Worker pattern**:

```go
// domain/extraction/worker.go
type Worker struct {
    jobs    *JobsService
    service *Service
    log     *slog.Logger
}

func (w *Worker) Start(ctx context.Context) error {
    ticker := time.NewTicker(5 * time.Second)
    for {
        select {
        case <-ctx.Done():
            return nil
        case <-ticker.C:
            w.processJobs(ctx)
        }
    }
}
```

### 9. ADK-Go Extraction Pattern

Object extraction uses Google's Agent Development Kit:

```go
// pkg/adk/entity_extractor.go
func NewEntityExtractorAgent(model genai.Model) *adk.LLMAgent {
    return adk.NewLLMAgent(
        "entity_extractor",
        model,
        adk.WithSystemPrompt(entityExtractionPrompt),
        adk.WithOutputSchema(EntityExtractionOutput{}),
    )
}

// Compose with SequentialAgent
pipeline := adk.NewSequentialAgent(
    "extraction_pipeline",
    entityExtractor,
    relationshipBuilder,
    qualityChecker,
)
```

## Testing

### Test Structure

Tests are organized into two categories:

| Location                              | Module                                | Can Run Against External Server |
| ------------------------------------- | ------------------------------------- | ------------------------------- |
| `e2e/tests-api/`                      | `github.com/emergent/api-tests`       | ✅ Yes                          |
| `apps/server/tests/integration/`      | `github.com/emergent-company/emergent.memory` | ❌ No (always in-process) |

**API e2e tests** live in the separate `e2e/tests-api/` Go module and validate HTTP API behavior through the full request/response cycle against a running server. Run them via the root `task test:e2e` (or directly: `cd e2e/tests-api && GOWORK=off go test ./...`). CLI e2e tests live in the `e2e/` module and are driven by `runlog test`.

**Integration tests** live in `apps/server/tests/integration/` (17 files) and test internal services that require direct database access (job queues, workers, schedulers). These always run in-process with a test database.

### Recommended Workflow

```bash
# 1. Build first - catch compile errors (~2s)
cd apps/server
go run ./cmd/tasks build

# 2. Check server health
go run ./cmd/tasks health

# 3. Run the API e2e suites for your change (root Taskfile)
task test:e2e -- -run TestYourSuite
```

### Running Tests (Root Task Commands)

```bash
# API e2e suites (e2e/tests-api, separate Go module)
task test:e2e                                         # All API e2e suites
task test:e2e -- -run TestDocumentsSuite              # Specific suite
task test:e2e -- -v                                   # Verbose

# Backend integration tests (apps/server/tests/integration)
task test:integration                                 # All integration tests

# Unit tests
cd apps/server && go run ./cmd/tasks test:unit
```

`task test:e2e` runs `cd e2e/tests-api && GOWORK=off go test ./... -v -count=1`.

**IMPORTANT for AI Agents:**

- Prefer `task test:e2e` / `task test:integration` over ad-hoc `go test` invocations.
- Use `-v` only when you need to see all test names.

### Test Utilities

Located in `internal/testutil/`:

- `TestDB` - Creates isolated test database with transactions
- `TestServer` - Creates Echo server with all routes registered
- `TestTokenBuilder` - Creates test JWT tokens with custom claims
- Request helpers: `WithAuth()`, `WithProjectID()`, `WithBody()`

```go
func (suite *MySuite) TestExample() {
    // Create test token
    token := testutil.NewTestTokenBuilder().
        WithUserID("user-123").
        WithScopes("documents:read").
        Build()

    // Make request
    rec := suite.server.GET("/api/documents",
        testutil.WithAuth(token),
        testutil.WithProjectID(suite.projectID),
    )

    suite.Equal(http.StatusOK, rec.Code)
}
```

### Test Structure

Tests use [testify suites](https://pkg.go.dev/github.com/stretchr/testify/suite):

```go
type DocumentsSuite struct {
    suite.Suite
    db        *testutil.TestDB
    server    *testutil.TestServer
    projectID string
}

func (suite *DocumentsSuite) SetupSuite() {
    suite.db = testutil.NewTestDB(suite.T())
    suite.server = testutil.NewTestServer(suite.db)
}

func (suite *DocumentsSuite) TearDownSuite() {
    suite.db.Close()
}

func TestDocumentsSuite(t *testing.T) {
    suite.Run(t, new(DocumentsSuite))
}
```

### API E2E Test Suites (e2e/tests-api/suites/)

HTTP API suites in the `github.com/emergent/api-tests` module that run against an external server:

| Test File           | Suite                  |
| ------------------- | ---------------------- |
| `chunks_test.go`    | `TestChunksSuite`      |
| `documents_test.go` | `TestDocumentsSuite`   |
| `graph_test.go`     | `TestGraphSuite`       |
| `health_test.go`    | `TestHealthSuite`      |
| `orgs_test.go`      | `TestOrgsSuite`        |
| `projects_test.go`  | `TestProjectsSuite`    |
| `search_test.go`    | `TestSearchSuite`      |

### Integration Test Files (tests/integration/)

Service + DB tests that always run in-process (17 files):

| Test File                          | Test / Suite                    |
| ---------------------------------- | ------------------------------- |
| `agentcompat_test.go`              | `TestAgentCompatSuite`          |
| `chunk_embedding_jobs_test.go`     | `TestChunkEmbeddingJobsSuite`   |
| `chunk_embedding_worker_test.go`   | `TestChunkEmbeddingWorkerSuite` |
| `compiled_types_ui_test.go`        | `TestCompiledTypesUISuite`      |
| `discoveryjobs_test.go`            | `TestDiscoveryJobsSuite`        |
| `document_parsing_jobs_test.go`    | `TestDocumentParsingJobsSuite`  |
| `extraction_fixed_schema_test.go`  | `TestExtractionFixedSchema`     |
| `forget_agent_test.go`             | `TestForgetAgentSuite`          |
| `graph_embedding_jobs_test.go`     | `TestGraphEmbeddingJobsSuite`   |
| `graph_embedding_worker_test.go`   | `TestGraphEmbeddingWorkerSuite` |
| `merge_enrichment_e2e_test.go`     | `TestMergeEnrichmentE2E`        |
| `merge_policy_test.go`             | `TestMergePolicySuite`          |
| `object_extraction_jobs_test.go`   | `TestObjectExtractionJobsSuite` |
| `personal_kb_agent_test.go`        | `TestPersonalKBAgentSuite`      |
| `remember_agent_test.go`           | `TestRememberAgentSuite`        |
| `remember_experiments_test.go`     | `TestRememberExperiments`       |
| `scheduler_test.go`                | `TestSchedulerSuite`            |

### Running Specific Suites

```bash
# API e2e suites (root Taskfile -> e2e/tests-api)
task test:e2e -- -run TestDocumentsSuite
task test:e2e -- -run "TestDocumentsSuite/TestCreateDocument_Success"
task test:e2e -- -run "Test(Chunks|Documents|Search)"

# Integration tests (apps/server)
cd apps/server
POSTGRES_PASSWORD=emergent-dev-password go test ./tests/integration/... -v -run TestSchedulerSuite
```

## Database Migrations

Migrations are managed by [Goose](https://github.com/pressly/goose):

```bash
# Check migration status
go run ./cmd/migrate -c status

# Run pending migrations
go run ./cmd/migrate -c up

# Rollback last migration
go run ./cmd/migrate -c down

# Create new migration
go run ./cmd/migrate -c create add_new_table
```

See `migrations/README.md` for detailed workflow.

## Implementation Status

### All Modules Complete

| Domain              | Endpoints                                    | Tests |
| ------------------- | -------------------------------------------- | ----- |
| Health              | `/health`, `/healthz`, `/ready`, `/debug`    | Pass  |
| Organizations       | CRUD `/api/orgs`                          | Pass  |
| Projects            | CRUD `/api/projects`                         | Pass  |
| Users               | Search `/api/users`                       | Pass  |
| User Profile        | Get/Update `/api/user-profile`            | Pass  |
| API Tokens          | CRUD `/api/api-tokens`                    | Pass  |
| Documents           | CRUD + upload `/api/documents`            | Pass  |
| Chunks              | List/Get `/api/chunks`                    | Pass  |
| Graph Objects       | CRUD + versioning `/api/graph/objects`    | Pass  |
| Graph Relationships | CRUD `/api/graph/relationships`           | Pass  |
| Graph Search        | FTS + vector + hybrid `/api/graph/search` | Pass  |
| Unified Search      | `/api/search` (with `?debug=true`)        | Pass  |
| Chat                | CRUD + SSE streaming `/api/chat`          | Pass  |
| MCP                 | Tools `/api/mcp`                          | Pass  |
| Extraction          | Background workers                           | Pass  |
| Email               | Background workers                           | Pass  |
| Scheduler           | Cron tasks                                   | Pass  |
| Superadmin          | User/Org/Project/Job management              | Pass  |
| Agents              | Reaction agent management                    | Pass  |

There is no longer a server-local e2e suite or a single "total passing" figure. API-level coverage is provided by the 7 suites in `e2e/tests-api/` (run via `task test:e2e`) and in-process service/DB coverage by the 17 files in `apps/server/tests/integration/` (run via `task test:integration`).

### Superadmin API

The superadmin module (`domain/superadmin/`) provides administrative endpoints for platform-wide management. Requires superadmin role.

**Endpoints:**

| Method | Endpoint                                | Description            |
| ------ | --------------------------------------- | ---------------------- |
| GET    | `/api/superadmin/users`                 | List all users         |
| GET    | `/api/superadmin/users/:id`             | Get user details       |
| PATCH  | `/api/superadmin/users/:id`             | Update user            |
| GET    | `/api/superadmin/organizations`         | List all organizations |
| GET    | `/api/superadmin/organizations/:id`     | Get organization       |
| PATCH  | `/api/superadmin/organizations/:id`     | Update organization    |
| GET    | `/api/superadmin/projects`              | List all projects      |
| GET    | `/api/superadmin/projects/:id`          | Get project details    |
| GET    | `/api/superadmin/email-jobs`            | List email jobs        |
| POST   | `/api/superadmin/email-jobs/:id/retry`  | Retry failed email job |
| GET    | `/api/superadmin/embedding-jobs`        | List embedding jobs    |
| GET    | `/api/superadmin/extraction-jobs`       | List extraction jobs   |
| GET    | `/api/superadmin/document-parsing-jobs` | List parsing jobs      |
| GET    | `/api/superadmin/sync-jobs`             | List sync jobs         |

### Agents API

The agents module (`domain/agents/`) provides endpoints for managing reaction agents. Uses admin auth middleware.

**Endpoints:**

| Method | Endpoint                               | Description        |
| ------ | -------------------------------------- | ------------------ |
| GET    | `/api/admin/agents`                    | List agents        |
| GET    | `/api/admin/agents/:id`                | Get agent          |
| GET    | `/api/admin/agents/:id/runs`           | Get agent runs     |
| GET    | `/api/admin/agents/:id/pending-events` | Get pending events |
| POST   | `/api/admin/agents`                    | Create agent       |
| PATCH  | `/api/admin/agents/:id`                | Update agent       |
| DELETE | `/api/admin/agents/:id`                | Delete agent       |
| POST   | `/api/admin/agents/:id/trigger`        | Trigger agent      |
| POST   | `/api/admin/agents/:id/batch-trigger`  | Batch trigger      |

**Agent Safeguards (queue explosion prevention):**

The agents module includes safeguards to prevent runaway execution, infinite retry loops, and budget overruns. All limits are configurable via environment variables with safe defaults.

| Env Var | Default | Description |
| ------- | ------- | ----------- |
| `AGENT_MAX_PENDING_JOBS` | `10` | Max pending jobs per agent before new runs are rejected (HTTP 429) |
| `AGENT_CONSECUTIVE_FAILURE_THRESHOLD` | `5` | Auto-disable agent after N consecutive failures |
| `AGENT_MIN_CRON_INTERVAL_MINUTES` | `15` | Minimum cron schedule interval; shorter schedules return HTTP 400 on create/update |
| `BUDGET_ENFORCEMENT_ENABLED` | `true` | Block agent runs when project monthly budget is exceeded (HTTP 402) |
| `AGENT_EXECUTION_ENABLED` | `true` | Emergency kill switch — set to `false` to halt all agent execution |

**OpenAI-Compatible Fallbacks (Standalone/Dev):**

These variables provide a fallback for standalone or development use when no database provider is configured. They are alternatives to `GOOGLE_API_KEY` or `VERTEX_*`.

| Env Var | Description |
| ------- | ----------- |
| `OPENAI_BASE_URL` | OpenAI-compatible endpoint base URL (e.g., http://localhost:11434/v1 for Ollama, http://localhost:8000/v1 for vLLM, http://localhost:8080/v1 for llama.cpp) |
| `OPENAI_API_KEY` | API key for OpenAI-compatible server (optional — leave empty for keyless local servers) |
| `LLM_MODEL` | Model name to use with OpenAI-compatible provider (e.g., llama3, mistral, codellama) |

**Error codes returned by the agents API:**

| HTTP Status | Error Code | Cause |
| ----------- | ---------- | ----- |
| 400 | `bad_request` | Cron schedule interval below minimum (< 15 min by default) |
| 402 | `budget_exceeded` | Project monthly budget has been reached |
| 429 | `queue_full` | Agent already has `AGENT_MAX_PENDING_JOBS` pending jobs |

**Auto-disable on consecutive failures:**

When an agent fails `AGENT_CONSECUTIVE_FAILURE_THRESHOLD` times in a row (or encounters a `RESOURCE_EXHAUSTED` / spending cap error), it is automatically disabled and its cron trigger is removed. Re-enable via `PATCH /api/admin/agents/:id` with `{"enabled": true}`.

**Cron duplicate prevention:**

Before each cron-triggered execution, the system checks pending job depth. If the agent already has pending jobs at or above `AGENT_MAX_PENDING_JOBS`, the cron tick is skipped silently (logged at debug level). This prevents a slow-running agent from accumulating an unbounded backlog.

### Search Debug Mode

The unified search endpoint supports a debug mode that returns timing metrics and match counts:

```bash
# Via query parameter
POST /api/search/unified?debug=true

# Via request body
POST /api/search/unified
{
  "query": "search term",
  "includeDebug": true
}
```

**Requires scope:** `search:debug`

**Debug response includes:**

```json
{
  "results": [...],
  "debug": {
    "fts_time_ms": 12,
    "vector_time_ms": 45,
    "fusion_time_ms": 3,
    "total_time_ms": 60,
    "fts_matches": 15,
    "vector_matches": 20
  }
}
```

## Architecture Decisions

### Why fx?

- Explicit dependency injection without reflection magic
- Lifecycle management (OnStart/OnStop hooks)
- Module composition for clean separation
- Easy testing with dependency overrides

### Why Bun?

- Direct SQL with type safety (vs full ORM abstraction)
- Built on pgx (fastest PostgreSQL driver for Go)
- Good pgvector support for embeddings
- Schema-qualified table names work out of the box

### Why Echo?

- High performance, minimal allocation
- Good middleware ecosystem
- Clean API similar to Express/Koa
- Easy to test

### Why ADK-Go?

- Native Go LLM orchestration (no Python sidecar)
- `SequentialAgent` for multi-step pipelines
- `LoopAgent` for retry logic
- `OutputSchema` for structured JSON extraction

### Hash Algorithms

- **SHA-256** for API token lookup - fits `varchar(64)` column in database
- **SHA-512** for introspection cache keys - better collision resistance for arbitrary OAuth tokens

## Debugging

### Logging

```go
log := log.With(logger.Scope("myservice"))
log.Info("operation completed", slog.String("id", id))
log.Error("operation failed", logger.Error(err))
```

Log levels controlled by `LOG_LEVEL` env var.

### Common Issues

1. **"relation does not exist"** - Use schema-qualified table names (`kb.documents` not `documents`)
2. **500 instead of 4xx** - Ensure returning `*apperror.Error` types, not `errors.New()`
3. **Auth failing** - Check `DISABLE_ZITADEL_INTROSPECTION=true` for local testing
4. **pgvector errors** - Ensure using custom `database.Vector` type for embedding columns

## References

- [Bun documentation](https://bun.uptrace.dev/)
- [Echo documentation](https://echo.labstack.com/)
- [fx documentation](https://uber-go.github.io/fx/)
- [Goose documentation](https://pressly.github.io/goose/)
- [Google ADK-Go](https://github.com/google/adk-go)
- [Retrospective](./RETROSPECTIVE.md)
- [Benchmark Results](./BENCHMARK_RESULTS.md)

## Graph Analytics API

The graph module includes analytics endpoints for tracking object access patterns. These endpoints leverage the `last_accessed_at` column added in migration `00009_add_access_tracking.sql`.

### Access Tracking

Graph search operations automatically update `last_accessed_at` timestamps asynchronously (non-blocking) for all returned objects. This enables:

- **Garbage collection** - Identify unused objects for cleanup
- **Usage analytics** - "What's trending in our org?"
- **Future ranking boost** - Social proof algorithm (frequency-based scoring)

### Analytics Endpoints

#### GET /api/graph/analytics/most-accessed

Returns objects sorted by most recent access time.

**Query Parameters:**

- `limit` (int, optional) - Max results, default: 50, max: 200
- `min_access_count` (int, optional) - Minimum access count filter (not yet implemented, default: 1)
- `X-Project-ID` (header, required) - Project context

**Response:**

```json
{
  "items": [
    {
      "id": "uuid",
      "canonical_id": "uuid",
      "type": "Requirement",
      "key": "REQ-001",
      "properties": {...},
      "labels": ["mvp", "backend"],
      "last_accessed_at": "2025-02-11T08:30:00Z",
      "created_at": "2025-01-15T10:00:00Z"
    }
  ],
  "total": 42,
  "meta": {
    "limit": 50,
    "minAccessCount": 1
  }
}
```

#### GET /api/graph/analytics/unused

Returns objects not accessed within specified days threshold.

**Query Parameters:**

- `limit` (int, optional) - Max results, default: 50, max: 200
- `days` (int, optional) - Days threshold for "unused", default: 30
- `X-Project-ID` (header, required) - Project context

**Response:**

```json
{
  "items": [
    {
      "id": "uuid",
      "canonical_id": "uuid",
      "type": "Feature",
      "properties": {...},
      "labels": ["archived"],
      "last_accessed_at": "2024-12-01T15:45:00Z",
      "days_since_access": 72,
      "created_at": "2024-11-20T09:00:00Z"
    }
  ],
  "total": 15,
  "meta": {
    "limit": 50,
    "daysThreshold": 30
  }
}
```

**Notes:**

- Only returns HEAD versions (`supersedes_id IS NULL`)
- Excludes soft-deleted objects
- Objects with `last_accessed_at = NULL` are never accessed (included in unused results)
- Timestamps updated via async goroutines (no search latency impact)

### Implementation Details

**Repository Methods:**

- `UpdateAccessTimestamps(ctx, objectIDs []uuid.UUID)` - Batch UPDATE single query
- `GetMostAccessed(ctx, projectID, limit, minAccessCount)` - ORDER BY last_accessed_at DESC
- `GetUnused(ctx, projectID, limit, daysThreshold)` - WHERE last_accessed_at IS NULL OR < cutoff

**Service Layer:**

- `GetMostAccessed()` - Returns `MostAccessedResponse` with metadata
- `GetUnused()` - Returns `UnusedObjectsResponse` with `days_since_access` calculated

**Search Integration:**

- `VectorSearch()` - Updates timestamps for pgvector results
- `FTSSearch()` - Updates timestamps for full-text results
- `executeGraphSearch()` - Updates timestamps for hybrid fused results

**Performance:**

- Index: `idx_graph_objects_last_accessed` (DESC, WHERE NOT NULL)
- Single UPDATE per search (batch operation)
- ~8 bytes per object (~8MB for 1M objects)
