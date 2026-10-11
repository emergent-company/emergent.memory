## Context

The Data Sources surface (gateway `datasources.go` / `datasources_ui.go` / `datasources.templ`) proxies Memory's `data-source-integrations` REST API and adds a document version-history sub-page. The live Playwright suite (`playwright.config.ts`) runs against the real dev installation (session-mode gateway + Zitadel + dev memory) and cannot produce a Drive source without real OAuth credentials (the feature's first iteration takes an operator-supplied refresh token, `add-google-drive-ingestion` §7.2). The suite already has a hermetic, no-auth mock harness (`run-e2e.sh` + `mock-memory.mjs`, used by `share.config.ts` and `dev-gallery.config.ts`) that points the gateway at a stateful mock memory.

## Goals / Non-Goals

**Goals:**
- Deterministic browser coverage of the data-sources UI: list/empty state, schema-driven connect form + inline config test, source creation → detail, sync trigger/progress/cancel/failure, and document version history (including `deleted_from_source`).
- No Google credentials, no OIDC session, no live network.
- Reuse the existing mock-harness pattern rather than a new server.

**Non-Goals:**
- Exercising the real Drive API or the server `domain/sources` worker (covered by `apps/server` unit/integration tests).
- Migrating the whole suite to hermetic.
- Asserting styling — the contract is testids, text, and routes.

## Decisions

### D1 — Hermetic mock harness, not the live session suite
Add `datasources.config.ts` (a `webServer` running `run-e2e.sh`, baseURL `http://localhost:8097`, serial workers) mirroring `share.config.ts`/`dev-gallery.config.ts`. The specs are excluded from the live `chromium` project via `testIgnore`.

- **Why:** the connect/sync flows need a stateful upstream; the live suite can't synthesize a Drive source. The harness already exists and is proven.
- **Alternative rejected:** a live-Drive scenario — needs secrets, is flaky, and can't run in CI.

### D2 — Sync outcomes keyed off `folder_id`
The mock creates a completed job by default, an in-progress job for `folder-slow`, and a failed job for `folder-fail`.

- **Why:** the connect form's only free-text lever is the folder, so it gives each spec a deterministic, credential-free way to reach every sync state (pending/processing/cancel, failed, completed).
- **Alternative rejected:** separate mock control endpoints — more surface, and the form can't set them through the UI.

### D3 — Version history is the observable outcome of a new-version sync
The document version-history sub-page (`/documents/:id/versions`) is asserted directly against seeded multi-version documents; the mock seeds a two-active-version document and a `deleted_from_source` document.

- **Why:** the data-sources list/detail surface does not render per-document versions, so the versions sub-page is the only user-visible proof of the versioning contract.
- **Alternative rejected:** driving a full sync→version chain through the mock — the mock has no ingestion worker, and the UI exposes no version link from the sync view.

### D4 — Credentials stay write-only in the mock
The mock stores a source's config server-side and never echoes it, so the UI's "leave blank to keep credentials" contract is honored and no spec can accidentally depend on credentials being returned.

- **Why:** mirrors the real server contract; keeps the mock honest.

## Risks / Trade-offs

- **[Shared stateful mock]** specs could race the source store → Mitigation: `workers: 1` (serial) and every spec deletes the sources it creates.
- **[Unwarmed worktree]** `run-e2e.sh` builds the gateway, which needs generated templ → Mitigation: `run-e2e.sh` runs `templ generate` when `*_templ.go` is absent.
- **[Mock drifts from the real API]** the mock's DTOs are hand-written → Mitigation: shapes copied from `gateway/memory_datasources.go`; the server side stays covered by its own tests.

## Migration Plan

Additive test assets only; no deployment or rollback concerns. Remove the config/specs/helper and the mock routes to revert.

## Open Questions

- Should the remaining session-mode surfaces be migrated onto this hermetic harness over time? Tracked with `web-ui-e2e-coverage`, not attempted here.
