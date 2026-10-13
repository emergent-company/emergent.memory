## Why

The MCP client docs the web UI hands out point at an unreachable internal host.

The server builds its MCP URLs from the inbound request host (`requestBaseURL`,
`apps/server/domain/mcp/share_instance_handler.go`, and the duplicated logic in
`bundle.go`). When the web-UI gateway proxies a share / agent-endpoint call to
the memory server, it talks to the internal `MEMORY_URL` (e.g.
`http://memory-server:3002`) and forwards no public host, so the server echoes
back `http://memory-server:3002/api/mcp/...` — a host the user's machine cannot
reach. The gateway then renders that URL verbatim into the reveal, the endpoint
URL box, the generated snippets, and the JSON echo responses.

Separately, Claude Desktop's one-click install path is the `.mcpb` MCP Bundle
(download → double-click → Install). The server already generates these
(`GET /api/mcp/bundle?token=`, `GET /api/projects/:id/mcp/bundle`) and the MCP
invite email links them, but the web UI never surfaces the download — it only
prints `npx mcp-remote` JSON. There is no per-agent bundle at all: the existing
public bundle is project-scoped, so an agent-endpoint key would install the
wrong server (and agent keys carry the `mcp:agent-call` marker the project
transports reject).

## What Changes

- **Gateway renders the external origin.** New `externalMCPURL` helper rewrites
  the backend-provided `mcpUrl` scheme+host to the memory server's public origin
  (`serverPublicBaseURL()`: `MEMORY_PUBLIC_URL`, defaulting to `MEMORY_URL` — the
  origin MCP routes are actually served from) before snippets are built and
  before the URL is rendered or echoed. The origin is never derived from the
  request Host. Applied to the agent MCP endpoint surface and the project MCP
  share surface.
- **`.mcpb` download in the web UI.** The share reveal and the agent-endpoint
  reveal gain a "Download Claude Desktop bundle" link pointing at the existing
  public bundle route, using the same external origin.
- **Per-agent `.mcpb` bundle (new server route).**
  `GET /api/mcp/agents/:agentId/bundle?token=<emt_*>` — public (token is the
  credential), registered before the auth-gated `/api/mcp/agents` group. It
  validates the token is an active agent key bound to that agent and builds a
  bundle whose MCP server URL is `/api/mcp/agents/:agentId`.

## Capabilities

### Modified Capabilities

- `mcp-share-link`: the share response's `mcpUrl` and the admin reveal use the
  external origin; the reveal offers a `.mcpb` download.
- `mcp-share-management`: the share-instance reveal uses the external origin and
  offers a `.mcpb` download.
- `agent-mcp-endpoint`: adds the agent `.mcpb` bundle download route and the
  external-origin rule for the agent endpoint docs.

## Impact

- `apps/web-ui/gateway/`: `externalMCPURL`; applied in
  `agent_mcp_endpoint_handlers.go` / `mcp_shares_handlers.go`; bundle links in
  `agent_mcp_endpoint.templ` / `mcp_shares.templ`; tests.
- `apps/server/domain/mcp/`: new public agent-bundle route + handler
  (`routes.go`, `bundle.go`), token + agent-key validation, Swagger; tests.
- `apps/server/docs/swagger/*`: regenerated.
- No schema change, no auth-boundary change on existing routes.

## Back-compat

Direct API/CLI callers are unaffected: they already reach the server on its
public host, and the rewrite only applies at the gateway. `requestBaseURL` keeps
its current behaviour server-side. The new route is additive and public like the
existing `/api/mcp/bundle`.
