## Context

Every `core.api_tokens` row today has a mandatory `project_id` FK to `kb.projects`. The `validateAPIToken` middleware always reads that field and stores it as `APITokenProjectID` in the request context; `RequireProjectScope` then uses it to enforce that the token can only touch that one project. There is no way to authenticate across multiple projects with a single token.

The goal is to support account-level tokens — tokens owned by a user but not bound to any project — while keeping all existing project tokens working exactly as before.

## Goals / Non-Goals

**Goals:**
- Make `project_id` nullable in `core.api_tokens` without touching existing rows
- Add new API routes `POST /api/tokens`, `GET /api/tokens`, `DELETE /api/tokens/:tokenId` for account token management
- Update `validateAPIToken` and `RequireProjectScope` middleware to pass account tokens through to any project the user belongs to
- Introduce `projects:read` and `projects:write` scopes
- Update CLI `memory tokens create/list/revoke` so `--project` is optional

**Non-Goals:**
- Frontend/UI changes (no admin UI for account tokens in this change)
- Organization-level tokens (account tokens are per-user, not per-org)
- Changing existing project token behavior, naming, or format
- Token rotation or expiry

## Decisions

### Decision: Nullable project_id with partial unique index
Rather than a separate `account_tokens` table, we extend `core.api_tokens` with `project_id` nullable. This keeps a single token lookup path in `validateAPIToken` and avoids duplicating the hash/prefix storage and revocation logic.

The unique constraint changes from `UNIQUE (project_id, name)` to a partial unique index:
```sql
CREATE UNIQUE INDEX api_tokens_user_name_unique
  ON core.api_tokens (user_id, name)
  WHERE revoked_at IS NULL;
```
This enforces uniqueness per user (not per project), allows revoked tokens to share a name, and handles NULL `project_id` correctly (the old constraint would have allowed duplicate `(NULL, name)` pairs since NULL ≠ NULL in SQL).

**Alternative considered:** A separate `account_tokens` table — rejected because it would require duplicating token hashing, prefix logic, revocation, and `validateAPIToken` branching.

### Decision: Middleware: empty APITokenProjectID = account token
`validateAPIToken` sets `APITokenProjectID` only when `project_id IS NOT NULL`. An empty `APITokenProjectID` in the context is the signal that the token is account-level.

`RequireProjectScope` currently rejects any request where the token's `APITokenProjectID` doesn't match the URL's `:projectId`. The update: if `APITokenProjectID == ""`, skip the project-match check and proceed (the user's org/project membership check, already present, still applies).

No new context key is introduced — the existing zero-value of `APITokenProjectID` becomes the sentinel.

**Alternative considered:** A separate boolean `IsAccountToken` context key — rejected as redundant; the empty string is sufficient and keeps the middleware diff minimal.

### Decision: New routes under /api/tokens (no project in path)
Account token routes live at `/api/tokens` (not `/api/projects/:projectId/tokens`) to make it clear they are not project-scoped. These routes require JWT auth (`RequireAuth`) but not `RequireProjectID`.

```
POST   /api/tokens          — create account token
GET    /api/tokens          — list caller's account tokens
DELETE /api/tokens/:tokenId — revoke caller's account token
```

Existing `/api/projects/:projectId/tokens` routes are untouched.

### Decision: New scopes projects:read and projects:write
These are added to `ValidApiTokenScopes` in `entity.go`. They govern whether an account token can call project-listing or project-mutation endpoints. The `scopeImplies` map in `middleware.go` is extended so `projects:write` implies `projects:read`.

All pre-existing scopes (`schema:read`, `data:read`, `data:write`, `agents:read`, `agents:write`) remain valid for both project tokens and account tokens.

### Decision: CLI routing by presence of --project flag
`memory tokens create` checks whether `--project` was explicitly supplied:
- If supplied → call `POST /api/projects/:id/tokens` (existing path)
- If omitted → call `POST /api/tokens` (new path)

Same pattern for `list` and `revoke`. This is backward compatible: existing scripts that pass `--project` are unaffected.

## Risks / Trade-offs

- **Middleware regression risk** — `RequireProjectScope` is called on many routes. Changing it to allow empty `APITokenProjectID` must be done carefully: only skip the project-match check, not the scope check. Covered by adding tests for both project-token and account-token paths.

- **NULL unique index behavior** — PostgreSQL partial unique indexes handle NULL `project_id` correctly, but this must be validated in the migration test.

- **Revoked name reuse** — the partial index (`WHERE revoked_at IS NULL`) allows a user to reuse a name after revoking a token with that name. This is intentional and consistent with how project tokens work post-migration, but should be documented.

- **`GET /api/projects` scope gate** — `GET /api/projects` currently works with any authenticated JWT. After this change, `emt_` tokens without `projects:read` scope must be rejected at the middleware layer. This is a narrowing change for token holders; JWT users are unaffected.

## Migration Plan

1. Write Goose migration (e.g. `00025_account_api_tokens.sql`):
   - `ALTER TABLE core.api_tokens ALTER COLUMN project_id DROP NOT NULL;`
   - `ALTER TABLE core.api_tokens DROP CONSTRAINT IF EXISTS <old_unique_constraint_name>;`
   - `CREATE UNIQUE INDEX api_tokens_user_name_unique ON core.api_tokens (user_id, name) WHERE revoked_at IS NULL;`
2. Deploy server with new migration (hot reload picks up code; migration runs on next start).
3. No data backfill needed — existing rows keep their non-null `project_id`.
4. Rollback: the migration is reversible — `DROP INDEX`, re-add old constraint, re-add `NOT NULL`. Existing data is unaffected since no rows will have `project_id = NULL` until a new token is created.

## Open Questions

- Should account tokens also be revocable via `DELETE /api/projects/:projectId/tokens/:tokenId` for convenience, or is the new `/api/tokens/:tokenId` endpoint sufficient? (Current decision: new endpoint only — keeps routing clean.)
- Should we enforce a maximum number of account tokens per user? (Not in scope for this change; can be added via a service-layer check later.)
