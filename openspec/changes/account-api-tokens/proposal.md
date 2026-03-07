## Why

API tokens are currently bound to a single project, making it impossible to build integrations or CI workflows that span multiple projects without managing one token per project. Account-level tokens with project-spanning access are needed so users can authenticate once and operate across all their projects.

## What Changes

- Make `project_id` nullable in `core.api_tokens` (with migration)
- Change unique constraint from `(project_id, name)` to `(user_id, name)`
- Introduce new account-scoped token scopes: `projects:read`, `projects:write`
- Add new API routes for account-level token management: `POST /api/tokens`, `GET /api/tokens`, `DELETE /api/tokens/:tokenId`
- Update `validateAPIToken` middleware: when a token has no `project_id`, it is not restricted to a single project
- Update `RequireProjectScope` middleware: account tokens (no bound project) pass project-scope checks without restriction
- Update CLI `memory tokens create` to make `--project` optional; omitting it creates an account token
- Update CLI `memory tokens list` to show both account and project tokens
- Update SDK client to support account token CRUD

## Capabilities

### New Capabilities

- `account-api-tokens`: Account-level API tokens — creation, listing, revocation via new `/api/tokens` routes; nullable `project_id` in the token schema; new `projects:read` / `projects:write` scopes; middleware updated to allow cross-project access when `project_id IS NULL`

### Modified Capabilities

- `authentication`: Token validation middleware behavior changes — `RequireProjectScope` must now allow tokens with no bound project to pass; `validateAPIToken` no longer unconditionally sets `APITokenProjectID`
- `cli-tool`: `memory tokens create` gains optional `--project` flag; `memory tokens list` shows type column (account vs project)

## Impact

- **Database**: `apps/server/migrations/` — new Goose migration to alter `core.api_tokens`
- **Backend**: `apps/server/domain/apitoken/` — entity, service, repository, handler, routes all updated
- **Middleware**: `apps/server/pkg/auth/middleware.go`, `context.go`
- **SDK**: `apps/server/pkg/sdk/apitokens/client.go`
- **CLI**: `tools/cli/internal/cmd/tokens.go`
- **Breaking**: none — existing project tokens continue to work unchanged; `project_id` becoming nullable is backward-compatible for existing rows
