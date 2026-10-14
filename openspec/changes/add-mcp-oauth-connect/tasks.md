# Tasks — add-mcp-oauth-connect

<!-- openspec:archive-hold: task 8.4 cannot complete as written. Figma's authorization server advertises a registration_endpoint but rejects dynamic client registration with 403 (only MCP-Catalog-listed clients may connect), so the browser consent flow is unreachable for it. Verified live against Figma: discovery + the authorization requirement; the fallback is the existing encrypted X-Figma-Token secret-header path. Held until a dynamic-client-capable provider completes a live end-to-end OAuth connect. See design.md "Live provider finding (Figma, recorded post-merge)". -->

## 1. Storage

- [x] 1.1 Add migration `00237_add_mcp_server_oauth.sql` creating `kb.mcp_server_oauth` (unique `server_id`, `authorization_server`, `token_endpoint`, `registration_endpoint`, `resource`, `scope`, `client_id`, encrypted `client_secret`/`access_token`/`refresh_token`, `expires_at`, `created_at`, `updated_at`) with project-scoped RLS policies matching the neighbouring `kb` tables
- [x] 1.2 Add the `MCPServerOAuth` Bun model plus store methods (upsert registration, save token set, read by server, delete by server) and verify they round-trip the encrypted fields
- [x] 1.3 Run `task migrate:up` and verify the migration applies and rolls back

## 2. Discovery

- [x] 2.1 Parse a `WWW-Authenticate: Bearer` challenge (`resource_metadata`, `authorization_uri`, `scope`) and fetch protected-resource metadata then authorization-server metadata over the SSRF-guarded outbound client
- [x] 2.2 Classify a 401/403 inspect result as `auth_required` with the discovered authorization server, scopes, and dynamic-registration support; keep 401-without-challenge and metadata-fetch failures as ordinary connection errors
- [x] 2.3 Expose discovery through the inspect DTO and verify handler tests cover `auth_required`, plain error, and metadata-unreachable paths

## 3. OAuth client

- [x] 3.1 Implement PKCE (S256) state/verifier generation and authorization-URL construction carrying `client_id`, `redirect_uri`, `state`, `code_challenge`, scopes, and the RFC 8707 `resource`
- [x] 3.2 Implement dynamic client registration (persisting the returned client id and, when issued, the client secret) and verify it is performed once per server
- [x] 3.3 Implement code→token exchange with the PKCE verifier and `resource`, supporting both `client_secret_basic` and `client_secret_post`, and implement refresh with the same supports
- [x] 3.4 Unit-test exchange and refresh against an `httptest` authorization server, asserting the exact form fields sent (including `resource` and `code_verifier`) and the persisted expiry

## 4. Endpoints

- [x] 4.1 `POST /api/admin/mcp-servers/:id/oauth/authorize` — authenticated, project-scoped; returns `{authorizeUrl, state}`; creates the pending state in `kb.oauth_pending_states` (provider `mcp`)
- [x] 4.2 `GET /api/mcp-servers/oauth/callback` — public, state-anchored; consumes the state once, exchanges the code, stores the token set, invalidates the tool pool, syncs tools, and 303-redirects to the UI landing path; every failure path redirects with an error indication
- [x] 4.3 `GET /api/admin/mcp-servers/:id/oauth/status` and `DELETE /api/admin/mcp-servers/:id/oauth` — status never exposes token material; delete closes the pooled connection
- [x] 4.4 Add `MCP_REGISTRY_OAUTH_REDIRECT_BASE_URL` and `MCP_REGISTRY_OAUTH_CLIENT_NAME` (with the landing path) to config, and reject `authorize` with an actionable error when the base URL is unset
- [x] 4.5 Verify handler tests cover: authorize success, authorize with an unconfigured deployment, authorize for a server that needs no authorization, callback success, callback with a replayed/expired state, and callback with a provider error

## 5. Runtime credential use

- [x] 5.1 Resolve a single header set per server (static headers + decrypted secrets + OAuth bearer) and use it at every connect site (pooled HTTP, pooled SSE, ephemeral inspect, discovery)
- [x] 5.2 Refresh on expiry and once after a 401, persisting the refreshed token set, with a per-server single-flight lock; verify concurrent-call tests issue exactly one refresh
- [x] 5.3 Ensure a server with an OAuth credential does not also send a configured static `Authorization` header, and that refresh failure yields an actionable "needs authorization" error
- [x] 5.4 Inject a refreshed bearer into the resolved device configuration for device-targeted servers and verify it never reaches a DTO or log

## 6. Registry install

- [x] 6.1 Report an authorization requirement from `InstallFromRegistry` (install still succeeds) and surface it in the install result DTO/message

## 7. Web UI

- [x] 7.1 Render the `auth_required` inspect state with a Connect action and a connected/Disconnect state on the MCP servers page
- [x] 7.2 Add the gateway routes that exchange the connect action for an authorization URL (302) and that disconnect a server (PRG flash)
- [x] 7.3 Report the callback outcome (success/error) from the landing query parameters, and mention the authorization step in the registry install flash

## 8. Verification

- [x] 8.1 `go build ./...` for the server and gateway, plus `templ generate` for changed templ files
- [x] 8.2 `task lint` (server, CLI, web UI, connector) passes
- [x] 8.3 Server unit tests for the new domain code pass; gateway handler/templ tests pass
- [ ] 8.4 Manual verification against a live OAuth provider: install from the registry, see the authorization requirement, complete the browser flow, and confirm tools are discovered and a tool call succeeds — **cannot complete against Figma**: its authorization server advertises a `registration_endpoint` but answers dynamic client registration with `403` (only MCP-Catalog-listed clients may connect), so the consent flow is unreachable. Verified live against Figma: discovery and the authorization requirement. Completion still requires a dynamic-client-capable provider (or a provisioned pre-registered client).

## 10. Post-merge fixes (live Figma verification)

- [x] 10.1 Surface a rejected dynamic client registration as an actionable 400 (`ErrOAuthClientRequired`) carrying the HTTP status and a capped, secret-free excerpt of the response body, instead of an opaque 500
- [x] 10.2 Add `PUT /api/admin/mcp-servers/:id/oauth/client` (provision a pre-registered client id + optional encrypted secret, clearing prior tokens) and `DELETE /api/admin/mcp-servers/:id/oauth/client` (clear the identity), with route-authority and Swagger registration
- [x] 10.3 Report `clientConfigured` on the OAuth status DTO, and make `StartMCPOAuth` reuse a provisioned client without re-registering
- [x] 10.4 Unit + DB-backed tests: rejected registration surfaces status and maps to 400; provisioning stores an encrypted secret, clears tokens, and reports `clientConfigured`; the authorize flow skips registration and uses the provisioned client id; DELETE clears the identity

## 11. OIDC discovery fallback + no-metadata 4xx (#1792)

- [x] 11.1 Fix `metadataURLFor` to the RFC 8414 §3.1 path-insertion form (well-known segment inserted between host and issuer path) and add the legacy append form as an additional candidate
- [x] 11.2 Add OpenID Connect candidate builders (path-insertion and append) and walk the ordered, deduplicated candidate list in step 2 of `discoverAuthRequirement`
- [x] 11.3 Make `getJSON` return a typed `metadataHTTPError` and classify a permanent absence (all candidates 404/410) as `ErrOAuthMetadataUnavailable`, keeping network/decode/5xx/401/403 as transient
- [x] 11.4 Map `ErrOAuthMetadataUnavailable` to an actionable 422 (token-header remedy) in `mcpOAuthError` and wrap it in `discoverServerAuthRequirement`; keep `ErrOAuthDiscoveryFailed` on the 503 path
- [x] 11.5 Extend `InspectAuthDTO` with `Unavailable`/`Remedy` and report an unavailable requirement (Connect disabled) from `classifyAuthRequired`
- [x] 11.6 Unit tests: OIDC fallback, path-insertion, all-404 → unavailable, transient-500 → discovery failure, 401/403 not absence, handler 422/503 mapping, and inspect unavailable DTO
- [x] 11.7 Add a `MCPREGISTRY_LIVE_OAUTH_PROBE=1`-gated GitHub probe exercising the real PRM + OIDC path

## Verification status (recorded at PR time)

Ran and passed:

- Server: `go build ./...`, `go vet ./domain/mcpregistry/...`, `golangci-lint run --new-from-rev` (0 issues), `scripts/lint-ratchet.sh`, `gofmt`.
- Gateway: `task generate`, `task build`, `task test`, `task lint` (golangci-lint 0 issues), `gofmt`.
- Tests: `go test ./domain/mcpregistry/...` (unit + `-race`), the DB-backed `MCPOAuthDBSuite` against a real Postgres, gateway `go test ./...`.
- Migration `00237` applied to a real Postgres (include FK cascade and RLS policy creation), then dropped.
- `swag init` regenerated for the new routes; `.env.example` documents the three OAuth variables.
- Live discovery probe (`MCPREGISTRY_LIVE_OAUTH_PROBE=1`) against `https://mcp.figma.com/mcp` resolved the real authorization server, authorize/token/registration endpoints, `mcp:connect`, and resource identifier.
- Root `task lint` end to end: every job green (server / CLI / web UI / connector `gofmt`, `vet`, `build`, `test`, `golangci-lint`) except `secrets`, which fails on two findings this branch does not introduce — a false positive in the generated, gitignored `apps/web-ui/gateway/project_settings_templ.go` (emitted by `task generate`, match is `templ_7745c5c3_Err`) and the environment-variable name `E2E_SCENARIO_LLM_API_KEY` in the unmodified `apps/web-ui/tests/e2e/helpers/live-chat.ts`. Both files are untouched by this branch.
- `go run ./cmd/route-authority-guard`: 563 routes, all declared, all tiers match.
- Connect-flow tests: `oauth_flow_test.go` (DB-backed: authorize success, no-authorization-required, callback success, replayed state, expired state) and `oauth_handler_test.go` (provider-error redirect) pass against a real Postgres.

Not ran (blocking 8.4):

- 8.4 needs a deployment with `MCP_REGISTRY_OAUTH_REDIRECT_BASE_URL` (and, when the UI origin differs, `MCP_REGISTRY_OAUTH_LANDING_URL`) set, plus a browser session — the redirect URI must be a public origin, so it cannot be exercised locally.
- The runlog API e2e suite (`e2e/tests/api`) was not extended for the connect flow: it requires the Docker e2e stack, and the SSRF guard rejects the loopback stub authorization server the suite would need. Handler coverage (4.5) is instead asserted by `oauth_handler_test.go` (provider-error redirect) and the DB-backed flow tests in `oauth_flow_test.go` (authorize success / no-auth-required / callback success / replayed / expired state) against a real Postgres.

### Manual verification steps (8.4)

> **Provider constraint (post-merge finding).** These steps cannot succeed end to end
> against Figma: it rejects dynamic client registration (`403`), so step 3's consent
> screen is unreachable. Steps 1–2 are verified against Figma; steps 3–5 require a
> provider that issues dynamic clients, or a pre-registered client provisioned via
> `PUT /api/admin/mcp-servers/:id/oauth/client`.

On a deployment whose server has a public origin:

1. Set `MCP_REGISTRY_OAUTH_REDIRECT_BASE_URL` to that origin (and
   `MCP_REGISTRY_OAUTH_LANDING_URL` when the UI origin differs), then restart the
   server. Confirm `<origin>/api/mcp-servers/oauth/callback` is reachable
   *without* a session — it is the provider's redirect target, so it is
   correctly public in the route-authority table.
2. Install a remote entry that needs authorization (e.g. Figma) from
   `/settings/mcp-registry` and confirm the inspect state reports the
   authorization requirement rather than a generic connection error.
3. On the MCP servers page press **Connect**, complete the provider consent
   screen, and confirm the landing returns to `/settings/mcp-servers` with a
   success flash and the row showing its connected/Disconnect state.
4. Confirm the server's tools are discovered and a tool call succeeds, i.e. the
   refreshable bearer is being injected at the transport boundary rather than a
   static header.
5. Disconnect, confirm the row returns to the unconnected state, then reconnect
   once to confirm the flow is repeatable (a replayed callback state must be
   rejected with an error, not silently accepted).
6. Optional: leave the server idle past the access-token expiry and confirm the
   next tool call refreshes rather than failing with a 401.

## Follow-ups (not in this change)

- [x] 9.1 Per-user (rather than project-wide) OAuth credentials — filed as #1783
- [x] 9.2 CLI `memory mcp oauth connect` parity — filed as #1784
- [ ] 9.3 Non-MCP data-source OAuth gaps tracked by `add-data-sources`
