## Context

See `proposal.md` — Why. Alfred has no backup surface, but the previous Memory UI supported project backups (create, list, download, checksums). This change adds a Backups page to the gateway, backed by Emergent Memory's backup domain (`/root/emergent.memory/apps/server/domain/backups`). It builds on `add-auth-project-frame`: backup calls carry the session's Bearer token and the active project/org via the `X-Project-ID` / `X-Org-ID` headers the same way the rest of the gateway does after that change.

Research findings from the Memory source (the `openapi.yaml` at repo root does **not** contain the backup routes — they live in `domain/backups/routes.go` and the swagger docs, and are feature-gated by `Config.Features.Backups`):

- **Endpoints** (note the `/api/v1/...` prefix — unlike the `/api/...` routes the rest of `memory.go` uses):
  - `GET /api/v1/organizations/{orgId}/backups` — paginated list, `?project_id=`, `?limit=`, `?cursor=`; returns `{ backups, total, nextCursor }`.
  - `POST /api/v1/projects/{projectId}/backups` — create; body `{ includeDeleted, includeChat, retentionDays }`; returns **202** with a `Backup` entity in `status: "creating"`.
  - `GET /api/v1/organizations/{orgId}/backups/{backupId}` — one backup's details.
  - `GET /api/v1/organizations/{orgId}/backups/{backupId}/download` — **302** redirect to a presigned URL (1h expiry); requires `status: "ready"`.
  - `DELETE /api/v1/organizations/{orgId}/backups/{backupId}` — **204** delete.
  - `POST /api/v1/projects/{projectId}/restore` and `GET /api/v1/projects/{projectId}/restores/{restoreId}` — **501 "not implemented"** (hardcoded stubs).
- **Statuses**: `creating` · `ready` · `failed` · `deleted`. **Progress**: int 0–100. **Checksums**: nullable `manifestChecksum` / `contentChecksum`. **Stats**: a `map[string]any` of object/chunk/chat counts.
- **Create DTO**: only `includeDeleted`, `includeChat`, `retentionDays` (default 30, valid 1–365). There is **no** `backupType` / `parentBackupId` on the create request — the `full`/`incremental` and `parentBackupId` fields exist on the entity but are not yet reachable from the create endpoint, so every gateway-created backup is `full`.

## Goals / Non-Goals

**Goals:**
- Add a Backups page: list backups for the active project (most recent first), create a backup, track its async status, download when `ready`, and delete.
- Show status, progress, size, statistics, expiry, and integrity checksums.
- Reuse the session/API-key + project-scoping model from `add-auth-project-frame`.

**Non-Goals:**
- No restore UI (Memory returns 501; restore ships in Memory's "next phase", not here).
- No incremental backups UI (not creatable via the current create endpoint).
- No superadmin database-level backups (`/api/superadmin/database-backups`) — those are a separate privileged surface.
- No scheduled/automated backups or retention policy editing beyond the `retentionDays` create field.

## Decisions

### D1 — Backups live under `/backups` with a single page (list + create inline)

A list page with an inline create form and a per-row details/actions menu, following the existing documents/skills single-page pattern rather than a separate detail route per backup.

- **Why:** backup details are compact enough to render inline; one page keeps the surface minimal and mirrors `uiDocuments`/`uiSkills`. Download and delete act on a row without a separate detail screen.
- **Alternative rejected:** a dedicated `/backups/:id` detail page (more routes/templates for fields that fit in a row expander or modal).

### D2 — Dedicated `MemoryClient` backup methods using the `/api/v1/...` prefix

Add typed methods for list/create/get/download/delete in `memory.go` using `do`/`doH` with the `/api/v1/organizations/:orgId/backups...` and `/api/v1/projects/:projectId/backups` paths.

- **Why:** keeps the HTTP mechanics (auth header, error envelope, 4xx mapping) in one place, matching every other client method.
- **Alternative rejected:** inline `http` calls in handlers (breaks the established `MemoryClient` convention and the per-request credential refactor from `add-auth-project-frame`).

### D3 — Resolve `orgId` from the active project record

Backups' list/get/download/delete routes are **organization**-scoped, but the gateway's session carries the active **project**. The gateway SHALL obtain `orgId` before calling the org-scoped backup routes.

- **Web session:** resolve the org from the session context first (its `OrgID`), and when only the active project id is present, recover the org from the tenancy listing (`GET /api/projects`). The current-project lookup (`GET /api/projects/current`) is token-bound on the memory side and returns *no project* for a user access token, so it is **not** a valid bridge for web sessions.
- **API-key path:** resolve the org from the token-bound current-project lookup (the key is project-scoped, so it returns the project's `orgId`).
- **Why:** the org id is the only tenancy key those routes accept; the active project's `orgId` is the authoritative bridge from session → org.
- **Alternative rejected:** caching org id in the session cookie (expands the session payload and re-introduces a second tenancy value that can drift from the project).

### D4 — Download is a browser redirect, not a proxied stream

The gateway's download action SHALL redirect the browser to the presigned URL Memory returns (302), matching Memory's own `DownloadBackup` behavior.

- **Why:** the archive lives in object storage behind a signed URL; proxying the bytes through the gateway would add latency and memory pressure for large archives. Redirect preserves Memory's 1-hour expiry semantics.
- **Alternative rejected:** streaming the archive through the gateway (extra hop, no benefit; breaks the signed-URL flow).

### D5 — Restore is surfaced as unavailable, never attempted

Because Memory's restore endpoint is a hardcoded 501, the gateway SHALL NOT wire a restore action that would fail. The page either hides restore or renders it disabled with "coming in a later Memory release".

- **Why:** shipping a button that always errors is a worse experience than a truthful "not yet available" label; the backend cannot back the promise yet.
- **Alternative rejected:** implementing a gateway-side restore (not possible — no backend endpoint exists); silently dropping restore from the proposal (the proposal lists it, so the change must document the 501 reality explicitly).

### D6 — Feature-gated: only render when Memory exposes backups

The gateway SHALL treat a 404/not-found or an "unknown route" from the backups list as "backups unavailable" (empty/disabled state) rather than a hard error, because Memory gates the backup routes behind `Config.Features.Backups`.

- **Why:** a Memory deployment with the feature disabled must not show a broken page; the page degrades to an empty/unavailable state.
- **Alternative rejected:** assuming backups are always enabled (produces 502-style error pages on gated deployments).

## Risks / Trade-offs

- **[Restore is a 501]** the proposal lists restore, but Memory has not implemented it → Mitigation: D5 documents restore as unavailable; the spec requirement captures the behavior so future Memory support can be wired without a spec change beyond flipping the requirement.
- **[Async create needs polling]** create returns `creating` and the UI must refresh to reflect progress → Mitigation: poll `get` on an interval (or refresh-on-demand) only while any backup is `creating`; stop once terminal (`ready`/`failed`).
- **[Large download redirect]** the presigned URL is only valid for 1 hour and the download is a full archive → Mitigation: the UI triggers download immediately on click and warns that the link expires.
- **[Org-scoped routes + missing org]** if the active project has no `orgId`, the org-scoped routes cannot be called → Mitigation: resolve org from the current project and, if absent, surface a clear error state rather than a 500.
- **[Feature gate 404 ambiguity]** a genuine Memory error can look like "feature disabled" → Mitigation: distinguish `404`/not-found from other status codes; only 404 degrades to "unavailable", other failures surface as errors.

## Migration Plan

1. Land on top of `add-auth-project-frame` (session bearer token + `X-Project-ID`/`X-Org-ID` headers). No config keys are added by this change.
2. Add `MemoryClient` backup methods + `orgId` resolution; add the `/backups` page, route, and sidebar entry.
3. Ship behind the existing session model; no data migration — backups are purely a new read/write surface over Memory.
4. Rollback: revert the route/sidebar/template and the new client methods; no persisted gateway state to clean up.

## Open Questions

- Whether Memory will add a `backupType` (incremental) and `parentBackupId` to the create endpoint — when it does, the create form gains a type selector and the spec's "create a backup" requirement expands.
- Exact `Config.Features.Backups` default in the target deployment (gated on/off) — drives how prominent the "unavailable" state needs to be.
