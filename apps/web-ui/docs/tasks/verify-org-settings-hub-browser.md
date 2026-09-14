# Browser-verify the org Settings hub

**Status:** done
**Created:** 2026-09-08
**Superseded by:** [2026-09-08-p0-e2e-coverage](../sessions/2026-09-08-p0-e2e-coverage.md)
Superseded by e2e `org-settings-hub.spec.ts` (rail, danger-zone reachability, tool-settings 301).
**Source:** [2026-09-08-org-settings-hub](../sessions/2026-09-08-org-settings-hub.md)

## What

Manual browser (and/or e2e) pass over the new org Settings hub:

- org sidebar shows Projects / Members / Settings (no separate "Tool settings"/"Delete");
- `/orgs/:id/settings` renders the Tools section with the left rail highlighting Tools;
  override rows toggle/delete and redirect back to `/settings?updated=1`;
- `/orgs/:id/settings/danger-zone` renders the delete-org card, rail highlights Danger zone,
  and Settings stays highlighted in the app sidebar;
- delete-org flow works and, on failure, surfaces the error back on the danger-zone page;
- old `/orgs/:id/tool-settings` URL 301-redirects to `/orgs/:id/settings`;
- picker cogwheel on an org header opens the org Settings hub.

## Why

This session's change was verified via unit HTML assertions, templ mirroring of the
project-settings layout, and updated Playwright specs — but no live server/browser pass was
run, so visual/interaction drift (rail highlight, sticky layout, rail width) is unconfirmed.

## Acceptance

Org Settings hub renders and behaves as described above in a real browser session (and/or the
updated `org.spec.ts` / `org-delete-ui.spec.ts` e2e specs pass against a running gateway).
