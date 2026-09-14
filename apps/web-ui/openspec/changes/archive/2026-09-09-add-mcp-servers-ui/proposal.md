## Why

Memory's MCP registry is the sole source of agent capabilities, but the gateway only exposes it as a bare JSON API (`/api/mcp-servers` CRUD, `main.go:92-96`) and a read-only per-server tool list on the agent page. Users cannot register, connect, or maintain MCP servers without curl + memory tokens. Managing servers directly from the UI closes the loop: register an external MCP server, discover/allowlist its tools, and attach them to agents — all without leaving the browser.

## What Changes

- New project-scoped **MCP Servers management page** (sidebar Settings group, following the API Tokens precedent): table of registered servers (name, type, status, tool count), create page, edit page, and delete with confirm dialog.
- **Register/connect external MCP servers** from the UI: name + transport (`stdio`/`sse`/`http`), transport-specific fields (url, headers as key/value rows, command/args/env for stdio), enable flag.
- **Tool management per server**: view cached tools (`tools/list` results), run **sync** (`POST /:id/sync`) and **inspect** (`POST /:id/inspect`) with results surfaced in the UI, per-tool **enable toggle**, prune notice on sync.
- Agent page empty-state CTA ("Register an MCP server…") becomes a link to the new page.
- No backend changes in memory — the gateway gains the missing client methods/handlers/routes (`sync`, `inspect`, `GET /:id/tools`, `PATCH /:id/tools/:toolId`) that proxy memory's existing admin API.

## Capabilities

### New Capabilities
- `mcp-servers-ui`: In-browser management of project MCP servers — list/register/edit/delete servers across stdio/sse/http transports, run sync/inspect, toggle tools, and navigate from the agent tool picker empty state.

### Modified Capabilities
<!-- None: agent tool whitelist editing behavior is unchanged; the agent page only gains a link in an existing empty-state description. -->

## Impact

- **Gateway**: new `mcp_servers*.go` files (data/client/handlers/templ) mirroring the api_tokens trio; extended `backend.go` `MemoryBackend` interface + `extras.go` client for sync/inspect/tools/toggle; new routes in `main.go` (~UI + JSON); sidebar entry in `ui.go` `sidebarGroups`; empty-state link in `agent.templ`; unit + handler + UI-render tests.
- **E2E (tests/e2e)**: one new mutation spec driving register → sync → attach flow against the live-dev suite (`*e2e*` target: dev memory `api.dev.emergent-company.ai`); env key for the example server URL added to `.env.e2e.example`.
- **No changes** to `/root/emergent.memory` (admin API already exists) or agent/chat behavior.

## Related change

Not to be confused with **`add-external-mcp-management`** (capability `external-mcp-nodes`): that change manages *relay nodes* — remote user machines that push MCP tools to the backend over the outbound-WebSocket MCP relay (`/api/mcp-relay`), e.g. Diane-style Mac/Linux connectors. This change manages *registry MCP servers* — stdio/sse/http servers the backend itself spawns or dials. Complementary mechanisms for reaching external tools; both land in the same Settings rail and touch shared gateway files (`backend.go` MemoryBackend interface, sidebar groups, tool picker adjacency) — coordinate apply order to avoid edit conflicts.
