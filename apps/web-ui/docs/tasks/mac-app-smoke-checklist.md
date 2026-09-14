# macOS app smoke checklist

**Status:** proposed
**Created:** 2026-09-13
**Source:** [2026-09-13-memory-mac-connector](../sessions/2026-09-13-memory-mac-connector.md)

## What
Finish the manual GUI verification of the macOS app on the main Mac (and the
`tool` Mac once signed in):
- Account switcher rows show **name · email · (Prod)/(Dev)** with an active check.
- Project switcher: single-line label, org-grouped dropdown, picker left of the
  person icon (person icon far right).
- Dashboard: equal stat tiles; single-column tools; footer status dot + connected
  project; agents list by name + detail sheet with exact tools.
- Connection: explicit Connect toggle (footer + picker reflect it), tools default
  OFF per project, master "All tools" switch.
- Sign out clears the account/session and stops its engine.
- Relaunch while disconnected starts no engine; relaunch connected starts exactly one.

## Why
Several UI changes were verified only by build/tests and by partial GUI visits;
the remaining items are quick confirmations that guard against regressions.

## Depends on
- `~/Applications/Memory.app` installed (`tools/mac-build.sh --install`).

## Notes
- Signing: secrets are 0600 files, so Keychain prompts are gone; `tools/mac-sign.sh`
  is optional (stable identity for TCC grants).
