## Context

The web UI Embeddings page (`/embeddings`) renders object and relationship queue
counters from `GET /api/embeddings/progress`. That endpoint (`EmbeddingControlHandler.Progress`)
calls `GraphEmbeddingJobsService.Stats` / `GraphRelationshipEmbeddingJobsService.Stats`,
which `COUNT(*)` over the whole `kb.graph_*_embedding_jobs` tables with no project
filter. The gateway sends `X-Project-ID`, but the handler ignores it.

A project-scoped counterpart already exists: `GET /api/projects/:id/embeddings/progress`
(`ProjectEmbeddingHandler.Progress`), which returns object + chunk counts for one
project. It is not used by the web UI, lacks relationship counts, and is
restricted to `project_admin`.

## Goals / Non-Goals

**Goals**
- The web UI Embeddings page shows only the active project's queue counts.
- Project-scoped progress covers object, relationship, and chunk queues.
- Reading project-scoped progress works for any project member (admin, user, viewer).

**Non-Goals**
- Changing the instance-wide `/api/embeddings/progress` (the CLI `memory embeddings
  progress` and superadmin surfaces keep their global semantics).
- Scoping worker running/paused state or worker configuration (instance-level).
- Adding retrigger/cancel UI.

## Decisions

**Reuse the project-scoped endpoint instead of adding a project filter to the global one.**
`/api/projects/:projectId/embeddings/progress` is purpose-built and already documented.
Filtering the global endpoint by `X-Project-ID` would silently change CLI/global callers
depending on headers. *Alternative rejected:* add `project_id` query param to the global
endpoint — more surface area and a membership check duplicated in a second place.

**Gateway resolves the project from session context.**
`MemoryClient.GetEmbeddingProgress` uses `m.projectIDFor(ctx)` and calls the
project-scoped path when non-empty. Fall back to the instance-wide path only when
no project is resolvable, preserving the pre-existing behaviour for project-less callers.

**Read gate is project membership; writes stay admin-only.**
Apply the canonical `RequireProjectTokenScope` + `RequireProjectMember` middleware to
the `/api/projects/:projectId/embeddings` group. `Progress` drops its
`requireProjectAdmin` check; `Retrigger`/`Cancel` keep it. Route param renamed
`:id` → `:projectId` to match the convention the middleware expects (URL unchanged).
*Alternative:* keep `:id` and hand-roll a membership check in the handler — rejected
because it diverges from the repo's canonical membership middleware.

**Worker state stays global.**
Workers are a single instance-wide pool; `/api/embeddings/status` is not project-scoped
and the page's "Workers" section is correct as-is. Only queue counts are project data.

## Risks / Trade-offs

- **Non-member / org-admin without a project membership row** → `RequireProjectMember`
  checks `kb.organization_memberships`, so org admins and superadmins in the owning org
  pass; a session user with neither org nor project membership gets 403. This is the
  intended read gate.
- **Project endpoint returns chunks in addition to objects/relationships** → the gateway
  decode struct ignores `chunks`; the page continues to show objects + relationships.
  A future chunk-queue section is additive.
- **No DB migration** → purely query + handler + client change.

## Migration Plan

No data migration. Deploy server and gateway together; the gateway falls back to the
global endpoint if the project-scoped route is unavailable, so a mixed-version window
degrades to the old (global) behaviour rather than erroring.

## Open Questions

None.
