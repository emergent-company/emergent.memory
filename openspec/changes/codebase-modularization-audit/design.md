## Context

The Memory Go server (`apps/server/`) is a 46-domain monolith using `uber-go/fx` for dependency injection. Each domain is architecturally consistent (handler → service → store → module), but six categories of cross-cutting technical debt have accumulated:

1. **Auth guard boilerplate** — 289 inline `user := auth.GetUser(c); if user == nil { return apperror.ErrUnauthorized }` blocks, plus 40 additional `if user.ProjectID == ""` checks, scattered across handler methods.
2. **Duplicate generic types** — `APIResponse[T]`, `PaginatedResponse[T]`, `SuccessResponse[T]` each defined in 3–4 domains independently.
3. **Inconsistent error construction** — two `apperror` call styles coexist across 1,246 combined call sites.
4. **Setter-injection anti-patterns** — 9 `SetXxx()` calls post-construction exist because circular domain imports were not resolved via interfaces at design time.
5. **Tight `graph`/`journal` coupling** — `graph.Service` holds a `*journal.Service` field and calls it with nil-guards in 6 places.
6. **No feature-toggle infrastructure** — all 46 domains are registered unconditionally in `main.go`; a "lite" build is currently impossible without code deletion.

This change is purely internal — no external API, CLI, or database schema changes.

## Re-sync (Sep 2026)

Three months after the original design, the codebase has drifted both directions. Correcting the record before resuming remediation:

| Debt | May (design) | Sep (re-sync) | Δ |
|---|---|---|---|
| inline `user == nil` guards | 289 | 312 | +23 |
| `apperror` Style A (`Err*.With*`) | 664 | 1168 | +504 |
| Style B (`apperror.New*`) | — | 732 | already mixed in |
| cross-domain `SetXxx` setters | 9 | 21 | +12 |
| local `getProjectID()` copies | 3 | 3 | = |
| domains | 46 | 49 | +3 |

**Shipped but not recorded in tasks.md:** §1 (httputil), §2 helpers, §4 helper, §6 (graph/journal decoupling), §7 (FeatureSet). **Recorded but not actually done:** the *application* of those helpers — `RequireProject()` is defined yet applied to 0 routes; 3 `fx.Hook` blocks remain; all 312 guards and 1168 Style-A calls remain.

**Wrong assumption:** `domain/chat` and `pkg/llm/vertex` were assumed possibly-dead. Both are **live** — `chat.Module` is wired in `main.go`, `pkg/sdk/chat` has a full client, `cmd/swiftbridge` imports it, and `domain/extraction` embedding workers depend on `vertex.EmbedResult`. Domain removal is off the table; keep them feature-flagged instead.

**Missing layer:** the `.golangci.yml` blanket-excludes `errcheck`, `staticcheck`, and `unused` via `text: "."` — no enforcement exists, which is *why* the debt grew. See D8.

**Resolved — `pkg/sdk` duplicate types are a legitimate boundary, not debt.** The sdk is a self-contained published module (`.../apps/server/pkg/sdk`) with **zero** imports from the server module, consumed by `apps/cli`, `cmd/swiftbridge`, and external clients. Its 4 DTO definitions (`agents`, `agentdefinitions`, `mcpregistry`, `superadmin`) must stay self-contained so client consumers don't pull in echo/fx/bun. Leave them. The genuine httputil residue is *inside* the server module only: 4 type aliases, duplicated `SuccessResponse`/`ErrorResponse` constructor funcs in `agents` + `mcpregistry` (also named differently from httputil's `NewSuccessResponse`/`NewErrorResponse`), and `superadmin.SuccessResponse`.

## Goals / Non-Goals

**Goals:**
- Eliminate the three largest duplication hotspots (auth boilerplate, response types, apperror style)
- Replace all setter-injection wiring with explicit named interfaces
- Decouple `graph` from `journal` via an event-sink interface
- Introduce `FeatureSet` config struct and conditional `fx.Options` pattern in `main.go`
- Gate optional/debug domains behind config-driven module inclusion
- Establish the architectural patterns that future domain authors should follow

**Non-Goals:**
- Full feature-flag UI or remote config — `FeatureSet` is env-var only
- Removing any currently-active domain (domain removal is a follow-on change after toggle infrastructure exists)
- Performance optimization
- Database schema changes
- Any external API changes

## Decisions

### D1 — `pkg/httputil` for shared response types (not `pkg/apperror`)

**Decision:** Create a new `pkg/httputil` package for `APIResponse[T]`, `PaginatedResponse[T]`, `SuccessResponse[T]`.

**Rationale:** These are HTTP response shapes, not error types. Placing them in `pkg/apperror` would mix concerns. A dedicated `pkg/httputil` is a clean extension point for future HTTP utilities (pagination helpers, cursor parsing, etc.).

**Alternative considered:** Embed in `pkg/apperror` — rejected because apperror is about error representation, not success response shaping.

### D2 — `RequireProject()` as Echo middleware, not just a helper function

**Decision:** `RequireProject()` returns an `echo.MiddlewareFunc` that can be applied to route groups or individual routes. Handlers that pass this middleware receive a guarantee that `auth.MustGetUser(c)` will never return nil and `user.ProjectID` will never be empty.

**Rationale:** Middleware application is a declarative statement of route requirements. It moves the concern out of every handler body and into the route registration layer, which is the correct place.

**Alternative considered:** A `mustGetUser(c) (*auth.User, error)` helper that returns early — simpler but still requires every handler to check the second return value. Middleware is strictly cleaner.

**Implementation:** `pkg/auth` gains:
```go
func RequireProject() echo.MiddlewareFunc
func MustGetUser(c echo.Context) *auth.User   // panics if nil (never should be post-middleware)
func GetProjectUUID(c echo.Context) (uuid.UUID, error)
```

> **Re-sync correction (Sep 2026):** the premise was wrong. Routes are **already** protected — `auth.Middleware.RequireAuth()` (65 usages) + `RequireProjectID()` (10) are applied in every `routes.go`. The 312 `user == nil` guards in handlers are **redundant defense-in-depth**, not missing auth (e.g. `apitoken.Create` re-checks `user == nil` on a route already wrapped in `RequireAuth()`). Corrected plan: **delete the guards** via `auth.MustGetUser(c)` and **retire the unused package-level `RequireProject()`** (0 usages — it duplicates `auth.Middleware.RequireAuth()`), rather than "rolling it out". `MustGetUser()` and `GetProjectUUID()` remain valuable.

### D3 — Standardize on `apperror` Style B (`apperror.New*` constructors)

**Decision:** Migrate all 664 Style A (`apperror.ErrBadRequest.WithMessage(...)`) usages to Style B (`apperror.NewBadRequest(...)`).

**Rationale:** Style B reads more naturally, is consistent with Go error construction conventions, and produces slightly more structured error objects. Style A is a builder-chaining pattern that adds visual noise.

**Migration approach:** Mechanical find-and-replace via script, not manual edits. Verified by compile + existing test suite.

### D4 — Interfaces defined in the *receiving* package (dependency inversion)

**Decision:** For each setter-injection anti-pattern, define the interface in the package that *depends on the capability* (the receiver), not in the package that implements it.

**Example:** `mcp` needs to dispatch to an agent tool handler. Define `AgentToolDispatcher` interface in `domain/mcp`. `domain/agents` implements it. `main.go` wires the concrete type at startup via fx — no setter needed.

**Rationale:** This is standard Go dependency inversion. The depending package owns the interface, making it easy to stub for tests and removing the circular import.

**7 interfaces to define:**

| Interface | Defined in | Implemented by |
|---|---|---|
| `AgentToolDispatcher` | `domain/mcp` | `domain/agents` |
| `EmbeddingWorkerController` | `domain/mcp` | `domain/extraction` |
| `GraphObjectPatcher` | `domain/mcp` | `domain/graph` |
| `SessionTitleHandler` | `domain/mcp` | `domain/agents` |
| `ToolPoolInvalidator` | `domain/mcpregistry` | `domain/agents` |
| `OrgToolPoolInvalidator` | `domain/orgs` | `domain/agents` |
| `SessionChangeHandler` | `domain/mcprelay` | `domain/agents` |

### D5 — `GraphEventSink` interface in `domain/graph`

**Decision:** Define:
```go
// domain/graph/events.go
type EventSink interface {
    LogObjectCreated(ctx context.Context, projectID string, obj *Object) error
    LogObjectUpdated(ctx context.Context, projectID string, obj *Object) error
    LogObjectDeleted(ctx context.Context, projectID string, id uuid.UUID) error
    // ... 3 more matching current journal call sites
}

type NoopEventSink struct{}
```

`graph.Service` holds `EventSink` (interface), defaulting to `NoopEventSink{}` when journal is not wired.

**Rationale:** Removes the nil-guard pattern, makes `journal` an optional dependency, and enables unit-testing `graph.Service` without a journal.

### D6 — `FeatureSet` as env-var struct in `internal/config`, conditional `fx.Options` in `main.go`

**Decision:**
```go
// internal/config/features.go
type FeatureSet struct {
    Agents     bool `env:"FEATURE_AGENTS"     envDefault:"true"`
    MCP        bool `env:"FEATURE_MCP"        envDefault:"true"`
    Sandbox    bool `env:"FEATURE_SANDBOX"    envDefault:"true"`
    Backups    bool `env:"FEATURE_BACKUPS"    envDefault:"true"`
    Devtools   bool `env:"FEATURE_DEVTOOLS"   envDefault:"false"`
    Monitoring bool `env:"FEATURE_MONITORING" envDefault:"true"`
    Tracing    bool `env:"FEATURE_TRACING"    envDefault:"true"`
    Superadmin bool `env:"FEATURE_SUPERADMIN" envDefault:"true"`
    Chat       bool `env:"FEATURE_CHAT"       envDefault:"false"`
}
```

`main.go` builds an `fx.Options` slice conditionally:
```go
opts := coreFxOptions()
if cfg.Features.Agents { opts = append(opts, agents.Module) }
// ...
fx.New(opts...).Run()
```

**Rationale:** Env-vars are the simplest zero-infra toggle mechanism. Defaults preserve current behavior. `FEATURE_DEVTOOLS=false` and `FEATURE_CHAT=false` by default reflect current reality.

**Alternative considered:** Go build tags — more powerful for binary size but require separate build pipelines; env-vars suffice for the "lite" deployment use case.

### D7 — `RegisterWorkerLifecycle` generic helper

**Decision:** Add to `domain/extraction/worker.go`:
```go
type Worker interface {
    Start(ctx context.Context) error
    Stop(ctx context.Context) error
}

func RegisterWorkerLifecycle(lc fx.Lifecycle, w Worker) {
    lc.Append(fx.Hook{
        OnStart: func(_ context.Context) error { return w.Start(context.Background()) },
        OnStop:  func(ctx context.Context) error { return w.Stop(ctx) },
    })
}
```

All 6 current lifecycle hook blocks in `extraction/module.go` collapse to `RegisterWorkerLifecycle(lc, worker)` calls.

### D8 — Prevention layer (L3): lint gates, not one-shot cleanup

**Decision:** Remediation alone will decay — it already did (+504 Style-A calls in 3 months). Add enforcement so re-introduction is impossible:

1. **Un-mask the linter.** Remove the three blanket `text: "."` exclusions for `errcheck`, `staticcheck`, `unused`. Replace with targeted `exclude-rules` (or per-line `//nolint`) for the confirmed-legacy set only.
2. **Custom rules for the four duplication patterns.** Fail CI on:
   - inline `user == nil` guard in a `domain/*/handler.go` → force `RequireProject()` + `MustGetUser()`
   - new `func (s *Service) Set[A-Z]` cross-domain setter → force constructor/`fx.Provide` injection
   - new `type APIResponse|PaginatedResponse|SuccessResponse` outside `pkg/httputil`
   - new `apperror.Err*.With*` Style A chaining → force Style B
3. **Ratchet, don't fix-all-at-once.** Record current counts as the floor; CI fails only on *new* violations (baseline ratchet). Existing debt gets paid down via §2–§5 without blocking.

**Rationale:** a linter that excludes everything is a lint theater. The growth data proves cleanup without gates is temporary. Enforcement turns a one-shot audit into a systemic invariant.

**Alternative considered:** full clean baseline before enabling any gate → rejected, would block all merge traffic for weeks. Ratchet enables the gate immediately and lets remediation proceed incrementally.

## Risks / Trade-offs

**`RequireProject()` middleware rollout is broad** → Mitigation: Apply incrementally per route group; handlers not yet migrated continue to work (middleware adds a check, doesn't remove existing ones). Use a single PR per domain.

**`apperror` style migration (664 sites) risks introducing subtle bugs** → Mitigation: Automated script + full compile check + existing e2e test suite. Style A and Style B produce equivalent HTTP error responses.

**Interface extraction for setter injection requires fx re-wiring in `main.go`** → The 7 new interfaces must be wired via `fx.Provide` or `fx.Invoke` in `main.go`. Risk of missed wiring causing nil panics at startup. → Mitigation: fx panics at startup (not at runtime) if a dependency is unsatisfied, so missing wires are caught immediately in dev.

**`FeatureSet` defaults must preserve current behavior** → If any default flips from `true` to `false`, existing deployments break silently. → Mitigation: All currently-active domains default to `true`; only currently-inactive defaults (`devtools`, `chat`) default to `false`.

**`domain/chat` is live, not removable** → Re-sync confirms `chat.Module` wired in `main.go`, `pkg/sdk/chat` client, and `cmd/swiftbridge` dependency. `FEATURE_CHAT` (default `true`) gates it for "lite" builds; do **not** plan deletion.

## Migration Plan

Incremental — each step is independently mergeable:

1. **Step 1:** Add `pkg/httputil` + migrate 4 domain files (no behavior change)
2. **Step 2:** Add `auth.GetProjectUUID()`, `RequireProject()`, `MustGetUser()` to `pkg/auth`; migrate 3 local `getProjectID()` copies
3. **Step 3:** Apply `RequireProject()` middleware to route groups domain-by-domain (one PR per domain or batch low-risk ones)
4. **Step 4:** Automated `apperror` Style A → Style B migration script; single PR
5. **Step 5:** `RegisterWorkerLifecycle` helper + collapse `extraction/module.go`
6. **Step 6:** Define 7 interfaces + remove setter-injection wiring (one PR per interface pair)
7. **Step 7:** `GraphEventSink` interface + `NoopEventSink` + remove `*journal.Service` embed from `graph.Service`
8. **Step 8:** `FeatureSet` config + conditional `fx.Options` in `main.go`
9. **Step 9:** Audit `/api/chat` usage; gate behind `FEATURE_CHAT=false`; schedule removal

**Rollback:** Each step is additive or mechanical. Steps 1–5 are purely additive (old call sites still compile). Steps 6–9 change wiring but are verified by Go compiler + fx startup checks.

## Open Questions

1. **Should `FEATURE_AGENTS=false` also implicitly disable `FEATURE_MCP`?** — MCP without agents is partially functional (can relay to external tools). Decision needed before Step 8.
2. ~~`domain/chat` — is the `/api/chat` route called by any current UI page?~~ **Resolved (Sep 2026):** chat is live — `pkg/sdk/chat` client + `cmd/swiftbridge` + `main.go` wiring. Keep feature-flagged, do not remove.
3. **`RequireProject()` vs `RequireAuth()` naming** — Should there be a separate `RequireAuth()` (user only, no project) for admin routes that don't need a project context?
