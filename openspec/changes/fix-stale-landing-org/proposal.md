## Why

The root landing redirect (`uiRoot`) resolves the session's remembered org
without checking it still exists. When that org is deleted from the backend
while a browser still holds it as the active org, every open of the root path
becomes a redirect loop: `/` → 302 `/o/<dead-org>` → `applyScope` fails closed →
404 → the error page's "Back to home" CTA points back at `/`, which repeats the
cycle. The user is locked out of the UI with no way in except clearing cookies.

## What Changes

- The landing resolver now fetches the caller's live org list ONCE and validates
  the remembered org against it, redirecting to `/o/<remembered>` only when that
  org is still live.
- When the remembered org is gone, the stale session context is cleared
  (`clearContext`) and the resolver falls through to the existing fallbacks —
  the first live org's scoped landing, else the onboarding destination
  `/orgs/new`.
- `applyScope` / `scope.go` are unchanged: a direct request for a `/o/<unknown>`
  scope remains a hard fail-closed 404. Only the landing resolution changes.

## Capabilities

### Modified Capabilities

- `web-routing`: the **Scoped landing redirect** requirement gains a "stale
  remembered org" scenario. The capability is introduced (not yet archived) by
  the `scoped-web-urls` change; this change edits the same delta before it is
  synced.

## Impact

- **Gateway routing** (`org_context.go`): `uiRoot` reorders the org resolution
  to validate the remembered org against `ListOrgs` before redirecting.
- **Session context** (`project_handlers.go`): reuses the existing
  `clearContext`; no new session helpers.
- **Tests** (`org_context_test.go`): new landing-resolution cases for a stale
  remembered org (with and without live orgs), and a re-issue assertion that the
  stale org is cleared from the session.
- **Memory backend**: no API change.
