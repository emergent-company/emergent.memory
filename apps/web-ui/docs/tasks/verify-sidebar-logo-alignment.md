# Verify sidebar logo / topbar vertical alignment

**Status:** proposed
**Created:** 2026-09-05
**Source:** [2026-09-05-sidebar-logo-alignment](../sessions/2026-09-05-sidebar-logo-alignment.md)

## What
In a signed-in browser session, confirm the sidebar logo ("M" monogram + "Memory" text) is vertically centered at the same height as the topbar elements (search trigger, project switcher, account menu).

## Why
The fix (sidebar header `h-14 items-center` + `#_layout-topbar { height: 100% }`) was verified by DOM/CSS reasoning only. The model cannot read screenshots and the DevTools browser could not complete Zitadel OIDC sign-in, so no pixel-level confirmation exists.

## Depends on
none

## Notes
- Dev URL: `http://alfred-dev.tail0358fa.ts.net:8095` (or `localhost:8095`); `AUTH_MODE=session`, sign-in via Zitadel.
- Expected: logo monogram center ≈ topbar search-button center (within <1px due to the topbar wrapper's 1px `border-b`).
- If off, revisit `gateway/sidebar_user.templ` header classes and `#_layout-topbar` rule in `gateway/webui/css/app.css`.
