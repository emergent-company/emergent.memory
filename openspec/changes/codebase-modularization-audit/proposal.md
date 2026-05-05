## Why

The Go server has accumulated 289 copy-pasted auth guard blocks, 4 duplicate generic response types, 9 setter-injection anti-patterns, and zero infrastructure for feature toggling — making the codebase hard to maintain and making a "lite" build (stripped of AI orchestration features) structurally impossible today. Addressing this now unlocks both a cleaner codebase and a viable path to shipping a lightweight deployment target.

## What Changes

- Introduce `pkg/httputil` package consolidating `APIResponse[T]`, `PaginatedResponse[T]`, and `SuccessResponse[T]` — currently defined in 4 separate domains
- Add `auth.GetProjectUUID(c)` helper to `pkg/auth` and remove 3 copy-pasted local `getProjectID()` functions
- Introduce `RequireProject()` middleware variant in `pkg/auth` to replace 289 inline user-nil-check + 40 project-ID-check boilerplate blocks across handlers
- Standardize on `apperror` Style B (`apperror.NewBadRequest/NewInternal/NewNotFound`) — migrate 664 Style A usages
- Extract `RegisterWorkerLifecycle[W Worker](lc, w)` generic helper in `apps/server/domain/extraction` to collapse 6 near-identical `fx.Lifecycle` hook blocks
- Replace 7 of 9 setter-injection (`SetXxx()`) wiring points with explicit named interfaces defined in the receiving package
- Decouple `graph.Service` from direct `*journal.Service` embed via a `GraphEventSink` interface
- Add `FeatureSet` config struct with env-var flags controlling conditional `fx.Options` in `main.go`
- Gate `devtools`, `monitoring`, `tracing`, `backups` behind build-tag or config-driven module inclusion to stop compiling debug code into production binaries
- Audit and remove `domain/chat` + `pkg/llm/vertex` if confirmed dead (no active UI callers)

## Capabilities

### New Capabilities

- `shared-http-types`: Consolidated `pkg/httputil` package with generic response types and constructors shared across all domains
- `auth-middleware-consolidation`: `RequireProject()` middleware + `MustGetUser(c)` / `GetProjectUUID(c)` helpers eliminating handler-level auth boilerplate
- `explicit-domain-interfaces`: Named interfaces replacing setter-injection anti-patterns across `mcp`, `mcpregistry`, `orgs`, and `mcprelay` domains
- `graph-journal-decoupling`: `GraphEventSink` interface decoupling `graph.Service` from direct `*journal.Service` dependency
- `worker-lifecycle-helper`: Generic `RegisterWorkerLifecycle` helper reducing extraction worker module boilerplate
- `feature-flag-infrastructure`: `FeatureSet` config struct + conditional `fx.Options` pattern in `main.go` enabling runtime feature toggling per deployment

### Modified Capabilities

None — these are internal refactors. No public API or CLI behavior changes.

## Impact

**Packages created:**
- `apps/server/pkg/httputil/` — new shared response types

**Packages modified:**
- `apps/server/pkg/auth/` — new helpers + middleware variant
- `apps/server/pkg/apperror/` — Style A usages migrated to Style B
- `apps/server/internal/config/` — `FeatureSet` added
- `apps/server/cmd/server/main.go` — conditional fx module loading

**Domains modified (handler-level auth cleanup):**
All ~30 domains that currently repeat the 3-line auth guard block

**Domains with setter injection replaced:**
`domain/mcp`, `domain/mcpregistry`, `domain/orgs`, `domain/mcprelay`

**Domains decoupled:**
`domain/graph` (from `domain/journal`)

**Domains potentially removed:**
`domain/chat`, `pkg/llm/vertex` (pending audit confirmation)

**No breaking changes to external APIs, CLI, or database schema.**
