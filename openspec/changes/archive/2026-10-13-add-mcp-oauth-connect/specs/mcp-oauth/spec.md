## Purpose

Let a Memory project authorize an OAuth-protected remote MCP server, so an
installed server that requires authorization can be connected once and used by
every server-side and device-side tool call in the project.

## ADDED Requirements

### Requirement: An authorization requirement is discovered, not guessed

When a connection to a remote MCP server (SSE or streamable HTTP) is rejected
with HTTP 401 or 403 and the response advertises a `WWW-Authenticate: Bearer`
challenge carrying protected-resource metadata (a `resource_metadata` URL or an
`authorization_uri`), the connection result SHALL report an authorization
requirement rather than a generic connection error. The reported requirement
SHALL name the protected resource metadata URL, the authorization server, the
scopes the server accepts, and whether the authorization server offers dynamic
client registration. Metadata SHALL be fetched over the SSRF-guarded outbound
client, and a metadata fetch failure SHALL NOT turn a connection error into an
authorization requirement.

#### Scenario: A 401 challenge advertising resource metadata

- **WHEN** a probe of a remote MCP server answers 401 with a `WWW-Authenticate: Bearer` challenge naming a resource-metadata URL
- **THEN** the result reports status "auth_required" together with the authorization server, accepted scopes, and dynamic-registration support

#### Scenario: A 401 with no OAuth metadata

- **WHEN** a probe answers 401 without an OAuth challenge
- **THEN** the result remains a credential/connection error and no authorization requirement is reported

#### Scenario: Metadata endpoint unreachable

- **WHEN** the challenge names a metadata URL that cannot be fetched
- **THEN** the result remains an error and is not reported as an authorization requirement

### Requirement: A project member can authorize a server in the browser

A project member SHALL be able to start an authorization flow for an MCP server
that reports an authorization requirement. Starting the flow SHALL register a
client with the authorization server when dynamic client registration is offered
(persisting the returned client identifier and, when issued, the client secret),
generate a single-use state and a PKCE (S256) code verifier, persist the pending
flow bound to the server and project, and return an authorization URL that
carries the protected resource, the requested scopes, and the PKCE challenge.
Starting the flow SHALL fail with an actionable error when the deployment's
public redirect origin is not configured.

#### Scenario: Authorize URL returned

- **WHEN** a project member starts the flow for a server that requires authorization
- **THEN** an authorization URL is returned carrying the client identifier, redirect URI, state, PKCE challenge, requested scopes, and the protected resource

#### Scenario: Client registered once

- **WHEN** the flow is started again for a server whose client registration is already stored
- **THEN** no second registration is performed and the stored client identifier is reused

#### Scenario: Deployment not configured

- **WHEN** the public redirect origin is not configured
- **THEN** starting the flow fails with an error naming the missing configuration

#### Scenario: Server does not require authorization

- **WHEN** a project member starts the flow for a server with no authorization requirement
- **THEN** the request fails with a clear error instead of an unusable authorization URL

### Requirement: The callback completes the flow and stores the token set

The authorization callback SHALL be unauthenticated and authenticated solely by
the single-use pending state. It SHALL consume the state exactly once, exchange
the authorization code together with the PKCE verifier and the protected
resource, persist the resulting access token, refresh token (when issued),
expiry, and granted scope for the server, invalidate the server's tool-pool
entry, discover the server's tools, and redirect the browser to the UI landing
path. A missing, expired, or already-consumed state, a failed exchange, or an
error returned by the authorization server SHALL redirect to the landing path
with an error indication, never with a partial credential stored.

#### Scenario: Successful callback

- **WHEN** the callback arrives with a valid state and an authorization code
- **THEN** the token set is stored for the server, the tool pool is invalidated, tools are discovered, and the browser lands on the UI with a success indication

#### Scenario: State replay

- **WHEN** a callback arrives with a state that was already consumed, expired, or never issued
- **THEN** no token is stored and the browser lands on the UI with an error indication

#### Scenario: Provider reports an error

- **WHEN** the authorization server redirects back with an `error` parameter
- **THEN** the browser lands on the UI with an error indication and no credential is stored

### Requirement: The stored credential authenticates server-side tool calls

An external MCP server with a stored OAuth credential SHALL be connected with an
`Authorization: Bearer <access token>` header. When the access token is expired
(or within a short expiry skew), the server SHALL refresh it with the stored
refresh token before connecting and persist the refreshed token set. A refresh
SHALL be single-flight per server, so concurrent tool calls cannot issue
concurrent refreshes. When no valid token can be obtained, the failure SHALL name
the authorization requirement so the UI can offer to reconnect. A server with an
OAuth credential SHALL NOT also send a configured static `Authorization` header.

#### Scenario: Bearer attached

- **WHEN** a tool is called on a server with a stored OAuth credential
- **THEN** the outbound request carries `Authorization: Bearer <access token>`

#### Scenario: Refresh on expiry

- **WHEN** the stored access token is expired and a refresh token is present
- **THEN** the token is refreshed, the new token set is persisted, and the call proceeds with the new access token

#### Scenario: Concurrent refresh

- **WHEN** several tool calls arrive while the access token needs refreshing
- **THEN** a single refresh request is issued and all calls use the resulting token

#### Scenario: Refresh impossible

- **WHEN** no refresh token is stored or the refresh is rejected
- **THEN** the failure states that the server needs to be authorized again

#### Scenario: Static header is not duplicated

- **WHEN** a server has both a configured `Authorization` header and a stored OAuth credential
- **THEN** only the OAuth bearer is sent

### Requirement: Device-targeted servers receive a resolved bearer

A device-targeted MCP server that has a stored OAuth credential SHALL have a
valid `Authorization: Bearer` header included in the resolved configuration
pushed to devices, refreshing the token first when it is expired. The value
SHALL remain confined to the resolved device configuration and SHALL never
appear in a DTO, log line, or status payload.

#### Scenario: Bearer pushed to the device

- **WHEN** configuration is resolved for a device-targeted server with a stored OAuth credential
- **THEN** the pushed header set contains an Authorization bearer for a valid access token

#### Scenario: Expired token refreshed before push

- **WHEN** the stored access token of a device-targeted server is expired
- **THEN** it is refreshed before the configuration is resolved, and the device receives the new token

#### Scenario: Token never leaks

- **WHEN** a server's configuration or status is serialized for any DTO or log
- **THEN** no access token, refresh token, or client secret is included

### Requirement: A project member can inspect and disconnect the credential state

The API SHALL report, per server, whether an OAuth credential is connected, the
authorization server, the granted scope, and the token expiry, and SHALL allow a
project member to delete the stored credential. Deleting the credential SHALL
close the server's pooled connection and SHALL leave any statically configured
headers untouched.

#### Scenario: Status reported

- **WHEN** a project member requests the authorization status of a server
- **THEN** the response reports whether a credential is stored, its authorization server, granted scope, and expiry, and never the token values

#### Scenario: Disconnect

- **WHEN** a project member disconnects a server
- **THEN** the stored credential is deleted and the server's pooled connection is closed

### Requirement: A registration failure surfaces as an actionable error

When dynamic client registration is unavailable (the authorization-server
metadata advertises no `registration_endpoint`) or is rejected by the
authorization server with a non-success HTTP status, starting the flow SHALL
fail with a client-facing 400 error that names the remedy (provision a
pre-registered client) rather than an opaque internal error. The rejected case
SHALL name the HTTP status the authorization server returned.

#### Scenario: Registration rejected

- **WHEN** the authorization server answers a dynamic client registration request with a non-2xx status (e.g. 403)
- **THEN** starting the flow fails with a 400 error naming the HTTP status and the pre-registered-client remedy

#### Scenario: Registration not offered

- **WHEN** the authorization-server metadata advertises no registration endpoint
- **THEN** starting the flow fails with a 400 error naming the pre-registered-client remedy

### Requirement: Authorization-server metadata discovery falls back to OpenID Connect

Authorization-server metadata discovery SHALL consult the RFC 8414 well-known
document, inserting the well-known path between the host and the path of a
path-bearing issuer (RFC 8414 §3.1), and SHALL fall back to the OpenID Connect
discovery document when RFC 8414 metadata is absent. The candidate set SHALL be
tried in order — the challenge's own `authorization_uri` when it is itself a
metadata document, then RFC 8414 (path-inserted, then appended), then OpenID
Connect (path-inserted, then appended) — and SHALL use the first candidate that
returns a parseable document.

#### Scenario: Path-bearing issuer

- **WHEN** the authorization server issuer carries a path (e.g. `https://github.com/login/oauth`)
- **THEN** the RFC 8414 well-known document is sought at `https://github.com/.well-known/oauth-authorization-server/login/oauth` (path inserted between host and path), not appended to the issuer

#### Scenario: RFC 8414 absent, OpenID Connect present

- **WHEN** the authorization server serves OpenID Connect discovery (`.well-known/openid-configuration`) but no RFC 8414 document
- **THEN** discovery resolves the authorization and token endpoints from the OpenID Connect document

### Requirement: A provider without discoverable metadata surfaces an actionable error

When a remote MCP server requires authorization but its provider publishes no
discoverable OAuth metadata at all (neither an RFC 8414 document nor an OpenID
Connect document), starting the connect flow SHALL fail with an actionable 4xx
error naming the token-header remedy, never a 503; and inspecting the server
SHALL report an authorization requirement whose Connect action is disabled and
whose reason (the token-header remedy) is shown. A transient metadata failure
(unreachable server, HTTP 5xx, or an observable 401/403 from a metadata
endpoint) SHALL remain a 503.

#### Scenario: Authorize surfaces the remedy

- **WHEN** a project member starts the flow for a provider with no discoverable metadata
- **THEN** the request fails with a 422 error naming the token-header remedy, not a 503

#### Scenario: Inspect disables Connect

- **WHEN** the server is inspected and its provider publishes no discoverable metadata
- **THEN** the result reports `status:"auth_required"` with Connect disabled and the token-header remedy shown

#### Scenario: Transient failure stays a 503

- **WHEN** a metadata endpoint fails transiently (5xx, unreachable, or 401/403)
- **THEN** the request fails with a 503, not a 422

### Requirement: An admin can provision a pre-registered OAuth client

An admin SHALL be able to provision a pre-registered client identifier (and an
optional client secret) for a remote MCP server. The secret SHALL be encrypted at
rest and never echoed. Starting the flow for a server with a stored client SHALL
skip dynamic client registration and reuse the stored identifier in the
authorization URL. Provisioning a client SHALL clear any token set issued to a
previous client, because those tokens are bound to the client they were issued
to.

#### Scenario: Provision then authorize

- **WHEN** an admin provisions a client id (and optional secret) for a remote server and then starts the flow
- **THEN** no dynamic client registration is performed and the authorization URL carries the provisioned client id

#### Scenario: Provisioning clears prior tokens

- **WHEN** an admin provisions a client for a server that already has a stored token set
- **THEN** the stored access token, refresh token, and expiry are cleared

#### Scenario: Provisioned client reported

- **WHEN** a server has a stored client id
- **THEN** its authorization status reports `clientConfigured` true, independent of any token presence
