## Why

Programmatic and CI/CD access currently shares a single static `X-API-Key`. Emergent Memory supports scoped, revocable API tokens at both the project and account level; Alfred has no way to manage either.

## What Changes

- Add a UI to list, create, revoke, regenerate, and edit scopes of scoped API tokens.
- Manage both surfaces: project-scoped tokens (Settings sidebar) and account-level tokens (a section on the `/profile` page).
- Show each token's scopes and revocation state; show the plaintext at creation and on regenerate.
- Keep the existing shared `X-API-Key` path working.

## Capabilities

### New Capabilities
- `api-tokens`: create, list, edit-scope, regenerate, and revoke project-scoped and account-scoped API tokens.

### Modified Capabilities
<!-- none -->

## Impact

- `gateway/`: Memory client methods for token CRUD (project + account), handlers, routes.
- `gateway/ui.go` + templ: tokens settings screen (project) and a `/profile` section (account).

## Scope

- **In scope**: project-level and account-level tokens; list/create/revoke/regenerate/edit-scopes.
- **Out of scope**: token expiry management, `admin:all` grant UI for non-org-admin users, ephemeral sandbox tokens.
