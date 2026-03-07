## 1. Database Migration

- [x] 1.1 Find current unique constraint name on `core.api_tokens` (check `00001_baseline.sql` or query `pg_constraint`)
- [x] 1.2 Create Goose migration `apps/server/migrations/00025_account_api_tokens.sql` that: drops the old `(project_id, name)` unique constraint, alters `project_id` to allow NULL, and creates partial unique index `api_tokens_user_name_unique ON core.api_tokens (user_id, name) WHERE revoked_at IS NULL`
- [x] 1.3 Run migration locally and verify `\d core.api_tokens` shows nullable `project_id` and new index

## 2. Entity and Scopes

- [x] 2.1 In `apps/server/domain/apitoken/entity.go`, make `ProjectID` field `bun:",nullzero"` (or use `*string`) to support NULL
- [x] 2.2 Add `projects:read` and `projects:write` to `ValidApiTokenScopes` in `entity.go`
- [x] 2.3 Add `CreateAccountTokenRequest` struct (no `ProjectID` field) alongside existing `CreateTokenRequest`

## 3. Repository

- [x] 3.1 In `apps/server/domain/apitoken/repository.go`, add `ListByUser(ctx, userID string) ([]APIToken, error)` method that queries tokens where `project_id IS NULL AND user_id = ?`
- [x] 3.2 Add `RevokeByUser(ctx, tokenID, userID string) error` that sets `revoked_at` only if token belongs to the user and has `project_id IS NULL`
- [x] 3.3 Add `CreateAccountToken(ctx, req CreateAccountTokenRequest) (string, *APIToken, error)` — mirrors existing `Create` but inserts with `project_id = NULL`

## 4. Service

- [x] 4.1 In `apps/server/domain/apitoken/service.go`, add `CreateAccountToken`, `ListAccountTokens`, `RevokeAccountToken` methods that delegate to new repository methods
- [x] 4.2 Add interface methods to the service interface (used by handler)

## 5. HTTP Handlers and Routes

- [x] 5.1 In `apps/server/domain/apitoken/handler.go`, add `CreateAccountToken` handler for `POST /api/tokens`
- [x] 5.2 Add `ListAccountTokens` handler for `GET /api/tokens`
- [x] 5.3 Add `RevokeAccountToken` handler for `DELETE /api/tokens/:tokenId`
- [x] 5.4 In `apps/server/domain/apitoken/routes.go`, register the three new routes under the `/api` group with `RequireAuth` but without `RequireProjectID`

## 6. Auth Middleware

- [x] 6.1 In `apps/server/pkg/auth/middleware.go`, update `validateAPIToken`: when token row has `project_id IS NULL`, do not set `APITokenProjectID` in context (leave it empty)
- [x] 6.2 Update `RequireProjectScope`: when `APITokenProjectID == ""`, skip the project-match enforcement (allow the request to proceed to membership check)
- [x] 6.3 Add `projects:write` implies `projects:read` to the `scopeImplies` map
- [x] 6.4 Add middleware guard on `GET /api/projects` (and project-mutating routes) to require `projects:read` / `projects:write` when the request is authenticated via an `emt_` token (check `IsAPIToken` in context)

## 7. SDK Client

- [x] 7.1 In `apps/server/pkg/sdk/apitokens/client.go`, add `CreateAccountToken(ctx, req)`, `ListAccountTokens(ctx)`, `RevokeAccountToken(ctx, tokenID)` methods calling the new routes

## 8. CLI

- [x] 8.1 In `tools/cli/internal/cmd/tokens.go`, make `--project` flag optional on `tokens create`; when omitted, call SDK `CreateAccountToken`; when provided, call existing project-scoped create
- [x] 8.2 Update `tokens list`: when `--project` is omitted, call `ListAccountTokens`; add "Type" column showing "account" or "project"
- [x] 8.3 Update `tokens revoke`: when `--project` is omitted, call `RevokeAccountToken`

## 9. Tests

- [x] 9.1 Add unit tests for `validateAPIToken` with a NULL `project_id` token (context should have empty `APITokenProjectID`)
- [x] 9.2 Add unit tests for `RequireProjectScope` confirming account tokens (empty `APITokenProjectID`) are not rejected by project-match check
- [x] 9.3 Add API e2e test: create account token → use it to access two different projects → verify both succeed
- [x] 9.4 Add API e2e test: account token without `projects:read` cannot call `GET /api/projects`
- [x] 9.5 Add API e2e test: revoke account token → subsequent request returns 401
- [ ] 9.6 Add CLI test: `memory tokens create --name t --scopes projects:read` (no `--project`) creates account token

## 10. Verification

- [x] 10.1 Run `task build` — no compile errors
- [x] 10.2 Run `task test` — all unit tests pass
- [x] 10.3 Run `task test:e2e` — all e2e tests pass including new account-token tests
- [ ] 10.4 Manually verify: create account token, use it against two projects, revoke it, confirm 401
