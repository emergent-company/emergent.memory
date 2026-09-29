## Why

The MCP Servers page (`/settings/mcp-servers`) lists memory's builtin server as a row named
`builtin` whose type badge also reads `builtin`, so the row renders `builtin · builtin` and is
indistinguishable from the transport label. Its cached tools render as one flat list of ~124 rows,
which is unreadable, even though the agent tool picker already renders the exact same tools in
frozen capability groups (`Graph · Read`, `Documents`, `Skills`, …).

Separately, there is no way to exercise a tool from the UI. Tool invocation exists over HTTP for
relay nodes (`POST /api/mcp-relay/sessions/:instanceId/call`) and for builtin tools
(`POST /api/mcp/rpc` `tools/call`), but registered external servers (stdio/sse/http) can only be
called through an agent run — no deterministic "call this tool with these arguments" path. That
makes debugging a newly registered server a slow, LLM-mediated round trip.

## What Changes

- **Builtin row identity**: display the builtin server as **"Memory tools"** and stop rendering a
  duplicated `builtin` transport badge.
- **Grouped builtin tools**: render the builtin server's cached tools in the same collapsible
  capability groups as the agent tool picker, from a new server-owned endpoint
  `GET /api/admin/builtin-tool-groups` (catalog-only membership, frozen group order, empty groups
  omitted). Tools covered by no group remain reachable in an "Other" group. The gateway renders the
  taxonomy; it never derives it.
- **Per-tool invocation**: a "Run" affordance on every tool row opens an arguments dialog and calls
  `POST /api/admin/mcp-servers/:id/tools/:toolName/call` (builtin tools execute in-process; external
  tools are proxied by server row, no name-prefix inference). The gateway proxies the same shape at
  `POST /api/mcp-servers/:id/tools/:toolName/call` and renders the result without `innerHTML`.
- **Authority parity**: builtin dispatch enforces the caller's per-tool authority exactly as the MCP
  HTTP transport does (rejects `AgentOnly`, checks `RequiredScope` and `SuperadminOnly`); the route
  never blanket-trusts. Invocation enforces `server.Enabled` and `tool.Enabled`, sanitizes upstream
  errors, and bounds the call with a timeout.

## Capabilities

### New Capabilities

- `mcp-tool-invocation`: the per-tool call endpoint, its enablement gates, authority rules, and
  error/response contract.
- `mcp-builtin-tool-groups`: the server-owned builtin tool capability taxonomy exposed for the
  MCP Servers page (catalog-only grouping).

### Modified Capabilities

- `mcp-servers-ui`: builtin server identity ("Memory tools"), grouped builtin tool rendering, and
  the per-tool invoke affordance.

## Impact

- **Server** (`apps/server/domain/`):
  - `mcpregistry/`: new `POST /api/admin/mcp-servers/:id/tools/:toolName/call` handler +
    `Service.CallToolOnServer` + `ProxyManager.CallToolOnServer`; typed errors (404/409/403/502).
  - `agents/`: new `GET /api/admin/builtin-tool-groups` → `ToolGroupsFromCatalog` (shared
    `catalogGroupMembership` core with `ToolGroupsWithCatalog`).
  - `mcp/`: `Service.AuthorizeToolCall` — the shared per-tool authority check used by the invoke
    path, mirroring the transport handlers.
- **Gateway** (`apps/web-ui/gateway/`): `mcp_servers.templ` grouped/invoke UI,
  `mcp_servers.go`/`_handlers.go`/`_client.go`, `backend.go`, `main.go` proxy route.
- **No migrations.** No new agent-facing tool; the invoke route is project-member surface, not an
  MCP tool itself.
