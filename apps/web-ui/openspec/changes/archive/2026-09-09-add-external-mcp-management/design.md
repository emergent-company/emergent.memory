# Design — add-external-mcp-management

## Context

See proposal.md. The Emergent Memory backend already exposes the MCP relay
hub (outbound-WS star topology) with a read API the gateway can reach using its
existing `emt_*` bearer token: `GET /api/mcp-relay/sessions` returns
`{sessions:[{instance_id, version, tool_count, connected_at}]}` and
`GET /api/mcp-relay/sessions/:instanceId/tools` returns the raw MCP `tools/list`
payload the node registered. Sessions are in-memory on the backend, keyed by
project. This change is read/manage-surface only: no backend changes, no DB
migration, no direct tool invocation from the UI.

Gateway facts used below: `MemoryBackend` interface (`backend.go`) is faked in
`handlers_test.go`; `MemoryClient` (`memory.go` + `extras.go`) does Bearer +
project-header REST against `MEMORY_URL`; REST admin-style responses come back
wrapped in `successEnvelope`, but relay endpoints DO NOT use the envelope —
they return plain JSON. UI is go-templ + daisyUI under `/root/alfred/gateway`;
agent Settings page + tool picker live in `agent.templ`/`agent.go`, loaders in
`agent.go` (`agentSettingsData`).

## Goals / Non-Goals

Goals:
- Gateway proxies relay sessions + per-node tools; errors map to the existing
  friendly 502 convention.
- Settings page "External MCP nodes" (`/settings/mcp-nodes`) lists connected
  nodes and per-node tools with agent-facing `<instance>_<tool>` names.
- Agent Settings → Tools panel also shows connected relay nodes' tools as
  checkable groups; relay-sourced tools are visually distinct.
- Unit-tested with a faked backend; deterministic.

Non-goals:
- No backend/emergent.memory edits, no direct tool calls from the UI, no
  start/stop/disconnect actions, no offline-node registry/persistence, no
  live/websocket push to the page (refresh-based presence).
- The Mac/Linux connector (Phase 2, separate change) is designed below only
  far enough to keep the API contract stable.

## Decisions

### 1. New backend client methods decode plain JSON (no successEnvelope)

`ListRelaySessions` / `GetRelaySessionTools` on `MemoryBackend` +
`MemoryClient`, calling the two GETs above with the standard session token +
project header. Response decoding is a dedicated path because relay endpoints
omit the `successEnvelope` wrapper every other gateway client method assumes.

Rationale: minimal surface, matches existing proxy conventions. Alternative —
add gateway-side registry of nodes — rejected: backend is already the source
of truth for relay sessions.

### 2. Node tools normalized to names at the client boundary

`GetRelaySessionTools` returns `[]RelayTool{Name string}` extracted tolerantly
from the raw MCP payload (nested `{"tools":[...]}` or flat array; tool entries
may carry `name` at top level or in `tool.name`). The agent-facing name is
derived once: `<InstanceID>_<ToolName>` (matches how the backend names relay
tools in the tool pool).

Rationale: the picker and page both only need names + descriptions; keeping raw
`map[string]any` downstream leaks MCP shape quirks into templates. The tolerant
extraction mirrors the backend's own `toolCount` handling.

### 3. Page: Settings rail, refresh-based presence

New route `GET /settings/mcp-nodes` → `uiMCPNodes` handler → `ExternalMCPNodesPage`
templ. Node rows: instance id, version, tool count, connected-at (relative).
Selecting a node (query param `?node=<instance_id>`) re-renders the page with
that node's tools shown (agent-facing names + description when available).
Empty state when `sessions` is empty; error state (friendly, rest-of-page
usable) when the relay API fails. A refresh control reloads the list.

Naming coordination with sibling change `add-mcp-servers-ui`: its Settings
rail entry is "MCP Servers" (registry CRUD). To avoid confusion this page's
rail entry is labelled **"MCP nodes"** (page header stays "External MCP
nodes"); where both entries exist in one rail they read distinctly.

Rationale: presence is backend-in-memory; refresh is honest and cheap. Tools are
small (typically a handful per node), so fetch-on-select is fine. Alternative —
full fetch-everything-on-load — rejected: node count can grow; fetch-on-select
keeps the common path light.

### 4. Agent tool picker merges relay nodes as extra labelled groups

The agent Settings loader already fetches `ListMCPServers` for the picker.
Extend it: also fetch relay sessions + selected node tools (bounded: sessions
are few) and expose them as `RelayNodes` on `agentSettingsData`. The picker
renders one `<details>` group per node, labelled with the instance id + a
"remote" badge (visually distinct from registry server groups), each tool a
checkbox `name="tool" value="<instance>_<tool>"` checked per the agent's
existing whitelist. Unlisted/other tools keep the existing "Other" group path
unchanged, so a node disconnecting never drops whitelisted tools on save.

Relay API failure during agent-settings load degrades: relay groups simply
absent, other panels intact (matches "Surface load failures" spirit of
agent-dashboard-ui spec); existing tools remain visible via the Other group.

Rationale: reuse of the flat tool-name whitelist + `mcpServerInUse` semantics;
no persistence change. Per-tool policy select reused if backend policy applies
to relay names too — verify in implementation, else omit policy select for
relay groups.

### 5. Backend relay API reachable with current token — no auth change

Relay routes require only `RequireAuth` + project context (verified in
emergent.memory `mcprelay/routes.go`), unlike admin MCP registry routes which
need an `admin` token scope. The gateway's existing session/`emt_*` project
token suffices. If a deployment token lacks project context, the page shows
the error state — no retry logic in v1.

## Risks / Trade-offs

- [Node disappears from UI while user is configuring] → Page is refresh-based;
  agent Settings keeps working because whitelist names persist and unlisted
  tools fall into the Other group.
- [Backend raw tools payload shape varies between connector implementations]
  → Tolerant extraction + normalization at the client boundary; unit-tested
  against nested and flat shapes.
- [Relay API error on agent settings page hides relay groups silently] →
  Degrade-only by design; document that the dedicated nodes page surfaces the
  real error state.
- [Multiple nodes export same tool name → prefixed names clash-free only if
  instance ids are unique] → instance ids are connector-generated and stable;
  note in UI copy that instance id is the unique key.

## Migration Plan

Additive feature, no schema or behavior migration. Rollback = revert gateway
change; backend untouched. Deploy with the normal gateway build.

## Phase 2 (separate change) — connector, design intent only

The future user-facing connector is a separate change/project; this section
locks the contract it must satisfy so Phase 1 UI stays valid.

- Architecture (user decision): Go core + Swift menu-bar shell on macOS.
  Go binary owns the relay client + MCP server + tool definitions; Swift shell
  owns onboarding, permission prompts, and supervision. Linux later reuses the
  Go core unchanged.
- Reference implementation, MIT-licensed, in `emergent-company/diane`:
  relay client `server/cmd/diane/mcp_relay.go` (~600 LOC, WS dial, register
  frame with instance_id/hostname/version/tools, request/response correlation,
  reconnect backoff, ping/pong, tool re-registration); Apple MCP tools
  `server/mcp/tools/apple/` (Reminders via `remindctl`, Notes via AppleScript,
  Contacts via embedded Swift); SwiftUI menu-bar companion pattern
  (`DianeCompanion`, MIT snapshot public in git history). Wire protocol docs
  live in the emergent.memory repo (`docs/site/developer-guide/mcp-relay.md`).
- License note: `emergent.memory` (the relay hub) is unlicensed — use as a
  service, copy nothing from it. Diane repos are MIT and safe to port.

## Open Questions

- Whether per-tool policy (`ask`/`deny`) should apply to relay-sourced tool
  names in the picker, or relay groups render allow-only in v1. Settle during
  implementation; does not change specs (tools remain checkable either way).
