## Context

See `proposal.md` — Why. Alfred's gateway proxies the Emergent Memory REST API and currently has no data-source support. Memory exposes a data-source-integrations surface (providers, config schema, connection testing, sync trigger/monitor/cancel) plus a document-scoped discovery-jobs surface for inferring object/relationship types. This change wires Alfred to those endpoints under the session/project model from `add-auth-project-frame` (signed-in user token + `X-Project-ID` header). The authoritative API shape is Memory's full swagger (`docs/swagger/swagger.yaml`) and SDK client (`apps/server/pkg/sdk/datasources/client.go`), since the data-source routes are absent from the root `openapi.yaml`.

## Goals / Non-Goals

**Goals:**
- Connect, list, update, and delete data-source integrations (IMAP, Gmail OAuth, Google Drive, ClickUp).
- Test a provider configuration and an existing integration's connection before/after save.
- Trigger, monitor (list/latest/detail), and cancel sync jobs.
- Start, monitor, and finalize schema-discovery jobs.
- Keep provider credentials out of gateway responses and logs.

**Non-Goals:**
- No OAuth authorization/callback endpoint in the gateway — Memory's data-source API has no documented OAuth callback surface (see Open Questions).
- No background sync scheduler, push/SSE progress streaming, or cross-project aggregation in Alfred.
- No change to iOS or the supervisor/bridge workers.

## Decisions

### D1 — Mirror the data-source-integrations REST surface with new `MemoryClient` methods

Add client methods for providers, provider schema, integration CRUD, test-config/test-connection, sync trigger, sync-job list/latest/detail/cancel, and discovery start/status/list/finalize — all over Memory's REST endpoints, not the MCP bridge.

- **Why:** the data-source endpoints are plain REST (bearer + `X-Project-ID`), matching the existing document/settings client patterns (`do`/`doH`); no JSON-RPC indirection needed.
- **Alternative rejected:** route through the MCP tools — the MCP surface doesn't expose data-source management, only memory entity queries.

### D2 — Project scoping via `X-Project-ID` header

Data-source calls carry `X-Project-ID` (and `X-Org-ID` for discovery finalize/start) on the request, exactly like `documentHeaders()`.

- **Why:** the swagger marks `X-Project-ID` as a required header on every data-source route; the session's active project supplies it, consistent with `add-auth-project-frame` D6.
- **Alternative rejected:** embedding the project id in the path — the endpoints don't accept it there.

### D3 — Schema-driven connect form

The connect/edit form renders from the provider's JSON config schema (`GET .../providers/{type}/schema` → `{type, properties, required[]}`), rather than hardcoding per-provider fields.

- **Why:** four providers with divergent configs (IMAP host/port/creds, OAuth client fields, ClickUp token) are best handled generically; new providers render without code changes.
- **Alternative rejected:** four bespoke templ forms — more code, and it drifts the moment Memory adds a provider.

### D4 — Connection testing before save, and on demand

Expose `POST .../test-config` (unsaved config) and `POST .../{id}/test-connection` (stored config) as explicit actions surfaced in the UI.

- **Why:** both endpoints exist precisely for this; surfacing both matches the docs' "test the connection before saving" guidance and lets users re-validate after credentials rotate.
- **Alternative rejected:** testing implicitly on create only — hides a dedicated endpoint and makes credential-rotation diagnosis harder.

### D5 — Sync monitoring via polling, not streaming

Monitor sync jobs by fetching `.../sync-jobs/latest` (or a specific job) on demand/interval.

- **Why:** the data-source API has no SSE/push for sync progress — the SSE `.../sync/stream` route belongs to the retired `integrations` system, not `data-source-integrations`. Polling the latest job is the only documented path.
- **Alternative rejected:** SSE streaming against the retired integration endpoint — wrong surface, would not reflect `data-source-integrations` jobs.

### D6 — Provider OAuth is a pass-through config, not a gateway flow

For `gmail_oauth` / `google_drive`, the gateway forwards whatever config the provider schema requires (e.g. OAuth client id/secret) and never implements its own authorize/callback. The user completes any out-of-band OAuth setup; Alfred only stores the resulting config via Memory.

- **Why:** the data-source API surface we researched has no `authorizationUrl`/callback endpoint (the `requiresOAuth` capability flag belongs to the retired `domain_integrations` system). Building a gateway callback against an undocumented endpoint would be speculative.
- **Alternative rejected:** implementing a gateway OAuth redirect/callback — blocked on a Memory endpoint that doesn't exist in the current API.

### D7 — Credentials are write-only through the gateway

The gateway never reads config back: the `DataSourceIntegrationDTO` response omits `config`, so stored secrets are not round-tripped to the client. The update path only sends config when the user re-enters it.

- **Why:** minimizes secret exposure and respects Memory's encrypted-at-rest model; the gateway persists nothing.
- **Alternative rejected:** caching config locally to pre-fill edit forms — violates the persist-nothing principle and exposes secrets.

### D8 — Discovery is document-scoped (swagger, not the user-guide)

Start discovery by `document_ids` (`POST /discovery-jobs/projects/{projectId}/start`), not by `integrationId`. Finalize maps selected discovered types into a template pack (`mode: create|extend`).

- **Why:** the swagger `StartDiscoveryRequest` requires `document_ids` and has no `integrationId`; the user-guide's `{"integrationId": ...}` snippet is stale. Following the swagger keeps the client correct.
- **Alternative rejected:** keying discovery off an integration id — not accepted by the current endpoint.

## Risks / Trade-offs

- **[Provider secrets in request bodies]** config (OAuth client secrets, IMAP passwords) transits the gateway on POST/PATCH → Mitigation: TLS end-to-end, no gateway-side logging of config bodies, config never echoed back in responses (D7).
- **[Long-running sync jobs + polling load]** syncs can be slow and the page polls the latest job → Mitigation: poll `.../sync-jobs/latest` only while a job is pending/running, with a modest interval and user-triggered refresh.
- **[Discovery finalize creates a template pack]** an accidental finalize mutates the type registry → Mitigation: finalize is an explicit two-step action (review discovered types → finalize) and defaults to a non-destructive `create` mode; `extend` is opt-in.
- **[Deleting a source is not reversible]** but documents persist → Mitigation: confirm dialog noting imported documents are kept, mirroring Memory's own note.
- **[OAuth providers need out-of-band setup]** gmail/drive credentials aren't obtainable through Alfred → Mitigation: document the config the provider schema requires; keep this a pass-through (D6) and track it as an open question.
- **[Stale docs vs. swagger]** the user-guide's discovery payload disagrees with the swagger → Mitigation: treat `docs/swagger/swagger.yaml` + SDK client as authoritative (D8).

## Migration Plan

1. Add the data-source client methods + DTOs to `gateway/` behind the existing `MemoryClient` (additive; no config/env changes).
2. Add handlers + routes and the templ pages; register the Data Sources item in the sidebar.
3. No data migration or config change; the feature is inert until the routes are wired and the sidebar entry is added.
4. Rollback: remove the routes/sidebar entry and the client methods — no persisted state in the gateway to clean up.

## Open Questions

- OAuth authorize/callback flow for `gmail_oauth` / `google_drive`: does Memory return an authorization URL from create, or must the user supply OAuth credentials entirely out-of-band? (D6 assumes pass-through until a callback endpoint is documented.)
- Is there a push/SSE channel for sync-job progress beyond polling `.../sync-jobs/latest`? (D5 assumes polling only.)
- Semantics of `Provider.available=false` — should the gateway hide or disable such providers in the connect flow?
