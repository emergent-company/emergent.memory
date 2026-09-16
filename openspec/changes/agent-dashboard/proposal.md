## Why

The Go web UI (`ui/`) lists agents as a flat table and shows a global, agent-agnostic session log, but there is no single screen where a user can see everything about one agent. Users want an agent dashboard — one place that shows an agent's summary, its configured tools, its recent chats, and the same memory browser the iOS app already offers (a searchable list of what the agent remembers, with a detail view).

## What Changes

- New per-agent **dashboard** screen in the Go web UI: an agent details page reachable from the agents list, showing the agent's summary (backend type, model, enabled state, version), its **configured tools** (MCP servers with their tool allowlists, plus built-in functions), and its **recent chats** (the agent's most recent sessions, linking to the full session timeline).
- A **memories subpage** linked from the dashboard that mirrors the iOS memory browser: a searchable list of the agent's memories (content, category, confidence) with a detail view for full content, reusing the existing `/api/memories` and `/api/memories/capability` endpoints.
- Session listing gains an agent filter so the dashboard can show an agent's recent chats instead of every room.

## Capabilities

### New Capabilities

- `agent-dashboard-ui`: Go web UI agent details screen (the agent dashboard) showing the agent summary, configured tools, recent chats, and a link to a memories subpage that mirrors the iOS memory browser (searchable list + detail).

### Modified Capabilities

None.

## Impact

- Go UI (`ui/`): new agent-detail route and dashboard page, a memories subpage, and client/model additions to fetch agent detail, MCP servers, agent-scoped sessions, and memories.
- `agent/admin.py`: add an agent filter to the sessions endpoint. The memory endpoints (`/api/memories`, `/api/memories/capability`) are consumed read-only as-is — no changes.
- Control-plane API (`agent/api/main.py`): consumed read-only (`GET /api/agents/{id}`, `GET /api/mcp-servers`) — no changes.
- Emergent Memory service: consumed read-only via the existing proxy — no changes.
- No breaking changes.
