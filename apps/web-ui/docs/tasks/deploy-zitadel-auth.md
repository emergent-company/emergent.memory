# Deploy Zitadel auth (flip AUTH_MODE=session)

**Status:** proposed
**Created:** 2026-09-04
**Source:** [2026-09-04-add-auth-project-frame](../sessions/2026-09-04-add-auth-project-frame.md)

## What
Ship the opt-in Zitadel browser auth built in `add-auth-project-frame`: set `AUTH_MODE=session`
and the Zitadel config on the deploy target, and verify the full sign-in loop end-to-end.

## Why
The auth flow is implemented and tested (unit + render) but was never exercised against a
real Zitadel instance — `ZITADEL_ISSUER` / `ZITADEL_CLIENT_ID` / `ZITADEL_CLIENT_SECRET` /
`ZITADEL_REDIRECT_URI` values come from `emergent-infra`. OpenSpec task 5.3 (manual browser
sign-in / switch / create / sign out) is blocked on this.

## Depends on
- Zitadel app/client registered in `emergent-infra` with the gateway's redirect URI.
- `SESSION_SECRET` set (random, stable across restarts).

## Notes
- Env: `AUTH_MODE=session`, `ZITADEL_ISSUER`, `ZITADEL_CLIENT_ID`, `ZITADEL_CLIENT_SECRET`,
  `ZITADEL_REDIRECT_URI`, `SESSION_SECRET` (all in the gateway `.env`). Optional
  `SESSION_MAX_AGE` (default 30 days) sets the session-cookie browser lifetime.
- **Zitadel refresh-token lifetime** — the session cookie now outlives the access token
  (see `2026-09-06-session-cookie-lifetime`), but the effective "stay signed in" ceiling
  is Zitadel's refresh-token lifetime. Set the app's refresh-token lifetime to ≥ 30 days
  so the 30-day session actually holds; otherwise users still get logged out when the
  refresh grant expires.
- Rollback = `AUTH_MODE=dev` (no code change).
- Verify the DevTools flow: `/auth/login` → Zitadel → `/auth/callback` → session cookie →
  org-grouped switcher shows real org/projects → switch + create → sidebar footer avatar/name →
  `Sign out`.
