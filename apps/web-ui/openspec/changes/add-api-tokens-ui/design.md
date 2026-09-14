## Context

The Alfred gateway renders its web UI with `templ` + go-daisy (daisyUI) and talks to Emergent Memory over REST through `MemoryClient` (`gateway/memory.go`). Navigation is a sidebar driven by `sidebarGroups()` in `gateway/ui.go`; pages follow the `s.page(c, title, component)` pattern. The Settings sidebar group holds Project, Blueprints, and Skills. The account dropdown (`account_menu.templ`) links to `/profile`. This change builds on `add-auth-project-frame` (Zitadel OIDC session auth; Memory calls carry the user's Bearer token + `X-Project-ID` header for the active project).

Emergent Memory already implements API-token management; no Memory-service change is required. Endpoints (from `/root/emergent.memory/apps/server/domain/apitoken/routes.go`, `handler.go`, `service.go`, `entity.go`, and `pkg/sdk/apitokens/client.go`):

- **Project-scoped** (`/api/projects/{projectId}/tokens`): `POST` (create), `GET` (list), `GET /{tokenId}` (get, may include decrypted plaintext), `PATCH /{tokenId}` (update scopes), `DELETE /{tokenId}` (revoke), `POST /{tokenId}/regenerate`.
- **Account-level** (`/api/tokens`, `project_id = NULL`, user-bound): `POST`, `GET`, `GET/PATCH/DELETE /{tokenId}`, `POST /{tokenId}/regenerate`.

Tokens are `emt_*`-prefixed (68 chars; `tokenPrefix` = first 12), scoped, revocable, stored as a SHA-256 hash. The plaintext is returned at creation and on regenerate, and **also via `GET /:tokenId` when encryption is configured** (`encryption.Service.IsConfigured()` → `INTEGRATION_ENCRYPTION_KEY` set and ≥32 chars). List responses return metadata only (`id`, `name`, `tokenPrefix`, `scopes`, `createdAt`, `lastUsedAt`, `isRevoked`, `expiresAt`). The create body is `{ name (1–255), scopes (≥1) }`.

Supported scopes (`ValidApiTokenScopes`, 23 total): coarse `schema:read/write`, `data:read/write`, `agents:read/write`, `projects:read/write`, `chat:use`; fine-grained `graph:read/write`, `schema:migrate`, `branches:read/write`, `search`, `journal:read/write`, `skills:read/write`, `documents:read/write`, `admin`, `admin:all`.

**Auth guard nuance:** the project-scoped `list/get/patch/delete/regenerate` routes sit under `RequireAPITokenScopes("project:read")`, which is a no-op for Zitadel/OAuth sessions (only enforced for `emt_*` callers). `POST …/tokens` (create) is registered *outside* that group — auth + membership only, so a project member can bootstrap tokens without the `project:read` scope. Account-level routes require only `RequireAuth()`.

**Server-side gates the UI must surface:**

| Condition | Result |
|---|---|
| `admin:all` scope w/o org-admin/superadmin | 403 `admin-all-scope-denied` |
| `project_viewer` + write scope (create/patch/regenerate) | 403 `viewer-write-scope-denied` |
| duplicate name (per user + project / per user) | 409 `token_name_exists` |
| revoke/regenerate already-revoked | 409 `token_already_revoked` |
| unknown scope / empty name / no scopes | 400 |

## Goals / Non-Goals

**Goals:**

- One project-scoped tokens page (Settings group) and one account-level tokens page (on `/profile/tokens`, reached from the profile sub-navigation) that each list in a table, create, revoke, regenerate, and edit scopes of tokens — with create and scope-edit on dedicated pages.
- Show the plaintext exactly once at creation and on regenerate (never on demand).
- `MemoryClient` methods for the full verb set on both surfaces, tested with `httptest`.
- Surface server-side errors (409 duplicate name, 403 viewer/admin gates, 400 invalid scope) as readable messages.
- Keep the existing shared `X-API-Key` programmatic path working unchanged.

**Non-Goals:**

- No token expiry management (separate follow-up).
- No `admin:all` grant UI for users who cannot hold it (org-admin/superadmin only; surface the 403 otherwise).
- No changes to the Memory service (separate repo, out of scope).
- No new persistence in the gateway; Memory remains the source of truth.

## Decisions

### 1. Routes and navigation

Project-scoped tokens live in the Settings sidebar group at `/settings/tokens`. Account-level tokens live on a dedicated `/profile/tokens` page reached from a `profileSubNav` rail (Profile / Invitations / API tokens) mirroring `settingsSubNav` — the profile page is split into three pages (account tokens are user-bound, not project-bound, so they sit in the profile area, not project settings).

The token list is a `<table>` (mirroring `blueprints.templ`), with create and scope-edit on separate pages. Both surfaces share one table + create-page + edit-page component, parameterized by base path.

Project routes:
- `GET /settings/tokens` — table list + "New token" action.
- `GET /settings/tokens/new` / `POST /settings/tokens/new` — create page (success re-renders with the one-shot plaintext panel; validation/Memory errors PRG back with `?err=`).
- `GET /settings/tokens/:tokenId/edit` — edit-scopes page (scope checkboxes pre-checked).
- `POST /settings/tokens/:tokenId/scopes` — save scopes, PRG (error → back to the edit page).
- `POST /settings/tokens/:tokenId/regenerate` — render the list page with the one-shot plaintext panel on top, no redirect.
- `POST /settings/tokens/:tokenId/revoke` — PRG.

Account routes mirror this at `/profile/tokens` (`GET/POST /profile/tokens/new`, `GET /profile/tokens/:tokenId/edit`, `POST /profile/tokens/:tokenId/{scopes,regenerate,revoke}`). Profile split: `GET /profile` (identity + edit form), `GET /profile/invitations` (pending invites), `GET /profile/tokens` (account tokens table). `auth.go` `projectScopePath` exempts the `/profile/` prefix so these pages are not treated as project-scoped.

### 2. Scoping: session project vs session user

Project tokens resolve `projectID` from the signed-in session's `active_project_id` and send the Bearer token + `X-Project-ID` header. Account tokens are scoped to the authenticated user only (no project header) via `/api/tokens`. Both reuse `MemoryClient.tokenFor(ctx)` / `projectIDFor(ctx)` / `sessionHeaders(ctx)`.

### 3. Full verb set on both surfaces

Implement six project + six account `MemoryClient` methods: `ListAPIToken(s)`, `CreateAPIToken`, `GetAPIToken`, `UpdateAPITokenScopes`, `RevokeAPIToken`, `RegenerateAPIToken` (and `*AccountAPIToken` equivalents). Rationale: the Memory SDK and server already expose every verb; the marginal cost is low and the user asked for full management.

### 4. Show-once create and regenerate

Create and regenerate render the plaintext exactly once in a one-shot panel with a copy affordance (no redirect, so the secret is not lost). Rationale: Memory returns plaintext only at create/regenerate; afterward the value cannot be shown again in the UI, so there is no on-demand action.

### 5. Error surfacing

Map Memory error responses to readable messages: 409 duplicate name, 403 viewer-write-scope-denied and admin-all-scope-denied, 400 invalid scope. The scope picker omits `admin:all` unless the caller can hold it (else the 403 is surfaced). A `project_viewer` is only offered read-only scopes (data:read, schema:read, agents:read, projects:read).

### 6. Testing (TDD)

Unit-test every `MemoryClient` method with `httptest` (mirroring `memory_test.go`), handler happy/error paths with the `handlers_test.go`/`settings_handlers_test.go` pattern, and `.templ` render tests (mirroring `project_settings_ui_test.go`). E2E is out of scope per project convention.

## Risks / Trade-offs

- [Plaintext token in the DOM] a copyable secret sits briefly in the create/regenerate response pages → Mitigation: render only in one-shot views, never in list/detail, and prefer `clipboard` copy without embedding in query strings.
- [Account tokens span all the user's projects] an account-level token is not project-bound and can act across projects → Mitigation: label account tokens distinctly and warn that they are account-wide.
- [Token lacks `project:read` when listing via an `emt_*` token] the `RequireAPITokenScopes("project:read")` guard rejects list for API-token callers → Mitigation: for the interactive session path this is a no-op; surface a 403 as a clear error rather than a crash.
- [Memory unreachable] → Mitigation: pages render an error state and do not treat partial data as authoritative (spec requirement).
- [Revoke/regenerate are destructive and immediate] → Mitigation: require explicit confirmation on the revoke and regenerate forms before submitting.

## Migration Plan

- Additive only: new routes, new client methods, a new `.templ` page (project) plus a `/profile` section (account), one sidebar item. No data migration.
- The shared `X-API-Key` path is untouched; `add-auth-project-frame`'s session model already provides the credential + project context these pages need.
- Deploy via the normal gateway build (`task dev`/`task build`); rollback is a revert of the change.
