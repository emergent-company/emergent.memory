# Add MCP OAuth connect

## Why

Installing an OAuth-protected remote MCP server (e.g. Figma's
`https://mcp.figma.com/mcp`) silently produces a server with zero tools. The
install path (`InstallFromRegistry` → `DiscoverAndSyncTools`) logs the failure
and returns success, and the registry domain has **no** OAuth support at all:
remote transports carry static headers plus encrypted secret headers only. The
user is left with no explanation and no way to authorize.

A live probe of the target shows the failure is an authentication rejection that
advertises a complete OAuth surface:

```
POST https://mcp.figma.com/mcp -> 401
www-authenticate: Bearer resource_metadata="https://mcp.figma.com/.well-known/oauth-protected-resource",
                  scope="mcp:connect", authorization_uri="https://api.figma.com/.well-known/oauth-authorization-server"
```

PRM → `authorization_servers: ["https://api.figma.com"]`, scope `mcp:connect`.
AS metadata → authorization endpoint `https://www.figma.com/oauth/mcp`, token
endpoint `https://api.figma.com/v1/oauth/token`, a **dynamic client
registration** endpoint, `code_challenge_methods_supported: ["S256"]`, and the
`authorization_code` + `refresh_token` grants.

## What Changes

- **Discover the authorization requirement.** A 401/403 from a remote MCP server
  is classified: an inspect returns `status: "auth_required"` carrying the
  protected-resource metadata (authorization server, scopes, whether dynamic
  client registration is offered) instead of an opaque connection error.
- **Connect the server with OAuth.** A project member can start a browser
  authorization flow for a server; the server performs dynamic client
  registration (when offered), PKCE, the code exchange, and persists the token
  set encrypted at rest. The flow reuses the existing unauthenticated
  state-anchored callback pattern from the data-source connect flow.
- **Use the credential at call time.** The proxy attaches the OAuth bearer to
  outbound MCP requests, refreshing on expiry and on a 401, and device-targeted
  servers receive a resolved (refreshed) bearer in their pushed config.
- **Surface it in the UI.** The registry install flash and the MCP servers page
  offer "Connect" (and "Disconnect") for OAuth-protected servers, and the
  inspect panel explains an authorization requirement rather than reporting a
  bare failure.
- **Keep the manual path.** A server that also accepts a static API token header
  (Figma accepts `X-Figma-Token`) remains usable through the existing encrypted
  secret-header configuration.

## Impact

- Affected specs: `mcp-oauth` (new), `mcp-registry` (modified).
- Affected code: `apps/server/domain/mcpregistry` (discovery, OAuth client,
  store, service, handlers, proxy, device config), one migration,
  `apps/server/internal/config`, `apps/web-ui/gateway` (MCP servers page,
  registry page).
- No breaking change: servers without an OAuth credential keep the existing
  header behaviour.
