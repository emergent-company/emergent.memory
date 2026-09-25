## Why

The web UI Embeddings page shows queue counts aggregated across the entire instance, not the active project. A user with an empty project still sees other projects' numbers, which reads as if their own project has embedding backlog. The gateway already sends `X-Project-ID`, but the server-side `/api/embeddings/progress` handler ignores it and counts every row in `kb.graph_embedding_jobs` / `kb.graph_relationship_embedding_jobs`. This was a known, deferred risk (R1) in the original embeddings-status change.

## What Changes

- The web UI Embeddings page queue counts (object and relationship) SHALL be scoped to the active project.
- The gateway SHALL fetch project-scoped progress from `GET /api/projects/:projectId/embeddings/progress` when an active project is resolvable, falling back to the instance-wide `GET /api/embeddings/progress` only when none is.
- The project-scoped progress endpoint SHALL include relationship queue counts (it already returns object and chunk counts).
- Reading project-scoped progress SHALL require project membership; retrigger/cancel remain project-admin-only.
- Worker running/paused state and worker configuration stay instance-wide (workers are instance-level); no change to `/api/embeddings/status`.

## Capabilities

### New Capabilities
<!-- none -->

### Modified Capabilities
- `embedding-status`: queue progress on the embeddings page is scoped to the active project instead of the whole instance.

## Impact

- `apps/server/domain/extraction/project_embedding_handler.go`: include the already-existing relationship `StatsByProject` in the progress response; read gate = project member.
- `apps/server/domain/extraction/project_embedding_routes.go`: apply canonical project-scope + membership middleware; `:id` → `:projectId` (URL unchanged).
- `apps/web-ui/gateway/embeddings.go` (+ tests): `GetEmbeddingProgress` calls the project-scoped endpoint.
- Spec: `openspec/specs/embedding-status/spec.md` (via this change's delta).
