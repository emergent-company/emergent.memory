## Why

The invitation lifecycle (accept, decline, revoke) has no end-to-end coverage. Accept and decline are the core of the invite flow, but they are untestable today because standalone mode seeds exactly one user and one API key, while `Accept`/`Decline` require the invitee's email to already exist on an authenticated user. This change adds a second standalone identity so the full invite lifecycle can be exercised in CI.

## What Changes

- Seed a second standalone identity (user + email + API key) alongside the primary one.
- Accept the second API key in standalone auth.
- Add API e2e tests covering the invite lifecycle: create validation, list-by-project, revoke, accept (happy + error), and decline (happy + error).

## Capabilities

### New Capabilities

<!-- none -->

### Modified Capabilities

<!-- none — no product requirement changes; this is test-infra + test coverage -->

## Impact

- `apps/server/internal/config/config.go`: `StandaloneConfig` gains `APIKey2` + `UserEmail2`.
- `apps/server/domain/standalone/bootstrap.go`: create the second user + `core.user_emails` row.
- `apps/server/pkg/auth/middleware.go`: `checkStandaloneAPIKey` maps the second key to the second user.
- `e2e/docker-compose.yml`: second-identity env vars.
- `e2e/tests/api/invite_lifecycle_test.go`: new lifecycle tests.
