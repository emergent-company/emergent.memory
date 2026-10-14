# Design — add-mcp-oauth-connect

## Context

- `apps/server/domain/mcpregistry` owns remote MCP servers. Remote transports
  (SSE / streamable HTTP) authenticate with plaintext `headers` plus
  `secret_headers` (encrypted via `crypto.Encryptor`).
- Outbound connections already funnel through a guarded client
  (`outbound.go`): SSRF pre-flight plus a guarded dialer, and an
  `authStatusTransport` that converts 401/403 into `ErrMCPAuthRejected`.
- A provider-generic OAuth connect flow already exists for data sources
  (`domain/sources`): `authorize` returns a URL, a **public** callback route
  consumes a single-use pending state (`kb.oauth_pending_states`) holding the
  PKCE verifier, then 303-redirects the browser back to the UI. Deployment
  config supplies the public API origin (`SourceOAuthConfig.RedirectBaseURL`).
- `github.com/mark3labs/mcp-go v0.44.0` is already a dependency and ships
  `client/transport/oauth.go`, but its handler **omits the RFC 8707 `resource`
  parameter** on both the authorization URL and the token/refresh requests, and
  its registration result lives only in process memory. It is therefore used for
  reference/helpers at most; the flow is implemented in `mcpregistry`.

## Decisions

### D1 — Discovery is a first-class result, not an error string

`ProxyManager.InspectServer` classifies a failed connection as
`status: "auth_required"` when the server answered 401/403 and advertised a
`WWW-Authenticate: Bearer` challenge with `resource_metadata` (or an
`authorization_uri`). The DTO gains an `auth` object:
`{resourceMetadataUrl, authorizationServer, scopes, registrationSupported}`.

_Why:_ the user's next action is unambiguous once the requirement is known —
authorize — and the same discovery feeds the Connect button. Discovery failures
degrade to today's `status: "error"` shape; a 401 without OAuth metadata stays a
credential error so no false "Connect" affordance appears for a plain
misconfigured header.

_Rejected:_ treating every 401 as OAuth. A server that rejects a wrong static
header would gain a misleading Connect button.

### D2 — Own OAuth client in `mcpregistry`

A small client implements: WWW-Authenticate/PRM/AS-metadata discovery, dynamic
client registration, PKCE (S256), authorize-URL construction, code→token
exchange, and refresh. Every request carries the RFC 8707 `resource` parameter
(the protected resource URL) on authorize, exchange, and refresh.

_Why:_ mcp-go's handler cannot send `resource` (see Context) and cannot persist
a registration result; wrapping it would recreate the flow anyway. Using the
library's transport OAuth option would also hide the refresh inside the
transport, where the proxy cannot report a re-authorization requirement.

### D3 — Token set stored per server row, project-scoped

New table `kb.mcp_server_oauth`, keyed by `server_id` **unique**, holding
`authorization_server`, `token_endpoint`, `registration_endpoint`, `resource`,
`scope`, `client_id`, and encrypted `client_secret`, `access_token`,
`refresh_token`, plus `expires_at`. Encryption uses the same `crypto.Encryptor`
as secret headers; DTOs expose status only, never token material.

_Why project-wide:_ `kb.mcp_servers` is project-scoped and device config is
fanned out per project; one connected account per server row is the model the
rest of the registry already has. Per-user tokens would need a second table plus
per-caller resolution in both the proxy and the agent tool pool — deferred (see
tasks.md "Follow-ups").

### D4 — Browser flow: authorize endpoint + public callback, mirroring data sources

- `POST /api/admin/mcp-servers/:id/oauth/authorize` (authenticated, project
  member) → performs discovery (cached), registers a client once (persisted),
  creates a pending state via the existing `kb.oauth_pending_states` (provider
  `mcp`, config carries the server id and discovered endpoints), and returns
  `{authorizeUrl, state}`.
- `GET /api/mcp-servers/oauth/callback?code&state` (public, unauthenticated) →
  consumes the state, exchanges the code (with the PKCE verifier and the
  `resource` parameter), persists the token set, invalidates the tool pool,
  syncs tools, then 303-redirects to the UI landing path. `error` from the
  authorization server lands on the same landing path with a generic error.

_Why:_ it is exactly the shape already proven for data sources, so the trust
anchor is the unguessable single-use state and no session cookie is needed on
the API origin. The gateway only needs to learn an authorize URL and redirect.

_Rejected:_ exchanging the code in the web-ui gateway. It would split OAuth
knowledge across two apps, require the gateway to reach the authorization
server, and leave CLI/device flows with no server-side path.

### D5 — Refresh at the transport boundary, single-flight

`ProxyManager` resolves headers through one helper: static headers (plaintext +
decrypted secrets) and, when an OAuth credential exists, a valid access token —
refreshed when within the expiry skew or after a 401 from a call, with a
per-server single-flight lock so concurrent tool calls cannot stampede the
token endpoint. A refresh failure returns an actionable error naming the
`Connect` action rather than a bare 401.

_Why:_ `WithHTTPHeaders`/`WithHeaders` are already how the proxy authenticates;
keeping OAuth on the same path means every connect site (pooled HTTP, pooled
SSE, ephemeral inspect, discovery) behaves identically.

### D6 — Device-targeted servers receive a resolved bearer

`resolveDeviceConfig` injects `Authorization: Bearer <access>` for a
device-targeted server that has an OAuth credential, refreshing best-effort
first. The value stays inside `DeviceServerConfig` (already documented as
never-logged / never-DTO).

_Why:_ the device cannot refresh a token it does not own; the server already
holds the credential and already pushes decrypted headers.

## Risks

- **Redirect URI stability.** Dynamic client registration binds the redirect URI
  at registration time, so the public API origin must be configured
  (`MCP_REGISTRY_OAUTH_REDIRECT_BASE_URL`) and stable per deployment. Unset
  disables Connect with an actionable error (same convention as
  `SourceOAuthConfig`).
- **Confidential vs public clients.** The authorization server may return a
  `client_secret`; both `client_secret_basic` and `client_secret_post` are
  supported on exchange/refresh, and a PKCE-only public client must work too.
- **Credential leak surface.** Access/refresh tokens and the client secret are
  encrypted at rest, excluded from every DTO, and redacted from logs.
- **False positives.** Only a 401/403 carrying an OAuth challenge becomes
  `auth_required` (D1).

## Live provider finding (Figma, recorded post-merge)

Live verification against `https://mcp.figma.com/mcp` exposed two defects in the
merged flow, fixed in this change:

- **Figma advertises a registration endpoint but rejects DCR with 403.** Figma's
  authorization-server metadata lists a `registration_endpoint`, but a dynamic
  client registration request is answered `403`. Figma documents that only
  clients listed in the Figma MCP Catalog may connect, so a dynamically
  registered client is never issued. The original flow turned this into an
  opaque 500 `internal_error`; it now surfaces as an actionable 400
  (`ErrOAuthClientRequired`) naming the HTTP status and the pre-registered-client
  remedy.
- **The supported fallback is the encrypted `X-Figma-Token` secret-header path.**
  Because Figma only allows catalog-listed clients, the OAuth connect flow cannot
  obtain a working client for it. Figma remains connectable via its existing
  encrypted `X-Figma-Token` secret-header configuration. For authorization
  servers that do issue pre-registered clients, the new
  `PUT /api/admin/mcp-servers/:id/oauth/client` provisioning path stores the
  client id and (encrypted) secret so the authorize flow can skip registration.
