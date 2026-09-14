## Context

See proposal.md — Why. The gateway is an MCP **client/manager**, not a server: it proxies the Memory admin MCP registry over REST (`gateway/mcp_servers_client.go`, models in `gateway/extras.go:256`) and reads relay sessions (`gateway/mcp_relay.go`). Its auth boundary is `requireSession` for `/settings/*` and `requireSessionOrKey` for `/api/*` (`gateway/auth.go:477,498`). Project scoping is carried by `sessionHeaders` (`X-Project-ID`/`X-Org-ID`, `gateway/memory.go:66-80`). Settings pages are rendered with templ and follow a page-title/test-id convention (`gateway/mcp_servers.templ`, `gateway/settings-nav`). The MCP Servers page already lives at `/settings/mcp-servers`.

The new backend change `add-mcp-share-instances` (emergent.memory) defines the authoritative share-instance and tool-catalog HTTP API this UI consumes.

## Goals / Non-Goals

**Goals:**
- A management surface that makes the backend share API usable without hand-crafted requests.
- Keep the gateway thin: translate UI actions to backend calls; own no share state.
- Preserve the existing auth and project-scoping model.

**Non-Goals:**
- Backend enforcement, token scoping derivation, or migration (owned by the backend change).
- Managing relay nodes or registry servers (existing MCP Servers page).
- Client-side OAuth for MCP consumers.

## Decisions

### 1. Proxy through the gateway, not browser-to-backend

**Decision:** Add gateway routes under `/api` that call the Memory share API via the existing memory client, mirroring `mcp_servers_client.go`.

**Rationale:** Reuses the shared client's per-request session token + `X-Project-ID` propagation, keeps the cookie/API-key boundary, and avoids CORS/credential plumbing in the browser.

**Alternatives considered:** Direct browser calls to the Memory API — would leak tokens into the browser and duplicate auth; rejected.

### 2. Page nesting under MCP servers

**Decision:** Serve the page at `/settings/mcp-servers/shares` with create/edit sub-routes, and add an entry-point link from the MCP Servers page rather than a new top-level settings-rail section.

**Rationale:** Sharing and consuming MCP are one mental model; nesting avoids expanding the global settings rail and keeps the change small. Changing the page requirement is captured as an ADDED requirement on `mcp-servers-ui`.

**Alternatives considered:** A new settings-rail section — more visible but increases nav churn; deferred.

### 3. One-time key reveal only in create/rotate responses

**Decision:** The gateway returns the raw key only for create and rotate; the list/get representations never include it. The UI shows a reveal modal with copy buttons and snippets and does not persist the key.

**Rationale:** Matches backend semantics (token returned once via `TokenEncrypted` only at creation) and avoids storing secrets in gateway memory or logs.

### 4. Tool and agent pickers fed by backend catalog/agent lists

**Decision:** The create/edit form loads the tool catalog from the backend catalog endpoint (grouped by category, searchable) and agents from the existing agent list. Selected tool names/agent IDs are sent as arrays on create/update.

**Rationale:** Server-enforced set == picker set; no gateway-side copy of the tool taxonomy to drift.

### 5. Agent "Share via MCP" as a deep link with preselection

**Decision:** An agent action navigates to the create form with the agent ID in the query string; the form preselects it.

**Rationale:** Minimal code, no separate flow, and the create path stays single-sourced.

## Risks / Trade-offs

- [Backend change not yet deployed → UI errors] → Surface the backend error state; deploy backend first. Treat 404/unsupported as "sharing unavailable" rather than a broken page.
- [Raw key exposure via logs/telemetry] → Never log request/response bodies on create/rotate; render the key only in the reveal modal.
- [Duplicate-name / validation errors mapped poorly] → Map backend 409/422 to inline form errors with the backend message.
- [Templ/CSS regeneration missed] → Run `templ generate` + build in the task checklist; verify in browser.
- [Stale list after rotate/revoke] → Re-fetch on action completion; last-used updates may lag and are shown as backend-reported.

## Migration Plan

1. Land the backend change and confirm the share + catalog endpoints exist.
2. Add the gateway client, routes, page, and entry points behind the same deploy.
3. **Rollback:** remove routes/templates; no gateway state or DB to undo. Backend instances remain, managed via API.

## Open Questions

- Exact snippet set to show (Claude Desktop/Code, Cursor, Cloud Code) can be finalized during implementation; the backend already returns snippets that the UI can display verbatim.
