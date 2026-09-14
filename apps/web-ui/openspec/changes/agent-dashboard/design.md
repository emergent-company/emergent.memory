## Context

The Go web UI (`ui/`) is a server-side-rendered app (templ + echo + go-daisy) that already proxies two upstreams through `Backends`: the control-plane API (`API_BASE_URL`, `:8081`, `GET /api/agents`, `GET /api/mcp-servers`) and the admin server (`ADMIN_URL`, `:8080`, `GET /api/sessions`, `GET /api/session?room=`, `GET /api/memories`, `GET /api/memories/capability`), all with the shared `X-API-Key`. It renders a flat agents table (`/agents`) and a global session log (`/sessions`), with no per-agent view. See proposal.md for motivation.

The agent's full configuration (backend, `tools` = `{mcp: [{mcp_server_id, allowlist}], functions: [...]}`, `sub_agents`, `routing`) is served by the control plane's `GET /api/agents/{id}`. Memory lives in the Emergent Memory service, already proxied by `agent/admin.py` through `GET /api/memories?agent=&query=` (searchable list of `{id, content, category, confidence}`) and `GET /api/memories/capability?agent=`; an agent "has memory" when its `tools.mcp` references the server named `memory`. The iOS app already consumes these same endpoints for its memory browser, which this change mirrors on the web.

## Goals / Non-Goals

**Goals:**

- Add a per-agent dashboard route in the Go UI that shows summary, configured tools, and recent chats, and links to a memories subpage.
- Mirror the iOS memory browser on the web: a searchable list of memories with a detail view, reusing the existing memory endpoints unchanged.
- Reuse the existing control-plane and admin endpoints rather than building parallel data paths.

**Non-Goals:**

- No memory editing/creation from the UI (read-only, per spec).
- No new memory backend surface — the existing `/api/memories` and `/api/memories/capability` are consumed as-is.
- No new agent-config editing surface here — editing stays on the existing control-plane flows (out of scope for this change).
- No real-time streaming; the dashboard and memories subpage pull on load.

## Decisions

### 1. Dashboard route is `/agents/{id}` (by control-plane agent id), sidebar unchanged

The agents table already has each row keyed by `a.ID`. Make the name cell a link to `/agents/{id}`. The sidebar keeps `Agents`/`Sessions`; the dashboard is a child of `Agents`.

- **Alternative considered:** key by agent name. Rejected — the control plane identifies agents by `id`; name can be edited and is not a stable key.

### 2. Agent detail and tool names come from the control plane, not admin.py

`GET /api/agents/{id}` returns the full agent (backend, tools, sub_agents, routing). MCP server names are resolved by fetching `GET /api/mcp-servers` and mapping `mcp_server_id → name`. The UI presents two tool groups: MCP tools (server name + allowlist) and built-in functions (`tools.functions`).

- **Alternative considered:** add a `tools` projection to the existing `/api/agents` list so the dashboard needs no second fetch. Rejected — the detail endpoint already returns everything; a parallel projection risks drift.

### 3. Recent chats = sessions filtered by agent, server-side

Sessions are keyed by `room`, and the room→agent association is the naming convention already used by token minting (`room.startswith(agent.name + "-")`). Add an optional `agent` query param to `GET /api/sessions` that filters to rooms matching that prefix; the dashboard calls it once.

- **Alternative considered:** filter client-side in the Go UI after fetching all rooms. Rejected — it pulls every agent's rooms into the payload and duplicates the room-prefix rule in Go.
- **Limitation accepted:** rooms not carrying an agent prefix (e.g. the legacy `alfred-persistent`) won't appear on any dashboard. That matches the existing `_room_allowed` behavior and is not a regression.

### 4. Memories subpage reuses `/api/memories` + `/api/memories/capability` unchanged

No new memory backend. The subpage calls `GET /api/memories/capability?agent=` to decide the entry point, then `GET /api/memories?agent=&query=` to list (empty query) or search (query present). The response shape (`{memories: [{id, content, category, confidence}]}`) mirrors the iOS models exactly.

- **Alternative considered:** add a web-specific memory endpoint. Rejected — the iOS endpoint already returns the shape the browser needs; duplicating it for the web invites drift between the two clients.

### 5. Memories subpage is a separate route `/agents/{id}/memories`

A search input drives the same server-side search the iOS `.searchable` triggers; the list shows content/category/confidence, and selecting an item opens a detail view with full content. A back link returns to the dashboard. Keeping it a distinct route (not a fragment) makes it a stable link and keeps the dashboard load light.

## Risks / Trade-offs

- **Cross-host memory dependency (Tailscale)** → memory outage makes the memory fetch error; the subpage renders an error state, never a crash. Mitigation: reuse the existing 502 path from `/api/memories`.
- **Room-prefix scoping is heuristic** → sessions in shared/non-prefixed rooms won't show on a dashboard. Mitigation: documented limitation, consistent with token minting.
- **Large memory sets** → `/api/memories` is bounded (limit 100 today); the browser shows that bounded window. Mitigation: accept the cap; note it in the subpage ("showing latest N").

## Migration Plan

1. Add the `agent` filter to `GET /api/sessions` in `admin.py`; restart `alfred-admin.service` (no DB migration — additive).
2. Extend the Go UI: models (`AgentDetail`, `McpServerSummary`, `Memory`), client methods, dashboard page, memories subpage, and the agents-table link. Run `templ generate` and `go build ./...`.
3. Deploy the UI binary; verify against the live control plane and admin server.
4. Rollback: revert `admin.py` (additive) and stop the previous UI binary independently.

## Open Questions

None — the memory browser shape is fixed by the existing `/api/memories` contract and the iOS client it already serves; no decision here would change the specs or task breakdown.
