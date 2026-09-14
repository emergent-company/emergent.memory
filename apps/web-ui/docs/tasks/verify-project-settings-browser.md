# Verify Project Settings page for signed-in web sessions

**Status:** proposed
**Created:** 2026-09-05
**Source:** [2026-09-05-fix-project-settings-current-project](../sessions/2026-09-05-fix-project-settings-current-project.md)

## What
Manually verify the Project Settings page (`/settings`) and its save flows against a live Memory deployment with a signed-in web session (Zitadel user access token + an active project).

## Why
The fix (`f5cb1db`) changed project resolution from the token-bound `GET /api/projects/current` to the session's active project id. Unit tests cover the client path, but no browser test confirms the end-to-end behavior: a signed-in session should load the project record and save remember/dedup/overrides against the active project.

## Depends on
- [deploy-zitadel-auth](deploy-zitadel-auth.md) — needs `AUTH_MODE=session` + Zitadel credentials at deploy time (or an equivalent signed-in dev setup).

## Notes
- Check: project name/info/chat-template/budget render; Remember & Dedup show stored values (not "not set"); overrides list/save; inline field saves (`POST /settings/project/:field`) toast success, not "memory returned no current project".
- Mirrors [verify-backups-ui-browser](verify-backups-ui-browser.md).
