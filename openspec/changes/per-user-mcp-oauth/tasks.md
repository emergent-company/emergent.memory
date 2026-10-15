# Tasks — per-user-mcp-oauth

## 1. Storage (per-user rows, shared fallback)

- [x] 1.1 Add migration `00243_per_user_mcp_server_oauth.sql`: drop the
  `server_id` primary key, add nullable `user_id`, add a unique partial index on
  `(server_id) WHERE user_id IS NULL` (at most one shared row) and a unique index
  on `(server_id, user_id) WHERE user_id IS NOT NULL` (at most one row per user),
  plus a `user_id` lookup index. Down migration restores the original PK.
- [x] 1.2 Add `UserID *string` to `MCPServerOAuth` and drop the `pk` tag from
  `ServerID` (the identity is now the composite uniqueness, not a literal PK).
- [x] 1.3 Store methods: `FindOAuthForUser(serverID, userID)` (per-user row with
  shared fallback), `FindOAuthByServerIDs(projectID, serverIDs, userID)` (same
  fallback per server), and add `userID` to `UpsertOAuth`, `UpdateOAuthTokens`,
  `DeleteOAuthByServerID`, `UpsertOAuthClient`, `ClearOAuthClient`.
- [ ] 1.4 Verify the migration applies and rolls back against a real Postgres;
  confirm an existing connected row survives as the shared row. (Not exercised in
  this change; covered by CI/dev apply on merge.)

## 2. Runtime resolution (proxy)

- [x] 2.1 Resolve the calling user from the request context and pass it into
  `oauthAuthorizationHeader` so the outbound bearer is the caller's credential,
  falling back to the shared row; device config keeps resolving the shared row.
- [x] 2.2 Key the connection pool by `(server, user)` when a user is present so
  two users never share a pooled connection; refresh/close evicts every key for
  the server, and call failure evicts only the failing connection.
- [x] 2.3 Unit-test header resolution — per-user over shared, shared fallback,
  no credential (`TestOAuthAuthorizationHeader_PerUserCredentialWinsOverShared`,
  `TestOAuthAuthorizationHeader_NoCredential`) — and that closing a server evicts
  all of its pooled connections (`TestCloseServerEvictsAllPerUserConnections`).

## 3. Connect / status / disconnect (service + handlers + DTOs)

- [x] 3.1 `StartMCPOAuth` binds the pending flow to the user and writes to the
  user's row; `CompleteMCPOAuth` writes the token set to the user named by the
  pending state.
- [x] 3.2 `MCPOAuthStatus`/`DisconnectMCPOAuth`/`ProvisionOAuthClient`/
  `ClearOAuthClient` take the caller's user id: status reports the resolved
  identity (shared vs personal `userId`), disconnect deletes only the caller's
  own row (never another user's), and provision/clear target the caller's row.
- [x] 3.3 DTOs: status and summary carry the owner identity (`userId`, `shared`)
  and never token material; the list summary reflects the caller's state.
- [x] 3.4 DB-backed service tests cover: status reports personal vs shared
  identity (`TestPerUserOAuthPersonalResolvesOverShared`), disconnect removes
  only the caller's credential (`TestPerUserOAuthDisconnectRemovesOnlyCallerRow`),
  and no endpoint leaks a token (`TestStatusNeverExposesTokenMaterial`).

## 4. SDK + CLI

- [x] 4.1 `mcpregistry` SDK client: `Authorize`, `Status`, `Disconnect` methods
  and their response/request types (status never carries a token).
- [x] 4.2 CLI `mcp-servers oauth` command group: `connect` (authorize → open
  browser → poll status until connected), `status` (print identity/scope/expiry),
  `disconnect`. Reuse the CLI's browser-open + polling conventions.
- [x] 4.3 SDK client tests for the three methods; CLI structure tests for the
  command group.

## 5. Verification

- [x] 5.1 `cd apps/server && go build ./... && go test ./domain/mcpregistry/...`
- [x] 5.2 Migration-order guard (`scripts/preflight/` migration check).
- [x] 5.3 `cd apps/cli && go build ./... && go test ./...`
- [x] 5.4 `openspec validate --all`
