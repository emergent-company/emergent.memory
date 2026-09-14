# Manual browser verification of the Backups page

**Status:** proposed
**Created:** 2026-09-04
**Source:** [2026-09-04-add-backups-ui](../sessions/2026-09-04-add-backups-ui.md)

## What
Manually verify the Backups page flows in a browser (DevTools) against a live Memory with `Config.Features.Backups` enabled and Zitadel sign-in configured:

- Navigate to `/backups` → list renders (or clear "no backups" state).
- Create a backup → shows `creating`, polls to `ready`.
- Download a `ready` backup → browser receives the archive via the presigned URL redirect.
- Delete a backup → confirm dialog, row disappears, empty state returns.
- Confirm the "unavailable" state when the Memory feature is gated off.

## Why
Task 4.2 of `add-backups-ui` is the only remaining verification step; it was deferred because it needs real `ZITADEL_ISSUER`/client/redirect values and a live Memory, which are only available at deploy time.

## Depends on
none (external credentials).

## Notes
- The gateway `/backups` route is served behind the session/API-key trust boundary — sign in first.
- Headless smoke check (`GET /backups` → 200) is already achievable; the interactive flows need the real backend.
- The org-resolution bug (signed-in users saw "no organization is bound to the active project") was fixed in [2026-09-05-fix-backups-org-resolution](../sessions/2026-09-05-fix-backups-org-resolution.md); the page now resolves the org from the session/project rather than the token-bound current-project lookup.
