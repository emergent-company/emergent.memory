## Context

The gateway document detail (`documents.templ`, `ui.go`) is one page. The rail/sub-nav pattern already exists (`layout.Rail` + `components.SettingsRail`, used by agents, settings, schema). Backend endpoints the gateway can already reach: `GET /api/documents/:id/content`, `GET /api/documents/:id/download` (307 → signed URL), `GET /api/admin/extraction-jobs/projects/:projectId?source_id=&source_type=`, `GET /api/graph/branches/:branchId/merge-readiness`, `POST /api/graph/branches/main/merge`. The gateway only consumes `CreateExtractionJob` and the extraction summary today.

## Goals / Non-Goals

**Goals:** three addressable document sub-views behind a rail; original-file preview; complete per-job extraction history with a merge action.

**Non-Goals:** xberg per-page image persistence/enrichment (the fields exist but the parsing worker discards `result.Images`; not implemented anywhere). Merge policy selection beyond the fixed `partial` preset. Server/schema changes.

## Decisions

### Sub-pages over tabs

Each view is a real route under `/documents/:id` (`/chunks`, `/extractions`) sharing `DocumentDetailPage`'s header and a `documentSubNav` rail, matching `agentSubNav`. The page stays server-rendered and htmx-boosted; no client tab state.

### Preview via a gateway binary proxy

The browser cannot attach the session bearer/`X-Project-ID` to a memory URL, and `/download` returns `attachment` (forces a download). The gateway adds `GET /documents/:id/file` that calls `DownloadDocument`, follows the 307 to the signed URL, and streams the bytes with `Content-Disposition: inline`, mirroring `avatarProxy`. Preview mode is selected by `doc.MimeType`.

### Derive the staging branch client-side

`GET` the extraction-jobs list yields each job's `created_objects`. The job's staging branch is read from the first created object (`GetGraphObject().BranchID`) — the same technique `loadExtractionResults` already uses — so no server DTO change is needed. The lookup happens only in the lazily-loaded per-job detail partial, not the list, so the list stays a single request.

### Lazy per-job detail

The history list renders counts/timestamps from the list response. Per-job objects, relationships, branch name, and readiness load via an HTMX partial (`/documents/:id/extractions/:jobId`), avoiding N+1 on page load and keeping large object sets off the list.

### Fixed merge policy

Merge posts to `POST /api/graph/branches/main/merge` with `policy:"partial"` — the same preset the auto-merge worker uses: apply clean additions and fast-forwards, leave conflicts/similar objects on the branch for review. Any project member may trigger it (the graph route needs only project membership); the action is hidden — not disabled — when there is no staging branch or the branch is already merged.

## Risks / Trade-offs

- [Branch lookup via first created object] → if a job's objects are gone the branch cannot be resolved; the detail degrades to counts only. Mitigation: fall back gracefully; the merge action is simply absent.
- [Large object sets per job] → detail lists are branch-scoped and ID-filtered to the job; acceptable for the lazy partial.
- [Signed-URL redirect following] → the default HTTP client follows it; an expired/failed download surfaces as a gateway error and the preview degrades to a download link.

## Migration Plan

Gateway-only, additive routes. No data migration. Rollback is reverting the branch.

## Open Questions

- Whether to surface a merge-policy picker later; out of scope now.
