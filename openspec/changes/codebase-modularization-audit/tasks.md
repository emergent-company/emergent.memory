## 1. Shared HTTP Types (pkg/httputil)

- [x] 1.1 Create `apps/server/pkg/httputil/response.go` with `APIResponse[T any]`, `PaginatedResponse[T any]`, `SuccessResponse[T any]` types and `NewSuccessResponse[T]` constructor
- [x] 1.2 Migrate `domain/agents/dto.go` — replace local `APIResponse[T]` and `SuccessResponse` with imports from `pkg/httputil`
- [x] 1.3 Migrate `domain/extraction/dto.go` — replace local `APIResponse[T]` with `pkg/httputil` import
- [x] 1.4 Migrate `domain/mcpregistry/entity.go` — replace local `APIResponse[T]` with `pkg/httputil` import
- [x] 1.5 Migrate `domain/sandboximages/entity.go` — replace local `APIResponse[T]` with `pkg/httputil` import
- [x] 1.6 Migrate `pkg/sdk/` client files — replace any duplicate response type definitions with `pkg/httputil` imports
- [x] 1.7 Verify build passes: `task build`

## 2. Auth Middleware Consolidation (pkg/auth)

- [ ] 2.1 Add `GetProjectUUID(c echo.Context) (uuid.UUID, error)` to `apps/server/pkg/auth/context.go`
- [ ] 2.2 Add `RequireProject() echo.MiddlewareFunc` to `apps/server/pkg/auth/middleware.go`
- [ ] 2.3 Add `MustGetUser(c echo.Context) *auth.User` to `apps/server/pkg/auth/context.go`
- [ ] 2.4 Remove local `getProjectID()` helper from `domain/chunks/handler.go` — replace with `auth.GetProjectUUID(c)`
- [ ] 2.5 Remove local `getProjectID()` helper from `domain/journal/handler.go` — replace with `auth.GetProjectUUID(c)`
- [ ] 2.6 Remove local `getProjectID()` helper from `domain/graph/handler.go` — replace with `auth.GetProjectUUID(c)`
- [ ] 2.7 Apply `RequireProject()` middleware to route groups in all domains — remove inline `user == nil` auth guard blocks from handler methods (batch by domain; one sub-task per domain group)
- [ ] 2.8 Verify build passes and all auth-protected routes return 401 for unauthenticated requests: `task build && task test`

## 3. apperror Style Standardization

- [ ] 3.1 Audit `pkg/apperror/` to confirm Style B constructors (`NewBadRequest`, `NewInternal`, `NewNotFound`, etc.) exist and are equivalent to Style A chaining
- [ ] 3.2 Write a migration script (or `sed` one-liner set) to convert Style A patterns (`apperror.ErrBadRequest.WithMessage(...)`, `apperror.ErrInternal.WithInternal(...)`) to Style B equivalents across the codebase
- [ ] 3.3 Run migration script and verify all 664 Style A usages are converted
- [ ] 3.4 Verify build passes: `task build`
- [ ] 3.5 Run test suite to confirm no behavior changes: `task test`

## 4. Worker Lifecycle Helper (domain/extraction)

- [ ] 4.1 Create `apps/server/domain/extraction/worker.go` — define `Worker` interface with `Start(context.Context) error` and `Stop(context.Context) error`
- [ ] 4.2 Add `RegisterWorkerLifecycle(lc fx.Lifecycle, w Worker)` function to `worker.go`
- [ ] 4.3 Verify all 6 extraction workers (`GraphEmbeddingWorker`, `GraphRelationshipEmbeddingWorker`, `ChunkEmbeddingWorker`, `DocumentParsingWorker`, `ObjectExtractionWorker`, `EmbeddingSweepWorker`) satisfy the `Worker` interface
- [ ] 4.4 Replace all 6 `lc.Append(fx.Hook{...})` blocks in `extraction/module.go` with `RegisterWorkerLifecycle(lc, worker)` calls
- [ ] 4.5 Verify build passes: `task build`

## 5. Explicit Domain Interfaces (setter injection removal)

- [ ] 5.1 Define `AgentToolDispatcher` interface in `domain/mcp` — match method signature currently used via `SetAgentToolHandler`
- [ ] 5.2 Define `EmbeddingWorkerController` interface in `domain/mcp` — match method signature currently used via `SetEmbeddingControlHandler`
- [ ] 5.3 Define `GraphObjectPatcher` interface in `domain/mcp` — match method signature currently used via `SetGraphObjectPatcher`
- [ ] 5.4 Define `SessionTitleHandler` interface in `domain/mcp` — match method signature currently used via `SetSessionTitleHandler`
- [ ] 5.5 Define `ToolPoolInvalidator` interface in `domain/mcpregistry` — match method signature currently used via `SetToolPoolInvalidator`
- [ ] 5.6 Define `OrgToolPoolInvalidator` interface in `domain/orgs` — match method signature currently used via `SetToolPoolInvalidator`
- [ ] 5.7 Define `SessionChangeHandler` interface in `domain/mcprelay` — match method signature currently used via `OnChange`
- [ ] 5.8 Update `domain/mcp` constructor(s) to accept the 4 interfaces as parameters (remove `SetXxx` methods)
- [ ] 5.9 Update `domain/mcpregistry` constructor to accept `ToolPoolInvalidator` as parameter (remove `SetToolPoolInvalidator`)
- [ ] 5.10 Update `domain/orgs` constructor to accept `OrgToolPoolInvalidator` as parameter (remove `SetToolPoolInvalidator`)
- [ ] 5.11 Update `domain/mcprelay` constructor to accept `SessionChangeHandler` as parameter (remove `OnChange` setter)
- [ ] 5.12 Update `cmd/server/main.go` — wire concrete agent/extraction/graph types to the new interfaces via `fx.Provide` or `fx.Annotate`; remove all `SetXxx()` calls from `fx.Invoke` blocks
- [ ] 5.13 Verify build passes and server starts cleanly: `task build && task start && task status`

## 6. Graph/Journal Decoupling (GraphEventSink)

- [ ] 6.1 Create `apps/server/domain/graph/events.go` — define `EventSink` interface with methods matching all current `s.journal.Log*(...)` call sites in `graph/service.go`
- [ ] 6.2 Add `NoopEventSink` struct to `events.go` implementing `EventSink` with no-op method bodies
- [ ] 6.3 Replace `*journal.Service` field in `graph.Service` struct with `EventSink` field; initialize default to `NoopEventSink{}`
- [ ] 6.4 Replace all `s.journal.Log*(...)` calls with `s.eventSink.Log*(...)` calls; remove all `if s.journal != nil` nil-guards
- [ ] 6.5 Verify `domain/graph` no longer imports `domain/journal`: `go list -deps ./domain/graph/... | grep journal` should return nothing
- [ ] 6.6 Update `domain/journal/service.go` — add methods to satisfy `graph.EventSink` interface (or verify existing methods already match signatures)
- [ ] 6.7 Update `domain/graph/module.go` — accept optional `graph.EventSink` via fx; wire `journal.Service` as the concrete implementation when journal is enabled
- [ ] 6.8 Update `cmd/server/main.go` if needed to wire journal → graph event sink
- [ ] 6.9 Verify build passes and graph mutations are still logged: `task build && task test`

## 7. Feature Flag Infrastructure (FeatureSet + conditional fx.Options)

- [ ] 7.1 Audit `/api/chat` route usage in `emergent.memory.ui` repo to answer open question: `grep -r "api/chat" /root/emergent.memory.ui/src/`
- [ ] 7.2 Create `apps/server/internal/config/features.go` — define `FeatureSet` struct with env-var tags and defaults per design doc
- [ ] 7.3 Add `Features FeatureSet` field to main `Config` struct in `internal/config/config.go`
- [ ] 7.4 Refactor `cmd/server/main.go` — extract `coreFxOptions()` function containing all always-on domain modules
- [ ] 7.5 Add conditional `fx.Options` blocks in `main.go` for each feature-flagged domain: `agents`, `mcp`, `mcpregistry`, `mcprelay`, `sandbox`, `sandboximages`, `backups`, `monitoring`, `tracing`, `devtools`, `superadmin`, `chat`
- [ ] 7.6 Verify default behavior unchanged — start server with no `FEATURE_*` env vars, confirm all routes work: `task build && task start && task status`
- [ ] 7.7 Verify feature toggle works — start server with `FEATURE_AGENTS=false`, confirm agents routes return 404 and server starts cleanly
- [ ] 7.8 Document `FEATURE_*` env vars in `apps/server/README.md` or equivalent config documentation

## 8. Verification & Cleanup

- [ ] 8.1 Run full test suite: `task test`
- [ ] 8.2 Run e2e tests: `task test:e2e`
- [ ] 8.3 Run linter: `task lint`
- [ ] 8.4 Confirm no `SetXxx` setter methods remain for cross-domain wiring: `grep -r "func.*Set[A-Z]" apps/server/domain/`
- [ ] 8.5 Confirm no inline `user == nil` auth guards remain in handler files: `grep -rn "user == nil" apps/server/domain/`
- [ ] 8.6 Confirm `APIResponse`, `PaginatedResponse`, `SuccessResponse` defined only in `pkg/httputil`: `grep -rn "type APIResponse" apps/server/`
- [ ] 8.7 Confirm no Style A `apperror` usage remains: `grep -rn "\.WithMessage\|\.WithInternal" apps/server/domain/`
