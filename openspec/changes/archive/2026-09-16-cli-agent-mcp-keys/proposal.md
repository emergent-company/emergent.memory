## Why

The agent-scoped MCP endpoint shipped on the server with a full admin API — one
endpoint per agent, many labeled keys (create/list/revoke/rotate, one-time
secret reveal), and a session list — but the `memory` CLI cannot reach any of
it. Today an operator who wants to hand an agent to an external MCP client has
to drive the API by hand or use the web UI. The CLI is where agent lifecycle
work already happens (`agents create`, `agents trigger`, `agents hooks`,
`agents mcp-servers`), so the endpoint belongs there too.

## What Changes

- Add `memory agents mcp-endpoint` — a new subgroup under the existing `agents`
  command — covering the endpoint lifecycle: `show`, `create`, `revoke`.
- Add `memory agents mcp-endpoint keys` — `create`, `list`, `revoke`, `rotate`.
  Create and rotate print the raw secret **exactly once** plus the MCP URL to
  paste into a client; list never shows a secret.
- Add `memory agents mcp-endpoint sessions` — list an endpoint's sessions with
  an optional `--status` filter, showing the owning key label, counters, and
  timestamps.
- Resolve the agent argument by id, name, or slug (runtime agent names and
  agent-definition names/slugs), so operators never have to paste a UUID.
- Resolve the project from the existing project-context mechanism
  (`--project`, config, `MEMORY_PROJECT`, picker).
- Support `--json` on every read command (`show`, `keys list`, `sessions`).
- Require `--yes` for destructive operations (`revoke`, `keys revoke`,
  `keys rotate`) in non-interactive contexts, prompting on a terminal.
- Map server errors readably: 409 duplicate label / endpoint already exists,
  404 not found, 403 not an admin.
- Add a thin SDK surface for the seven routes in
  `apps/server/pkg/sdk/mcp` (endpoint, keys, sessions) — the SDK has no agent
  MCP endpoint client today.

## Capabilities

### New Capabilities

- `cli-agent-mcp-keys`: the `memory agents mcp-endpoint` command tree —
  endpoint show/create/revoke, labeled key create/list/revoke/rotate with
  exactly-once secret reveal, session listing, agent reference resolution, JSON
  output, destructive confirmation, and readable error mapping.

### Modified Capabilities

- None. The server API contract, the five-tool MCP surface, and session
  semantics are unchanged; this change only consumes them.

## Impact

- **CLI:** new `apps/cli/internal/cmd/agent_mcp_endpoint.go`; commands
  registered on the existing `agentsCmd` group. `--project` comes from the
  group's existing persistent flag.
- **SDK:** new `apps/server/pkg/sdk/mcp/agent_endpoint.go` adding
  endpoint/key/session methods and DTOs to the existing `mcp` client
  (`Client.Share` is the closest existing analog — one-time credential minting).
- **Server:** no changes — no domain, migration, route, or contract edits.
- **Web UI:** no changes.
