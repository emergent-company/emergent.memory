## Context

The Emergent platform backend is a NestJS/TypeScript application (~109K lines of code, 659 files) with:

- **64 TypeORM entities** across `kb` and `core` schemas
- **129 services** handling business logic
- **53 controllers** exposing REST APIs
- **49 modules** organized by domain

Key integrations:

- PostgreSQL 16 with pgvector extension
- Zitadel (OIDC/OAuth 2.0)
- Google Gemini/Vertex AI (embeddings, chat)
- OpenTelemetry (observability)
- LangChain/LangGraph (AI orchestration)

**Stakeholders:**

- Development team (daily workflow changes)
- Operations (deployment, monitoring)
- Product (feature velocity during migration)

## Technology Stack Summary

| Component                 | Technology                                                            | Reference   |
| ------------------------- | --------------------------------------------------------------------- | ----------- |
| **Application Framework** | [Uber fx](https://github.com/uber-go/fx)                              | Decision 3  |
| **Web Framework**         | [Echo](https://echo.labstack.com/)                                    | Decision 2  |
| **Database ORM**          | [Bun](https://bun.uptrace.dev/) + [pgx](https://github.com/jackc/pgx) | Decision 4  |
| **Authentication**        | [zitadel-go](https://github.com/zitadel/zitadel-go)                   | Decision 5  |
| **Migrations**            | [Goose](https://github.com/pressly/goose) (post-cutover)              | Decision 9  |
| **Background Jobs**       | [River](https://github.com/riverqueue/river)                          | Decision 10 |
| **Observability**         | [OpenTelemetry Go](https://opentelemetry.io/docs/languages/go/)       | Decision 7  |
| **Validation**            | [go-playground/validator](https://github.com/go-playground/validator) | —           |

**Reference implementation:** `/root/huma-blueprints-api/`

## Goals / Non-Goals

### Goals

- Achieve 40%+ reduction in P99 API latency
- Reduce memory footprint by 50%+
- Enable single-binary deployment with <50MB container images
- Maintain 100% API contract compatibility throughout migration
- Complete migration within 6-12 months using strangler fig pattern

### Non-Goals

- Changing API contracts or frontend behavior
- Rewriting the React frontend
- Changing database schema or structure
- Adding new features during migration (feature freeze for migrated modules)
- Migrating to microservices architecture (keep monolith)

## Decisions

### Decision 1: Project Structure

**What:** Create `apps/server-go/` as a new Go module within the Nx monorepo, following the battle-tested architecture from `huma-blueprints-api`.

**Why:**

- Allows parallel development and testing
- Both servers can run simultaneously during migration
- Nx can orchestrate builds (with custom executor)
- Architecture proven in production at `/root/huma-blueprints-api/`

**Structure:**

```
apps/server-go/
├── cmd/
│   └── server/
│       └── main.go              # Entry point, fx.New() composition
├── domain/                      # Domain modules (fx.Module per domain)
│   ├── user/
│   │   ├── module.go            # fx.Module definition
│   │   ├── handler.go           # HTTP handlers (controllers)
│   │   ├── service.go           # Business logic
│   │   ├── store.go             # Database access (repository)
│   │   ├── routes.go            # Route registration
│   │   └── dto.go               # Request/response types
│   ├── organization/
│   ├── project/
│   ├── document/
│   ├── graph/
│   └── chat/
├── internal/
│   ├── server/
│   │   └── server.go            # Echo setup, middleware chain, lifecycle
│   ├── database/
│   │   └── database.go          # Bun connection, fx lifecycle hooks
│   ├── config/
│   │   └── config.go            # Environment configuration
│   └── clients/                 # External service clients (Vertex AI, etc.)
├── pkg/
│   ├── auth/                    # Zitadel middleware
│   ├── apperror/                # Error handling
│   ├── validator/               # Custom validators
│   └── tracing/                 # OpenTelemetry setup
├── migrations/                  # Goose SQL migrations
├── go.mod
├── go.sum
├── Makefile
└── air.toml                     # Hot reload config
```

**Key patterns:**

- **fx.Module per domain**: Each domain directory has a `module.go` that exports an `fx.Module`
- **Dependency injection**: Services declare dependencies as constructor parameters; fx wires them
- **Lifecycle management**: Database, HTTP server, background workers use `fx.Lifecycle` hooks
- **Clean separation**: `pkg/` for reusable utilities, `internal/` for app-specific code

**Alternatives considered:**

- Separate repository: Rejected (harder to share types, migrations)
- Replace in-place: Rejected (no parallel testing possible)
- Flat structure: Rejected (doesn't scale with 49 modules)

### Decision 2: Web Framework

**What:** Use [Echo](https://echo.labstack.com/) for HTTP routing.

**Why:**

- Mature, production-proven framework used by many large companies
- Excellent middleware ecosystem (logging, recovery, CORS, rate limiting)
- Native OpenTelemetry integration
- Battle-tested in `huma-blueprints-api`
- High performance with minimal allocations

**Key features:**

```go
// Echo handler with middleware
func NewHandler(e *echo.Echo, svc *DocumentService, auth *AuthMiddleware) {
    g := e.Group("/api/documents")
    g.Use(auth.RequireAuth())

    g.GET("", h.List)
    g.POST("", h.Create)
    g.GET("/:id", h.Get)
    g.PUT("/:id", h.Update)
    g.DELETE("/:id", h.Delete)
}

func (h *Handler) Create(c echo.Context) error {
    var input CreateDocumentDTO
    if err := c.Bind(&input); err != nil {
        return echo.NewHTTPError(http.StatusBadRequest, err.Error())
    }
    if err := c.Validate(&input); err != nil {
        return err
    }
    doc, err := h.service.Create(c.Request().Context(), &input)
    if err != nil {
        return err
    }
    return c.JSON(http.StatusCreated, doc)
}
```

**OpenAPI generation:**

- Use [swaggo/swag](https://github.com/swaggo/swag) for OpenAPI spec generation from annotations
- Or generate OpenAPI spec from TypeScript server and validate Go implementation matches

**Alternatives considered:**

- Gin: Popular but less flexible middleware
- Chi: Lightweight but limited features
- Fiber: Fast but different API patterns, less mature ecosystem
- Standard library: Too verbose for complex routing

### Decision 3: Application Framework (Dependency Injection)

**What:** Use [Uber fx](https://github.com/uber-go/fx) as the application framework for dependency injection and service lifecycle management.

**Why:**

- Provides consistent patterns for building services at scale (used by Uber for hundreds of services)
- Automatic dependency injection — no manual wiring
- Built-in lifecycle management (OnStart/OnStop hooks) for graceful startup/shutdown
- `fx.Module` groups related components — directly maps to NestJS modules
- Establishes conventions that scale as we add more services

**NestJS to fx mapping:**

| NestJS Concept           | fx Equivalent                           |
| ------------------------ | --------------------------------------- |
| `@Module()` decorator    | `fx.Module("name", ...)`                |
| `@Injectable()` service  | `fx.Provide(NewService)`                |
| Constructor injection    | Constructor functions with typed params |
| `OnModuleInit` lifecycle | `fx.Lifecycle` with `OnStart` hook      |
| `OnModuleDestroy`        | `fx.Lifecycle` with `OnStop` hook       |
| Module imports           | `fx.Module` composition                 |

**Example domain module:**

```go
// domain/document/module.go
package document

import "go.uber.org/fx"

var Module = fx.Module("document",
    fx.Provide(
        NewStore,       // repository layer
        NewService,     // business logic
        NewHandler,     // HTTP handlers
    ),
    fx.Invoke(RegisterRoutes),
)

// domain/document/service.go
type Service struct {
    store  *Store
    logger *zap.Logger
}

func NewService(store *Store, logger *zap.Logger) *Service {
    return &Service{store: store, logger: logger}
}

func (s *Service) Create(ctx context.Context, input *CreateDTO) (*Document, error) {
    // Business logic here
}
```

**Main application composition:**

```go
// cmd/server/main.go
func main() {
    fx.New(
        // Infrastructure
        fx.Provide(
            config.New,
            logger.New,
            database.New,
        ),

        // Domain modules (like NestJS modules)
        user.Module,
        organization.Module,
        project.Module,
        document.Module,
        graph.Module,
        chat.Module,

        // HTTP server with lifecycle
        fx.Provide(NewEchoServer),
        fx.Invoke(StartServer),

        // Graceful shutdown
        fx.StartTimeout(30*time.Second),
        fx.StopTimeout(30*time.Second),
    ).Run()
}
```

**Lifecycle hooks for HTTP server:**

```go
func NewEchoServer(lc fx.Lifecycle, cfg *config.Config, logger *zap.Logger) *echo.Echo {
    e := echo.New()

    lc.Append(fx.Hook{
        OnStart: func(ctx context.Context) error {
            logger.Info("Starting HTTP server", zap.String("addr", cfg.ServerAddr))
            go func() {
                if err := e.Start(cfg.ServerAddr); err != nil && err != http.ErrServerClosed {
                    logger.Error("Server error", zap.Error(err))
                }
            }()
            return nil
        },
        OnStop: func(ctx context.Context) error {
            logger.Info("Stopping HTTP server")
            return e.Shutdown(ctx)
        },
    })

    return e
}
```

**Benefits for future services:**

- New services follow the same fx.Module pattern
- Shared infrastructure modules (database, auth, observability) are reusable
- Consistent lifecycle management across all services
- Easy to test — fx supports test containers with `fxtest`

**Alternatives considered:**

- Manual wiring: Rejected — doesn't scale, error-prone
- Wire (Google): Rejected — compile-time DI adds complexity, less flexible
- Dig (Uber): Considered — fx is built on Dig, provides higher-level abstractions
- Custom DI: Rejected — reinventing the wheel

### Decision 4: Database Access

**What:** Use [Bun ORM](https://bun.uptrace.dev/) + [pgx](https://github.com/jackc/pgx) driver for database access.

**Why:**

- **Bun**: Lightweight ORM with excellent PostgreSQL support, struct mapping, migrations
- **pgx**: Fastest PostgreSQL driver with native pgvector support
- Proven in `huma-blueprints-api` with complex domain models
- More familiar ORM patterns for developers coming from TypeORM

**Bun advantages over sqlc:**

- Struct-based models (similar to TypeORM entities)
- Dynamic query building for complex filters
- Built-in relation loading (belongs-to, has-many)
- Model hooks for auditing, timestamps
- Easier migration from TypeORM mental model

**Example model:**

```go
type Document struct {
    bun.BaseModel `bun:"table:kb.documents"`

    ID          uuid.UUID  `bun:"id,pk,type:uuid,default:gen_random_uuid()"`
    Name        string     `bun:"name,notnull"`
    Content     string     `bun:"content"`
    Embedding   pgvector.Vector `bun:"embedding,type:vector(768)"`
    ProjectID   uuid.UUID  `bun:"project_id,type:uuid,notnull"`
    CreatedAt   time.Time  `bun:"created_at,nullzero,notnull,default:current_timestamp"`
    UpdatedAt   time.Time  `bun:"updated_at,nullzero,notnull,default:current_timestamp"`

    // Relations
    Project     *Project   `bun:"rel:belongs-to,join:project_id=id"`
    Chunks      []Chunk    `bun:"rel:has-many,join:id=document_id"`
}
```

**pgvector support:**

```go
// Semantic search with Bun + pgvector
var docs []Document
err := db.NewSelect().
    Model(&docs).
    Where("project_id = ?", projectID).
    OrderExpr("embedding <-> ?", queryEmbedding).
    Limit(10).
    Scan(ctx)
```

**Alternatives considered:**

- sqlc: Rejected — requires writing SQL manually, less familiar to TypeORM developers
- GORM: Rejected — heavier, more magic, worse performance
- Ent: Rejected — code generation complexity
- Raw SQL only: Rejected — no type safety, verbose

### Decision 5: Authentication

**What:** Use [zitadel-go](https://github.com/zitadel/zitadel-go) SDK for authentication and authorization.

**Why:**

- Official Zitadel SDK with first-class support
- Handles JWT validation, JWKS rotation, and token introspection
- Provides middleware for Echo and other frameworks
- Battle-tested in `huma-blueprints-api`
- Better Zitadel-specific features than generic go-oidc

**Implementation:**

```go
// pkg/auth/middleware.go
package auth

import (
    "github.com/labstack/echo/v4"
    "github.com/zitadel/zitadel-go/v3/pkg/authorization"
    "github.com/zitadel/zitadel-go/v3/pkg/authorization/oauth"
    "github.com/zitadel/zitadel-go/v3/pkg/zitadel"
)

type Middleware struct {
    authorizer *authorization.Authorizer[*oauth.IntrospectionContext]
}

func NewMiddleware(cfg *config.Config) (*Middleware, error) {
    z, err := zitadel.New(cfg.ZitadelDomain, zitadel.WithInsecure())
    if err != nil {
        return nil, err
    }

    authorizer, err := authorization.New(
        z,
        oauth.DefaultAuthorization(cfg.ZitadelClientID, cfg.ZitadelClientSecret),
    )
    if err != nil {
        return nil, err
    }

    return &Middleware{authorizer: authorizer}, nil
}

func (m *Middleware) RequireAuth() echo.MiddlewareFunc {
    return func(next echo.HandlerFunc) echo.HandlerFunc {
        return func(c echo.Context) error {
            token := extractBearerToken(c.Request())
            ctx, err := m.authorizer.CheckAuthorization(c.Request().Context(), token)
            if err != nil {
                return echo.ErrUnauthorized
            }
            // Store user context for handlers
            c.Set("auth", ctx)
            return next(c)
        }
    }
}

func (m *Middleware) RequireScope(scope string) echo.MiddlewareFunc {
    return func(next echo.HandlerFunc) echo.HandlerFunc {
        return func(c echo.Context) error {
            ctx := c.Get("auth").(*oauth.IntrospectionContext)
            if !ctx.HasScope(scope) {
                return echo.ErrForbidden
            }
            return next(c)
        }
    }
}
```

**fx integration:**

```go
// pkg/auth/module.go
var Module = fx.Module("auth",
    fx.Provide(NewMiddleware),
)
```

**Alternatives considered:**

- go-oidc: Generic OIDC library, missing Zitadel-specific features
- Custom JWT validation: Rejected — reinventing the wheel, security risk

### Decision 6: AI/LLM Integration Strategy

**What:** Evaluate options in Phase 0, likely hybrid approach.

**Options:**

1. **LangChainGo**: Go port of LangChain (limited features)
2. **Direct API calls**: Google Gemini/Vertex AI REST/gRPC APIs
3. **Python sidecar**: gRPC service for complex LangGraph workflows

**Recommendation:**

- Direct API calls for embeddings (simple, well-documented)
- Evaluate LangChainGo for chat (may be sufficient)
- Python sidecar only if LangGraph features are essential

**Why not pure Go:**

- LangChainGo is less mature than Python/JS versions
- Some LangGraph features have no Go equivalent
- Sidecar adds latency but preserves functionality

### Decision 7: Observability

**What:** Use [OpenTelemetry Go SDK](https://opentelemetry.io/docs/instrumentation/go/) for tracing, metrics, and logging.

**Why:**

- Native OTEL support in Go
- Compatible with existing SigNoz/Langfuse setup
- Automatic instrumentation for HTTP, database, gRPC

### Decision 8: Migration Routing (Strangler Fig)

**What:** Use Traefik path-based routing to direct traffic between NestJS and Go servers.

**Implementation:**

```yaml
# Phase 1 example: /health, /api/settings go to Go
http:
  routers:
    go-health:
      rule: 'PathPrefix(`/health`)'
      service: server-go
    go-settings:
      rule: 'PathPrefix(`/api/settings`)'
      service: server-go
    legacy-all:
      rule: 'PathPrefix(`/`)'
      service: server-nestjs
      priority: 1 # Lower priority = fallback
```

**Why:**

- Zero-downtime migration
- Easy rollback (change routing rules)
- Gradual traffic shifting possible

### Decision 9: Database Migration Strategy

**What:** TypeORM (NestJS) remains the **single source of truth** for database migrations during the transition. Go server uses [Goose](https://github.com/pressly/goose) for future migrations post-cutover.

**Why:**

- Migrations are a one-way operation — only one system should own them
- TypeORM migrations are already battle-tested in production
- Avoids drift between two migration systems
- Goose uses SQL migrations (easier to sync with TypeORM output if needed)
- Battle-tested in `huma-blueprints-api`

**Strategy:**

```
┌─────────────────────────────────────────────────────────────────┐
│                     MIGRATION OWNERSHIP                          │
├─────────────────────────────────────────────────────────────────┤
│  Phase 0-5 (Transition)     │  Phase 6+ (Post-Cutover)          │
│  ─────────────────────────  │  ──────────────────────────────── │
│  TypeORM owns migrations    │  Goose owns migrations            │
│  Go uses Bun models only    │  TypeORM retired                  │
│  Bun models updated when    │  Bun models updated when          │
│  schema changes             │  schema changes                   │
└─────────────────────────────────────────────────────────────────┘
```

**Workflow during transition (Phases 0-5):**

1. **Schema changes** are made via TypeORM migrations (NestJS)
2. **Run migration** using existing `npm run migration:run`
3. **Update Bun models** in Go server to match new schema
4. **Commit both** the TypeORM migration AND updated Bun models together

**Bun model sync:**

Unlike sqlc, Bun doesn't auto-generate from schema. Models are written manually:

```go
// Update model when TypeORM migration adds a column
type Document struct {
    bun.BaseModel `bun:"table:kb.documents"`

    ID        uuid.UUID `bun:"id,pk,type:uuid"`
    Name      string    `bun:"name,notnull"`
    NewField  string    `bun:"new_field"` // Added after migration
}
```

**Post-cutover migration (Phase 6):**

After NestJS is retired, use Goose for all new migrations:

```bash
# Create new migration
goose -dir migrations create add_feature sql

# Run migrations
goose -dir migrations postgres "$DATABASE_URL" up
```

Example Goose migration:

```sql
-- migrations/20240115120000_add_feature.sql

-- +goose Up
ALTER TABLE kb.documents ADD COLUMN feature_flag boolean DEFAULT false;

-- +goose Down
ALTER TABLE kb.documents DROP COLUMN feature_flag;
```

**Handling edge cases:**

| Scenario                   | Action                                                      |
| -------------------------- | ----------------------------------------------------------- |
| New column added (TypeORM) | Update Bun model, add field with correct tags               |
| Column renamed             | Update Bun model field name and tag                         |
| New table added            | Create new Bun model struct                                 |
| Index added                | No Go changes needed (transparent)                          |
| Breaking schema change     | Coordinate: migration + both servers updated in same deploy |

**CI/CD integration:**

```yaml
# In CI pipeline - verify Bun models compile and match schema
- name: Verify Go builds
  run: |
    cd apps/server-go
    go build ./...

- name: Run database tests
  run: |
    cd apps/server-go
    go test ./domain/... -tags=integration
```

**Alternatives considered:**

- **Goose from day 1**: Rejected — requires maintaining two migration systems simultaneously
- **golang-migrate**: Considered — Goose is simpler and used in huma-blueprints-api
- **Atlas**: Considered — declarative approach adds complexity

### Decision 10: Background Jobs

**What:** Use [River](https://github.com/riverqueue/river) for background job processing.

**Why:**

- Postgres-backed job queue — no additional infrastructure (Redis)
- Native Go library with excellent performance
- Reliable job execution with retries, scheduling, unique jobs
- Battle-tested in `huma-blueprints-api`
- Works well with fx lifecycle management

**Implementation:**

```go
// internal/jobs/module.go
package jobs

import (
    "github.com/riverqueue/river"
    "github.com/riverqueue/river/riverdriver/riverpgxv5"
    "go.uber.org/fx"
)

var Module = fx.Module("jobs",
    fx.Provide(NewRiverClient),
    fx.Provide(NewWorkers),
    fx.Invoke(StartWorkers),
)

func NewRiverClient(lc fx.Lifecycle, pool *pgxpool.Pool, workers *river.Workers) (*river.Client[pgx.Tx], error) {
    client, err := river.NewClient(riverpgxv5.New(pool), &river.Config{
        Queues: map[string]river.QueueConfig{
            river.QueueDefault:  {MaxWorkers: 10},
            "extraction":        {MaxWorkers: 5},
            "email":             {MaxWorkers: 3},
        },
        Workers: workers,
    })
    if err != nil {
        return nil, err
    }

    lc.Append(fx.Hook{
        OnStart: func(ctx context.Context) error {
            return client.Start(ctx)
        },
        OnStop: func(ctx context.Context) error {
            return client.Stop(ctx)
        },
    })

    return client, nil
}
```

**Job definition:**

```go
// domain/extraction/jobs.go
type ExtractDocumentArgs struct {
    DocumentID uuid.UUID `json:"document_id"`
}

func (ExtractDocumentArgs) Kind() string { return "extract_document" }

type ExtractDocumentWorker struct {
    service *ExtractionService
}

func (w *ExtractDocumentWorker) Work(ctx context.Context, job *river.Job[ExtractDocumentArgs]) error {
    return w.service.Extract(ctx, job.Args.DocumentID)
}
```

**Enqueuing jobs:**

```go
// In a service
func (s *DocumentService) Create(ctx context.Context, input *CreateDTO) (*Document, error) {
    doc, err := s.store.Create(ctx, input)
    if err != nil {
        return nil, err
    }

    // Enqueue extraction job
    _, err = s.riverClient.Insert(ctx, &ExtractDocumentArgs{DocumentID: doc.ID}, nil)
    return doc, err
}
```

**Alternatives considered:**

- BullMQ pattern on Postgres: Rejected — River provides this natively
- Redis-based (Asynq): Rejected — adds infrastructure dependency
- Temporal: Rejected — overkill for our use case

## Risks / Trade-offs

| Risk                                   | Likelihood | Impact | Mitigation                                                 |
| -------------------------------------- | ---------- | ------ | ---------------------------------------------------------- |
| Performance gains less than expected   | Medium     | Medium | Benchmark early in Phase 0; define go/no-go criteria       |
| LangChain feature gaps in Go           | High       | High   | Python sidecar fallback; prioritize direct API integration |
| Extended timeline (>12 months)         | Medium     | High   | Strict phase gates; MVP-first for each module              |
| Team productivity drop during learning | High       | Medium | Go training; pair programming; comprehensive examples      |
| Subtle API incompatibilities           | Medium     | High   | Contract tests run against both servers; shadow traffic    |
| TypeORM-specific features hard to port | Medium     | Medium | Document edge cases; simplify where possible               |
| Bun model drift from schema            | Low        | Medium | Integration tests verify models match database             |

## Migration Plan

### Phase 0: Foundation (2-4 weeks)

1. Set up `apps/server-go/` project structure with fx
2. Configure Echo + Bun + pgx
3. Set up zitadel-go authentication
4. Implement health/ready endpoints
5. Set up CI pipeline (build, test, lint)
6. Create API contract test suite
7. **Go/No-Go gate**: Benchmark results meet targets

### Phase 1: Stateless APIs (2-3 weeks)

1. Migrate `/api/settings` endpoints
2. Migrate `/api/user-profile` (read-only)
3. Configure Traefik routing for migrated endpoints
4. **Go/No-Go gate**: 100% contract test pass rate

### Phase 2: Auth & Core (4-6 weeks)

1. Implement zitadel-go JWT middleware
2. Port scope-based authorization
3. Migrate Organizations module
4. Migrate Projects module
5. Migrate Users module
6. Migrate API tokens module
7. **Go/No-Go gate**: All auth flows working in production

### Phase 3: Data-Intensive (6-8 weeks)

1. Implement pgvector queries with Bun
2. Migrate Graph module (objects, relationships)
3. Migrate Graph search (semantic + lexical)
4. Migrate Documents module
5. Migrate Chunks module
6. Migrate Unified search
7. **Go/No-Go gate**: Search latency meets targets

### Phase 4: AI/LLM (4-6 weeks)

1. Implement Gemini embedding API client
2. Implement chat streaming (SSE)
3. Port conversation management
4. Evaluate/implement LangChainGo or sidecar
5. Migrate MCP integration
6. **Go/No-Go gate**: Chat functionality complete

### Phase 5: Background Workers (4-6 weeks)

1. Set up River job queue with fx lifecycle
2. Migrate extraction pipeline workers
3. Migrate email service jobs
4. Migrate data source sync jobs
5. Migrate scheduled tasks (cron-like)
6. **Go/No-Go gate**: All background jobs running

### Phase 6: Cleanup (2-4 weeks)

1. Remove NestJS from production
2. Archive TypeScript server code
3. Switch migrations from TypeORM to Goose
4. Update all documentation
5. Performance tuning

### Rollback Strategy

Each phase has isolated rollback:

1. Revert Traefik routing rules
2. NestJS server remains running during migration
3. Feature flags for hybrid operation if needed

## Open Questions

1. ~~**Should we use a job queue library or build on Postgres?**~~ **RESOLVED**

   - **Decision:** Use River (see Decision 10)
   - Postgres-backed, native Go, excellent fx integration

2. ~~**How to handle TypeORM migrations during transition?**~~ **RESOLVED**

   - **Decision:** TypeORM owns migrations until Phase 6 cutover (see Decision 9)
   - Go uses Bun models (manually updated when schema changes)
   - Post-cutover: migrate to Goose

3. **What's the minimum Go version to target?**

   - Recommendation: Go 1.22+ (latest stable, generics mature)

4. **Should MCP server be ported or kept as TypeScript sidecar?**

   - MCP SDK has Go support but less mature
   - Recommendation: Evaluate in Phase 4

5. **How to handle streaming SSE in Go?**
   - Echo has SSE support
   - Need to match current chunked response format

## Appendix: Current Server Metrics

| Metric                 | Current (NestJS) | Target (Go) |
| ---------------------- | ---------------- | ----------- |
| Cold start             | 15-20s           | <2s         |
| P99 API latency        | ~200ms           | <120ms      |
| Memory (idle)          | ~500MB           | <100MB      |
| Container image        | ~800MB           | <50MB       |
| Concurrent connections | ~1000            | ~10000      |

## Appendix: Module Migration Complexity

| Module          | Files | Complexity | Notes                          |
| --------------- | ----- | ---------- | ------------------------------ |
| health          | 3     | Low        | Simple HTTP checks             |
| settings        | 4     | Low        | Basic CRUD                     |
| user-profile    | 7     | Low        | CRUD with auth                 |
| auth            | 18    | Medium     | Zitadel integration            |
| orgs            | 6     | Medium     | Multi-tenant logic             |
| projects        | 6     | Medium     | RLS patterns                   |
| graph           | 43    | High       | Complex queries, relationships |
| documents       | 11    | Medium     | File handling, storage         |
| chat            | 9     | High       | Streaming, AI integration      |
| extraction-jobs | 19    | High       | Background processing, AI      |
| mcp             | 9     | Medium     | Protocol implementation        |
