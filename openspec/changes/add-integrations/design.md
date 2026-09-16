## Context

Alfred is a Go (echo + templ) web console for Emergent Memory. The previous Memory UI offered a GitHub App integration plus general third-party integrations; Alfred has no integration surface. This change adds it, building on `add-auth-project-frame`: every Memory call carries the signed-in user's Bearer token plus the active project via `X-Project-ID` (and `X-Org-ID` when known).

Memory exposes two distinct integration surfaces (see `/root/emergent.memory/openapi.yaml`, `docs/site/user-guide/integrations.md`, and `apps/server/pkg/sdk/integrations/client.go`):

- **GitHub App** — first-class OAuth flow under `/api/v1/settings/github/*`: `POST /connect` (returns an authorization URL), `GET /callback?code=...` (Memory exchanges the code and stores the credential), `POST /cli` (PAT for non-browser setups), `GET /` (status), `DELETE /` (disconnect), `POST /webhook` (out of scope).
- **General integrations** — provider-agnostic CRUD under `/api/integrations/*`: `GET /available` (catalog of installable types), `GET /` + `POST /` + `GET /{name}` + `PUT /{name}` + `DELETE /{name}`, plus `POST /{name}/test` and `POST /{name}/sync` (and `GET /{name}/sync/stream` for SSE progress). Settings are encrypted at rest (AES-256-GCM) and never returned in API responses.

## Goals / Non-Goals

**Goals:**
- Add a GitHub App connect (OAuth), status, and disconnect flow.
- Add general integration list (available vs configured), create/update, test, sync, and delete.
- Scope every integration view and action to the active project via the session model's project headers, requiring an authenticated session.
- Store nothing durable in the gateway: credentials and settings live only in Memory.

**Non-Goals:**
- No GitHub webhook handling, ClickUp-specific structure/spaces/folders pickers, or SSE sync-progress streaming in this slice.
- No data-source imports (Gmail/Drive/ClickUp content ingestion) — separate change (`add-data-sources`).
- No per-integration secret management UI in Alfred; the gateway forwards opaque settings and never reads them back.

## Decisions

### D1 — The OAuth callback and token exchange live on Memory, not Alfred

Alfred's connect action calls Memory `POST /api/v1/settings/github/connect`, receives an authorization URL, and redirects the browser to it. GitHub's redirect lands on Memory's `GET /api/v1/settings/github/callback`, which exchanges the code and stores the credential. Alfred then reads status (`GET /api/v1/settings/github`) to reflect the result.

- **Why:** Memory already owns all durable, encrypted state; the `persist-nothing` principle means Alfred should never touch the GitHub `code` or token. This also keeps the `redirect_uri` registered with the GitHub App pointed at Memory, unchanged from the prior UI.
- **Alternative rejected:** an Alfred-hosted `/github/callback` that exchanges the code server-side and forwards the token to Memory. Rejected because it would require the GitHub App's `redirect_uri` to point at Alfred, briefly place a secret in Alfred's memory/process, and duplicate credential handling.

### D2 — No credential storage in Alfred

The gateway forwards integration settings to Memory as opaque JSON and stores nothing locally. It never reads settings back (Memory omits them from responses) and never logs them.

- **Why:** preserves the "Memory owns all durable state" invariant and avoids a second encryption-at-rest scheme and secret-audit surface in the gateway.
- **Alternative rejected:** a local settings store (SQLite/file) in Alfred for integration config. Rejected because it duplicates Memory's encrypted store, splits the source of truth, and complicates rollback/consistency.

### D3 — Two distinct client method groups: GitHub App vs general integrations

Implement separate `MemoryClient` method groups mirroring Memory's two API shapes: `GitHubConnect`/`GitHubStatus`/`GitHubDisconnect` (optionally `GitHubCLIToken`) against `/api/v1/settings/github/*`, and `ListAvailableIntegrations`/`ListIntegrations`/`GetIntegration`/`CreateIntegration`/`UpdateIntegration`/`TestIntegration`/`SyncIntegration`/`DeleteIntegration` against `/api/integrations/*`. Both are added to the `MemoryBackend` interface so handlers stay testable with the existing fake.

- **Why:** the two surfaces have different path prefixes (`/api/v1/settings/github` vs `/api/integrations`) and different semantics (OAuth flow vs CRUD), so mirroring them keeps each method trivially correct.
- **Alternative rejected:** collapsing GitHub into the generic integration CRUD. Rejected because Memory does not model GitHub App connect as a generic `CreateIntegration`; it is a dedicated OAuth flow with no equivalent settings object Alfred could express.

### D4 — Available vs configured rendered as two lists on one screen

The integrations screen fetches `GET /api/integrations/available` (catalog: name, display name, description, category, auth type, required/optional fields) and `GET /api/integrations` (configured instances), and renders the catalog with connect/configure affordances plus the configured instances with test/sync/delete actions.

- **Why:** Memory distinguishes "installable type" from "configured instance"; the user needs both to see what is set up and what can be added.
- **Alternative rejected:** a single list of configured instances only. Rejected because it hides installable integrations the user has not yet configured.

### D5 — Test / sync / delete use name-scoped PRG form posts

UI actions for test, sync, and delete POST to name-scoped routes (mirroring existing PRG patterns in `ui.go`) and redirect back to the integrations screen with a flash toast on success or `?err=` on failure.

- **Why:** matches the gateway's existing create/update/delete UX convention (see skills, settings) and keeps the page stateless.
- **Alternative rejected:** JSON API endpoints for every action with client-side fetch. Rejected as unnecessary — the management console already uses form-post PRG for mutations.

### D6 — Project scoping via session headers

Integration requests reuse the session token and send `X-Project-ID` (and `X-Org-ID` when known), matching `add-auth-project-frame` D6. The GitHub App endpoints are likewise project-scoped.

- **Why:** integration state is per-project; the header is Memory's multi-project selector for interactive (non-`emt_*`) tokens.
- **Alternative rejected:** minting separate credentials per integration request (unnecessary; Memory already scopes by header).

## Risks / Trade-offs

- **[OAuth post-callback return path]** the GitHub App's `redirect_uri` targets Memory, and it is not yet confirmed how the browser returns to Alfred after the callback. → Mitigation: Alfred reads status on page load; flag the exact redirect-back behavior as an Open Question and verify against a live Memory during apply.
- **[DTO field-name drift]** docs use camelCase (`enabled`, `displayName`, `settings`) while the Go SDK uses snake_case (`display_name`, `provider_type`, `config`). → Mitigation: keep the spec behavior-level; pin the exact wire shape by inspecting a live Memory during apply before finalizing the client structs.
- **[Credential leakage]** settings must never round-trip through Alfred. → Mitigation: a spec requirement forbids returning or logging credentials; Memory omits settings from responses and the gateway never re-sends stored values it cannot read.
- **[Endpoints still maturing]** the GitHub/integrations routes are documented but not all present in the current server source tree. → Mitigation: treat OpenAPI + docs as the contract; keep the feature additive and behind the existing session gate so an unreachable endpoint degrades to an error state, not a crash.
- **[Path-prefix confusion]** `/api/v1/settings/github` vs `/api/integrations` invites copy-paste mistakes. → Mitigation: separate method groups with explicit full paths and unit tests against the fake backend that assert the exact URL.

## Migration Plan

1. Add the new `MemoryBackend` methods and `MemoryClient` implementations (additive; no existing method changes).
2. Add the integrations handlers + routes and a "Settings → Integrations" sidebar entry; no new config keys — reuse the session token and project headers from `add-auth-project-frame`.
3. Ship behind the existing session gate; the screen renders empty/error states if Memory lacks the endpoints.
4. Rollback: remove the route + sidebar entry and the new methods; no data to migrate or revert in the gateway (all state remains in Memory).

## Open Questions

- How does Memory return the browser to Alfred after the GitHub callback (redirect target, base URL, success/failure query params)? Needed to wire a clean post-connect UX.
- Exact wire shape for create/update DTOs (`enabled`/`displayName`/`settings` vs `display_name`/`provider_type`/`config`) — confirm against a live Memory before finalizing the client structs.
- Should sync progress use the SSE `/{name}/sync/stream` endpoint now, or ship fire-and-forget sync first and stream later?
