## Context

See proposal.md — Why. The gateway already proxies the memory MCP registry and share APIs (see the sibling changes `add-mcp-share-management` and `add-mcp-share-instances`) and renders agent-facing pages with templ (`gateway/agent.templ`). Auth is `requireSession` for `/settings/*` and `requireSessionOrKey` for `/api/*`; project scoping rides on `X-Project-ID`.

The backend change `add-agent-mcp-endpoint` defines the contract this UI consumes: a per-agent MCP endpoint `/api/mcp/agents/:agentId` with a single `call_agent` tool, and project-admin endpoints to create/list/revoke/rotate a share credential bound to that agent.

## Goals / Non-Goals

**Goals:**
- Make the per-agent MCP credential usable without hand-crafted API calls.
- Reuse the existing gateway patterns and auth boundary; own no share state.

**Non-Goals:**
- The MCP endpoint/enforcement itself (backend).
- More than one tool per agent; async/polling; conversation continuity.

## Decisions

### 1. Proxy through the gateway

**Decision:** Add gateway routes under `/api` that call the memory per-agent share endpoints via the shared client, mirroring `mcp_shares.go`/`mcp_servers_client.go`.

**Rationale:** Keeps the cookie/API-key boundary and per-request project scoping; avoids browser-to-backend credential plumbing.

### 2. Entry point on the agent view, list nested by agent

**Decision:** The primary action lives on the agent ("Share as MCP tool"); the shares list is scoped to that agent (sub-route of the agent sharing area or an agent-filtered section).

**Rationale:** An agent share is meaningless without its agent; co-locating avoids a separate top-level settings section and matches the "one agent = one server" mental model.

### 3. One-time key reveal only from create/rotate

**Decision:** The gateway returns the raw key only for create and rotate; list/get never include it. The UI reveals it once with copy buttons and snippets and does not persist it.

**Rationale:** Matches backend semantics and avoids storing secrets in the gateway or logs.

### 4. Snippets generated gateway-side unless the backend supplies them

**Decision:** Build client config snippets (Claude Desktop/Code, Cursor, Cloud Code) from the per-agent endpoint URL and key, reusing the share-management snippet helper if present.

**Rationale:** The endpoint URL shape is known and stable; avoids a backend dependency for presentation.

## Risks / Trade-offs

- [Backend change not deployed] → Show a clear "sharing unavailable" state; deploy backend first.
- [Key leakage via logs/telemetry] → Never log create/rotate bodies; render the key only in the reveal.
- [Duplicate/validation errors mapped poorly] → Map backend 409/422 to inline form errors with the backend message.
- [Snippet drift as client config formats change] → Centralize snippet generation so one update fixes all.

## Migration Plan

1. Land the backend `add-agent-mcp-endpoint` change and confirm endpoints exist.
2. Ship the gateway client, routes, view, and entry point together.
3. **Rollback:** remove routes/view/action; no gateway state or DB to undo. Backend shares remain manageable via API.

## Open Questions

- Whether the shares list is a dedicated page or an inline panel on the agent view can be settled during implementation without changing the spec.
