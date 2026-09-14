# Verify account identity display in browser

**Status:** done
**Created:** 2026-09-08
**Superseded by:** [2026-09-08-p0-e2e-coverage](../sessions/2026-09-08-p0-e2e-coverage.md)
Superseded by e2e `account-menu-ui.spec.ts` (avatar trigger, dropdown name+email, profile card email).
**Source:** [2026-09-08-account-identity-display](../sessions/2026-09-08-account-identity-display.md)

## What

Manually verify the account identity UI + email fallback in a live browser (DevTools or a real device), signed in as the user who reported the missing email:

1. Topbar shows **only** the avatar (no name text) — the avatar alone reads as the account trigger.
2. Opening the avatar dropdown shows the name on line 1 and the **email** on line 2 in the active-account row.
3. The profile page (`/profile`) identity card shows the display name on line 1 and the **email** on line 2 (no repeated name, no blank second line).

## Why

The changes (`0a78ab7`, `09062ef`) were verified by `templ generate` + `go build` + `go test` + lint only. The dev environment's `localhost` resolves to the user's machine, so no live browser check was possible during the session. The reporter originally saw email on neither surface, and this pass confirms the Memory-profile fallback resolves it for their account.

## Depends on

none

## Notes

- If the email still does not render after this fix, the account likely has no email registered in Memory's `user_emails` either (the fallback is only as good as the profile data) — investigate the user's records in the Memory backend.
- Topbar trigger evolved after this session's commit: the chevron was removed (`add7993`) and an avatar ring-hover added (`57adf6b`) by later sessions — avatar-only intent is unchanged.
