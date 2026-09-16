## Why

Memory can now show external MCP relay nodes in the UI (`add-external-mcp-management`),
but nothing connects yet: no client ships that registers a user's local machine
as a node and serves its tools over the relay. The Diane project (MIT) already
solves the client half — `diane mcp relay` — and its Apple tool set, but the
Memory product needs its own connector: configured against a Memory project,
registering tools the user chooses, running headless on macOS (Apple Notes /
Reminders) today and Linux (generic local MCP servers) later.

Phase 2a (this change): the **Go core CLI**. Phase 2b (separate change): a Swift
menu-bar shell (onboarding, permission prompts, supervision) wrapping this
binary — the Go core stays usable headless now and is the shared engine the
Linux port reuses unchanged.

## What Changes

- New self-contained Go module `connector/` in this repo building
  `memory-connector`, a CLI daemon that:
  - **Connects** to a Memory project's MCP relay hub over an outbound WebSocket
    (`/api/mcp-relay/connect`) using a project-scoped `emt_*` bearer token — no
    inbound ports, no NAT config.
  - **Registers** an instance (stable `instance_id`, version, tool list) and
    keeps the registration fresh (re-register on tool changes/reconnects).
  - **Serves** relayed `tools/call` requests for its registered tools by
    dispatching to local tool handlers, returning MCP results/errors.
  - **Stays alive**: keepalive pings, exponential reconnect with backoff and
    cap, clean SIGTERM/SIGINT shutdown.
- **Config + onboarding**: `memory-connector init` writes
  `~/.config/memory-connector.yml` (server URL, token, project id, instance id)
  with a `doctor`/connectivity probe; `memory-connector status` shows
  connection state and registered tools.
- **v1 tool set — macOS Apple tools, zero third-party deps**: Notes and
  Reminders tool handlers via `/usr/bin/osascript` (AppleScript — both apps are
  scriptable), each tool with an MCP `inputSchema` + description. No brew
  dependencies (Diane used `remindctl` for Reminders; AppleScript avoids it).
- Works with the just-shipped UI: a connected node appears on
  `/settings/mcp-nodes`, its tools become attachable to agents, and calls are
  forwarded by the existing hub — no backend or gateway changes.
- Linux: compiles and runs; Apple tools degrade to a clear "unavailable on this
  platform" set (generic tool hooks land in a later change).

## Capabilities

### New Capabilities

- `mcp-connector`: A headless local CLI connector that registers a machine as
  an external MCP node on a Memory project over the MCP relay — configurable
  per project, resilient (reconnect/keepalive), serving a v1 set of local
  Apple (Notes/Reminders) MCP tools implemented via AppleScript.

### Modified Capabilities

<!-- None: hub-side behavior (mcprelay in emergent.memory) and Memory UI specs
     (external-mcp-nodes, agent-dashboard-ui) are unchanged. This change adds
     the client that consumes those surfaces. -->

## Impact

- **New `connector/` Go module** (own `go.mod`, module path
  `github.com/emergent-company/memory.web-ui/connector` or a connector-scoped
  path decided at implementation): cmd/ tree, config, relay client, tool
  registry + Apple handlers, tests.
- Dep: `gorilla/websocket` (matches the hub) or `coder/websocket` — decided at
  implementation; Go proxy reachable from this repo's build env.
- Derived from Diane (`emergent-company/diane`, MIT): relay-client behavior
  (frames, backoff, keepalive) and Apple tool concepts are ported, not copied
  verbatim; MIT attribution recorded in the module's NOTICE.
- Unit + integration tests with an in-memory WS relay hub (httptest) —
  deterministic, no real Memory needed. Playwright e2e deferred to the shell
  phase.
- No changes to gateway/, emergent.memory backend, or existing specs.
