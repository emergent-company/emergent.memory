## Why

The Memory backend can now expose a single agent as its own MCP server with one tool (`call_agent`) and a per-agent key, but the web app has no way to generate or manage that key. Users must call the API by hand to hand an agent to an external LLM. This change adds the UI and gateway proxy so an admin can generate, copy, list, rotate, and revoke an agent's MCP credential.

## What Changes

- Add a **"Share as MCP tool"** action on the agent view that creates a per-agent MCP share and opens a one-time key reveal.
- Add an **Agent MCP Sharing** area (nested under the agent's sharing context) listing that agent's shares with status and timestamps, and offering rotate and revoke.
- The reveal shows the per-agent MCP endpoint URL (`…/api/mcp/agents/<agentId>`), the raw API key once, and ready-to-paste client config snippets (Claude Desktop, Claude Code, Cursor, Cloud Code) each with a copy control.
- Add gateway routes under `/api` that proxy the backend per-agent share endpoints (create, list, revoke, rotate), preserving the existing session/API-key auth boundary.
- Legacy/foreign shares are never shown; only the active project's agent shares appear.

## Capabilities

### New Capabilities

- `agent-mcp-share-ui`: gateway API + web UI to create, list, rotate, and revoke per-agent MCP share credentials and reveal the per-agent endpoint and key.

### Modified Capabilities

<!-- None: the agent dashboard's existing requirements are unchanged; this adds a new action and area. -->

## Impact

- **`gateway/`**: new `agent_mcp_shares.go` (models + memory-backed client) and `agent_mcp_shares_handlers.go` (JSON + UI handlers), a templ view, route registration in `main.go`, and `MemoryBackend` additions in `backend.go`.
- **`gateway/agent.templ`**: the "Share as MCP tool" action.
- **Auth**: reuse `requireSession` (UI) and `requireSessionOrKey` (`/api`); no new primitive.
- **Depends on** the Memory backend change `add-agent-mcp-endpoint` in `emergent.memory` (per-agent endpoint + share API). Browser end-to-end is deferred until that backend is deployed.
- **Out of scope**: async/polling agents, conversation continuity, and backend enforcement (owned by the backend change).
