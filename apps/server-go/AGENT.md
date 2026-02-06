# Go Server - AI Agent Guide

## Overview

This is the Go implementation of the Emergent backend server, which has fully replaced the NestJS/TypeScript server. It uses:

- **Echo** - HTTP framework
- **Bun** - ORM with pgx driver for PostgreSQL
- **fx** - Uber's dependency injection framework
- **Zitadel** - OAuth2/OIDC authentication
- **Google ADK-Go** - AI extraction pipeline orchestration

**Status**: **Production Ready** - 609 E2E tests passing, full feature parity with NestJS.

## Quick Commands (Tasks CLI)

The `cmd/tasks` CLI provides common development commands:

```bash
cd apps/server-go

# Build - ALWAYS run after code changes to catch compile errors
go run ./cmd/tasks build           # Build all packages (fast ~2s)

# Health check - verify server is running
go run ./cmd/tasks health          # Quick check
go run ./cmd/tasks health -v       # Verbose (show response bodies)
go run ./cmd/tasks health -v -db   # Also check database connectivity

# Run tests
go run ./cmd/tasks test:e2e                         # All E2E tests against running server
go run ./cmd/tasks test:e2e -run TestDocumentsSuite # Specific test suite
go run ./cmd/tasks test:e2e -run "TestDocumentsSuite/TestCreate"  # Specific test
go run ./cmd/tasks test:unit                        # Unit tests only

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
apps/server-go/
├── cmd/
│   ├── server/           # Main server entry point
│   │   └── main.go       # fx.New() composition root
│   ├── migrate/          # Migration CLI tool
│   └── tasks/            # Development tasks CLI (health, test, build)
├── domain/               # Business logic modules (19 domains)
│   ├── agents/           # Reaction agent management
│   ├── apitoken/         # API token CRUD
│   ├── branches/         # Branch CRUD (for graph versioning)
│   ├── chat/             # Chat conversations + streaming
│   ├── chunks/           # Document chunks with embeddings
│   ├── datasource/       # External data sources (ClickUp)
│   ├── devtools/         # Development utilities
│   ├── documents/        # Document CRUD + file upload
│   ├── email/            # Email jobs + Mailgun
│   ├── embeddingpolicies/ # Embedding policy configuration
│   ├── extraction/       # Object extraction pipeline (ADK-Go)
│   ├── graph/            # Graph objects + relationships + search
│   ├── health/           # Health check endpoints
│   ├── mcp/              # Model Context Protocol endpoints
│   ├── orgs/             # Organizations CRUD
│   ├── projects/         # Projects CRUD
│   ├── scheduler/        # Cron-based scheduled tasks
│   ├── search/           # Unified search (FTS + vector)
│   ├── superadmin/       # Superadmin management API
│   ├── userprofile/      # User profile management
│   └── users/            # User search
├── internal/             # Private packages
│   ├── auth/             # Authentication middleware
│   ├── config/           # Environment configuration
│   ├── database/         # Bun + pgx database setup
│   ├── jobs/             # Job queue base patterns
│   ├── middleware/       # RLS, logging middleware
│   ├── migrate/          # Goose migration API
│   ├── server/           # Echo HTTP server setup
│   ├── storage/          # MinIO/S3 storage client
│   └── testutil/         # E2E test utilities
├── migrations/           # Goose SQL migrations
├── pkg/                  # Public packages
│   ├── adk/              # Google ADK-Go agents (extraction)
│   ├── apperror/         # Application error types
│   ├── clickup/          # ClickUp API client
│   ├── embeddings/       # Vertex AI embeddings
│   ├── kreuzberg/        # Document parsing client
│   ├── logger/           # Structured logging
│   └── mailgun/          # Mailgun email client
└── tests/
    ├── e2e/              # End-to-end HTTP API tests (23 suites)
    └── integration/      # Service + DB integration tests (8 suites)
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
    g := e.Group("/api/v2/documents")
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
import "github.com/anomalyco/emergent/apps/server-go/pkg/apperror"

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

| Directory            | Purpose                       | Can Run Against External Server |
| -------------------- | ----------------------------- | ------------------------------- |
| `tests/e2e/`         | HTTP API tests (23 suites)    | ✅ Yes                          |
| `tests/integration/` | Service + DB tests (8 suites) | ❌ No (always in-process)       |

**E2E tests** validate HTTP API behavior through the full request/response cycle. They can run against either an in-process test server or an external running server.

**Integration tests** test internal services that require direct database access (job queues, workers, schedulers). These always run in-process with a test database.

### Recommended Workflow

```bash
cd apps/server-go

# 1. Build first - catch compile errors (~2s)
go run ./cmd/tasks build

# 2. Check server health
go run ./cmd/tasks health

# 3. Run tests
go run ./cmd/tasks test:e2e -run TestYourSuite
```

### Running Tests (Tasks CLI)

The easiest way to run tests is using the tasks CLI:

```bash
cd apps/server-go

# Run E2E tests against running server (recommended)
go run ./cmd/tasks test:e2e                              # All E2E tests
go run ./cmd/tasks test:e2e -run TestDocumentsSuite      # Specific suite
go run ./cmd/tasks test:e2e -run "TestGraphObjectsSuite/TestCreate"  # Specific test

# Run unit tests only
go run ./cmd/tasks test:unit
```

### Running Tests via nx (Recommended)

Always use nx targets for running tests - they handle environment setup and provide summary statistics:

```bash
# E2E tests - shows summary only (fast to read)
nx run server-go:test-e2e                      # All E2E tests
nx run server-go:test-e2e --args="GraphSuite"  # Specific suite

# E2E tests - verbose output (shows all test names)
nx run server-go:test-e2e --args.v=true        # Verbose mode

# Integration tests
nx run server-go:test-integration              # All integration tests
```

**Default output (summary only - shows failing tests if any):**

```
Running E2E tests...
========================================
========================================
TEST SUMMARY
========================================
Passed:   609
Failed:   0
Skipped:  9
Total:    618
Duration: 57.763s

RESULT: PASSED
```

**On failure, automatically shows failed test details without -v flag.**

### Running Tests (Manual)

Only use manual commands if you need specific go test flags:

```bash
cd apps/server-go

# Build first!
go build ./...

# E2E tests via script (includes summary)
./scripts/run-e2e-tests.sh                   # Summary only
./scripts/run-e2e-tests.sh -v                # Verbose
./scripts/run-e2e-tests.sh TestDocumentsSuite # Specific suite
./scripts/run-e2e-tests.sh -v TestDocumentsSuite # Verbose + specific
```

**IMPORTANT for AI Agents:**

- Never run tests twice to get counts - use nx or the script
- Default mode shows summary + failing tests only (efficient)
- Use `-v` only when you need to see all test names

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
    rec := suite.server.GET("/api/v2/documents",
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

### E2E Test Files (tests/e2e/)

HTTP API tests that can run against an external server:

| Test File                    | Suite                    | Coverage                                             |
| ---------------------------- | ------------------------ | ---------------------------------------------------- |
| `auth_test.go`               | `AuthSuite`              | JWT/API token validation, auth error responses       |
| `security_scopes_test.go`    | `SecurityScopesSuite`    | Scope enforcement for all endpoints                  |
| `tenant_isolation_test.go`   | `TenantIsolationSuite`   | RLS, cross-project isolation, header validation      |
| `documents_test.go`          | `DocumentsSuite`         | CRUD, pagination, deduplication                      |
| `documents_upload_test.go`   | `DocumentsUploadSuite`   | File upload auth/scope/project validation            |
| `chunks_test.go`             | `ChunksSuite`            | Chunk listing, pagination, bulk delete               |
| `graph_test.go`              | `GraphObjectsSuite`      | Objects, relationships, history, soft delete, search |
| `graph_search_test.go`       | `GraphSearchSuite`       | Graph search with debug mode                         |
| `search_test.go`             | `SearchSuite`            | Unified search, fusion strategies, debug mode        |
| `chat_test.go`               | `ChatSuite`              | Conversations, SSE streaming, CRUD                   |
| `mcp_test.go`                | `MCPSuite`               | MCP RPC/SSE authentication                           |
| `orgs_test.go`               | `OrgsSuite`              | Organization CRUD, cascade delete                    |
| `projects_test.go`           | `ProjectsSuite`          | Project CRUD, members, cascade delete                |
| `users_test.go`              | `UsersSuite`             | User search                                          |
| `userprofile_test.go`        | `UserProfileSuite`       | Profile get/update                                   |
| `useraccess_test.go`         | `UserAccessSuite`        | Access tree                                          |
| `apitoken_test.go`           | `APITokenSuite`          | Token CRUD                                           |
| `health_test.go`             | `HealthSuite`            | Health/ready/debug endpoints                         |
| `invites_test.go`            | `InvitesSuite`           | Invite CRUD                                          |
| `events_test.go`             | `EventsSuite`            | Event listing                                        |
| `email_jobs_test.go`         | `EmailJobsSuite`         | Email queue (HTTP endpoints)                         |
| `embedding_policies_test.go` | `EmbeddingPoliciesSuite` | Embedding policy CRUD                                |
| `branches_test.go`           | `BranchesSuite`          | Branch CRUD for graph versioning                     |
| `superadmin_test.go`         | `SuperadminSuite`        | Superadmin user/org/project/job management           |
| `templatepacks_test.go`      | `TemplatePacksSuite`     | Template pack management                             |
| `tasks_test.go`              | `TasksSuite`             | Task management                                      |
| `notifications_test.go`      | `NotificationsSuite`     | Notification management                              |
| `useractivity_test.go`       | `UserActivitySuite`      | User activity tracking                               |

### Integration Test Files (tests/integration/)

Service + DB tests that always run in-process:

| Test File                        | Suite                       | Coverage                           |
| -------------------------------- | --------------------------- | ---------------------------------- |
| `scheduler_test.go`              | `SchedulerSuite`            | Cron task execution, cleanup tasks |
| `datasource_deadletter_test.go`  | `DatasourceDeadletterSuite` | Dead letter handling               |
| `document_parsing_jobs_test.go`  | `DocumentParsingJobsSuite`  | Document parsing job queue         |
| `chunk_embedding_jobs_test.go`   | `ChunkEmbeddingJobsSuite`   | Embedding job queue                |
| `chunk_embedding_worker_test.go` | `ChunkEmbeddingWorkerSuite` | Embedding worker processing        |
| `graph_embedding_jobs_test.go`   | `GraphEmbeddingJobsSuite`   | Graph embedding job queue          |
| `graph_embedding_worker_test.go` | `GraphEmbeddingWorkerSuite` | Graph embedding worker             |
| `object_extraction_jobs_test.go` | `ObjectExtractionJobsSuite` | Object extraction job queue        |

### Running Specific Suites

```bash
# E2E tests (using tasks CLI)
go run ./cmd/tasks test:e2e -run TestDocumentsSuite
go run ./cmd/tasks test:e2e -run "TestDocumentsSuite/TestCreateDocument_Success"
go run ./cmd/tasks test:e2e -run "Test(Auth|Security|Tenant)"

# Integration tests (manual)
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
go run ./cmd/migrate -c create add_new_table sql
```

See `migrations/README.md` for detailed workflow.

## Implementation Status

### All Modules Complete

| Domain              | Endpoints                                    | Tests |
| ------------------- | -------------------------------------------- | ----- |
| Health              | `/health`, `/healthz`, `/ready`, `/debug`    | Pass  |
| Organizations       | CRUD `/api/v2/orgs`                          | Pass  |
| Projects            | CRUD `/api/v2/projects`                      | Pass  |
| Users               | Search `/api/v2/users`                       | Pass  |
| User Profile        | Get/Update `/api/v2/user-profile`            | Pass  |
| API Tokens          | CRUD `/api/v2/api-tokens`                    | Pass  |
| Documents           | CRUD + upload `/api/v2/documents`            | Pass  |
| Chunks              | List/Get `/api/v2/chunks`                    | Pass  |
| Graph Objects       | CRUD + versioning `/api/v2/graph/objects`    | Pass  |
| Graph Relationships | CRUD `/api/v2/graph/relationships`           | Pass  |
| Graph Search        | FTS + vector + hybrid `/api/v2/graph/search` | Pass  |
| Unified Search      | `/api/v2/search` (with `?debug=true`)        | Pass  |
| Chat                | CRUD + SSE streaming `/api/v2/chat`          | Pass  |
| MCP                 | Tools `/api/v2/mcp`                          | Pass  |
| Extraction          | Background workers                           | Pass  |
| Email               | Background workers                           | Pass  |
| Data Sources        | ClickUp sync                                 | Pass  |
| Scheduler           | Cron tasks                                   | Pass  |
| Superadmin          | User/Org/Project/Job management              | Pass  |
| Agents              | Reaction agent management                    | Pass  |

**Total: 609 E2E tests passing**

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

### Search Debug Mode

The unified search endpoint supports a debug mode that returns timing metrics and match counts:

```bash
# Via query parameter
POST /api/v2/search/unified?debug=true

# Via request body
POST /api/v2/search/unified
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
- [Migration spec](../../openspec/changes/port-server-to-golang/design.md)
- [Retrospective](./RETROSPECTIVE.md)
- [Benchmark Results](./BENCHMARK_RESULTS.md)
