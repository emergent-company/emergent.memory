## Why

`apps/server/domain/extraction/embedding_control_routes.go` registers the `/api/embeddings` control group with only `RequireAuth()` — no project/membership/admin scoping. Two distinct gaps follow:

1. **Unprojected reads.** `GET /api/embeddings/progress` calls `GraphEmbeddingJobsService.Stats` / `GraphRelationshipEmbeddingJobsService.Stats`, which are deployment-wide `COUNT(*)` with no project filter, so any authenticated user sees cross-org aggregate queue counts.
2. **Operator writes.** `pause`, `resume`, `config` (PATCH), `queue` (DELETE), and `reset-schedule` are deployment-wide controls reachable by any authenticated non-admin, non-member caller.

These are deployment-wide operator controls, not project-scoped resources, so the shared project pair is the wrong fit for the writes.

## What Changes

Split the single `RequireAuth()` group into three authorization postures (issue #940):

- **Read surface** (`status`, `progress`): `RequireProjectTokenScope` + `RequireProjectMember` — both are consumed by the gateway embeddings page on behalf of a project member. `progress` additionally scopes its queue counts to the caller's own project via `StatsByProject`; a caller with no project context may obtain the deployment-wide view only with an active `superadmin_full` grant.
- **Operator write surface** (`pause`, `resume`, `config`, `queue`, `reset-schedule`): an active `superadmin_full` grant (`RequireSuperadminFull`). A scope gate is deliberately NOT used — `admin:all` is mintable by any `org_admin` (`pkg/auth.CanGrantAdminAll`), so `admin:write`/`admin:read` would admit a non-platform-admin caller.
- **Diagnostic read surface** (`diagnose`): the same `superadmin_full` grant — unprojected global queue counts.

`GraphRelationshipEmbeddingJobsService` gains a `StatsByProject` counterpart to the object service's existing `StatsByProject` (join `kb.graph_relationships` on `project_id`). `pkg/auth` gains a `superadminRole` helper (the single `core.superadmins` query), an `IsSuperadminFull` handler-layer helper, and a `RequireSuperadminFull` middleware, so both the middleware and the handler fallback resolve the same canonical platform-admin boundary.

## Capabilities

### New Capabilities

<!-- none -->

### Modified Capabilities

- `embedding-status`: documents the authorization posture of the `/api/embeddings` control group (project-scoped reads, `superadmin_full`-gated operator writes and diagnostics).

## Impact

- `apps/server/domain/extraction/embedding_control_routes.go` — three posture groups replace the single `RequireAuth()` group.
- `apps/server/domain/extraction/embedding_control_handler.go` — `Progress` scopes counts to the caller's project; deployment-wide view requires `superadmin_full`.
- `apps/server/domain/extraction/graph_relationship_embedding_jobs.go` — new `StatsByProject`.
- `apps/server/pkg/auth/superadmin.go` — `superadminRole` / `IsSuperadminFull` / `RequireSuperadminFull`.
- `apps/server/pkg/auth/scope_mapping.go` — `dbSuperadminRole` delegates to the shared `superadminRole`.
- `apps/server/domain/extraction/embedding_control_authz_test.go` — fail-first/post-fix authz test proving org_admin `admin:all` token 403, superadmin_full pass, and project-scoped read preservation.
- **No migration, no response-shape change.**
- **Consumer impact:** the gateway embeddings page (`GET /api/embeddings/progress` and `/status`) continues to work — it sends `X-Project-ID` and its user is a project member, so both reads still return. The CLI `memory embeddings` write commands (pause/resume/config/clear) and the global `progress` view now require a `superadmin_full` credential, which is the intended operator posture. MCP tool access (`CurrentStatus`/`PauseAll`/`ResumeAll`/`ApplyConfig`) is in-process, not HTTP, and is unaffected.

