## Why

Users run MCP servers on their own machines (Mac/Linux) with tools the cloud
Memory backend cannot reach — Apple Notes/Reminders on macOS, local files,
home automation, etc. The Emergent Memory backend already ships an MCP relay
hub (`/api/mcp-relay`, outbound WebSocket star topology, Diane-derived) that
lets those machines register and serve tools, and relay tools already flow
into agent tool pools as `<instance>_<tool>`. But the Memory web UI is blind
to it: there is no page listing connected external nodes or their tools, and
the agent tool picker reads only the admin MCP-server registry, so relay tools
are invisible and cannot be attached to agents from the UI. Result: the
capability exists end-to-end in the backend yet is unmanageable and unusable
from Memory.

This is the Memory-side half of the "Diane-style external MCP" feature.
Phase 1 (this change): surface and manage external MCP relay nodes + tools in
the Memory UI so users can see connected machines and attach their tools to
agents. Phase 2 (separate change, designed in design.md): the user-facing Mac
connector app (Go core + Swift menu-bar shell) that runs those local MCP
servers and opens the relay connection.

## What Changes

- Gateway proxies the backend relay inspection API so the web UI can list
  connected external MCP nodes and each node's tools.
- New Memory UI page listing connected external MCP nodes: instance id,
  version, connected-at, tool count, per-node tool list (grouped, showing the
  `<instance>_<tool>` agent-facing names), and connection health derived from
  session presence.
- Agent tool picker additionally shows tools from connected external MCP
  nodes (labelled by node), so relay tools can be checked onto an agent's tool
  whitelist / banned list like any other tool.
- Empty/error states: no connected nodes, backend relay API unreachable.
- Backend (emergent.memory) unchanged — relay REST API already exists and is
  reachable with the gateway's existing bearer token. No new backend scope
  required.
- Related sibling change `add-mcp-servers-ui` (capability `mcp-servers-ui`)
  manages *registry* MCP servers (stdio/sse/http the backend spawns or dials).
  This change manages *relay nodes* (remote machines that push tools over the
  backend MCP relay). Complementary, not overlapping: distinct routes, data
  sources, and capability specs. Both add entries to the Settings rail and
  extend `backend.go` — coordinate apply order to avoid edit conflicts.
- Out of scope (later change): Mac/Linux connector app, node lifecycle
  actions from UI (start/stop/disconnect), offline-node registry
  (sessions are in-memory only today).

## Capabilities

### New Capabilities

- `external-mcp-nodes`: Memory web UI management surface for external MCP
  relay nodes — list connected nodes, inspect their tools, show health, and
  represent nodes/tools so agents can be configured to use them.

### Modified Capabilities

- `agent-dashboard-ui`: the agent tool picker SHALL also surface tools from
  connected external MCP relay nodes (grouped by node, using their
  agent-facing `<instance>_<tool>` names) so a user can attach them to an
  agent's allowed/banned tools from the UI.

## Impact

- `gateway/` (Go, this repo): new backend client methods + handlers proxying
  the relay REST API; nav entry + templ page for external MCP nodes; tool
  picker data merge in the agent dashboard loader.
- Agent/tool persistence unchanged (flat tool-name whitelist already accepts
  relay-prefixed names).
- No backend / emergent.memory changes; no DB migration.
- Tests: unit tests for gateway proxy handlers + page render + picker merge
  (deterministic, faked backend). E2E optional.
