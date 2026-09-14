## Why

The Memory backend can expose a project over MCP (read-only share endpoint plus scope-filtered tool catalog), but the web app offers no way to create or manage those shares. Users cannot see existing shares, choose which memory tools an outside agent may call, share a specific agent, or copy connection details — they must call the API by hand. This blocks the intended "hand an outside agent a scoped MCP instance" workflow.

## What Changes

- Add an **MCP Sharing** management area in the gateway/web UI, backed by the Memory backend's share-instance API, where a project admin can create, view, edit, and revoke named MCP share instances.
- The create/edit form lets the admin **select which memory tools** an instance may expose (grouped by category, with a search/filter), and **select which agents** it may access; the instance's token is scoped automatically from the selected tools.
- After creation (and on demand via rotate), the UI reveals the instance **API key once** with copy controls and ready-to-paste client config snippets (Claude Desktop, Claude Code, Cursor, Cloud Code).
- List existing shares with name, tool/agent counts, created date, last used, and status; support rename/update and revoke with confirmation.
- Add gateway API routes that proxy the backend share-instance + tool-catalog endpoints, keeping the existing auth boundary (session in the UI, `X-API-Key`/session on `/api/*`).
- Add an entry point from the existing MCP Servers page and a **"Share via MCP"** action on an agent that pre-selects that agent in a new share.
- Legacy shares created through the backend's `POST /:projectId/mcp/share` are listed read-only as legacy instances.

## Capabilities

### New Capabilities

- `mcp-share-management`: gateway API + web UI to create, list, edit, rotate, and revoke project MCP share instances, including the memory-tool and agent pickers and the one-time key/snippet reveal.

### Modified Capabilities

- `mcp-servers-ui`: the MCP Servers page gains a navigation entry point to the new MCP Sharing area, and relay/registry tool presentation stays unchanged.

## Impact

- **`gateway/`**: new `mcp_shares.go` (models + memory-backed client), `mcp_shares_handlers.go` (JSON/echo handlers + UI actions), `mcp_shares.templ` (list/create/edit views), route registration in `main.go`, and backend interface additions in `backend.go`.
- **`gateway/mcp_servers.templ`**: entry point link to the MCP Sharing area.
- **`gateway/agent.templ`**: "Share via MCP" action on an agent.
- **Auth**: reuse existing `requireSession` (UI) and `requireSessionOrKey` (`/api/*`) gates; no new auth primitive.
- **Depends on** the Memory backend change `add-mcp-share-instances` in `emergent.memory` (share CRUD + tool catalog + per-instance allowlist). Until it lands, the UI is inert; ship both together.
- **Out of scope**: writable/scoped-by-scope configuration beyond per-tool selection, OAuth for MCP clients, and backend enforcement logic (owned by the backend change).
