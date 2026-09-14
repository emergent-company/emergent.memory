# 2026-09-05 — Remove duplicate sidebar fold button + align logo with topbar

## Goal
Two cosmetic fixes to the app shell layout:
1. Remove the redundant sidebar-hide icon in the sidebar header (the topbar already has an identical toggle).
2. Vertically center the sidebar logo so it sits at the same height as the topbar elements.

## Outcome
Done. Both fixes applied, `templ generate` + tailwind + `go build` all pass.

## Decisions
- Remove the sidebar header fold `<label>` entirely (not relocate it) — the go-daisy `layout.Navbar` already renders the same toggle (`for="_layout-sidebar-toggle-trigger"`), so the sidebar copy was pure duplication.
- Set the sidebar header to `h-14 items-center` — matches the topbar wrapper's `h-14` height and centers the logo.
- Fix the *topbar* side of the misalignment with CSS (`#_layout-topbar { height: 100% }`) instead of editing the shared `go-daisy` dependency — the topbar wrapper (`ui.templ`) is `h-14` but has no `flex items-center`, and the `layout.Navbar` child has no height, so its content was top-aligned; making the Navbar fill its wrapper lets its own `items-center` center the content without touching go-daisy or risking a flex-item width regression.

## Changes
- `gateway/sidebar_user.templ` — dropped the header fold `<label>` (duplicate of the topbar toggle); header is now `flex h-14 items-center gap-3 px-5 shrink-0` with just the logo.
- `gateway/webui/css/app.css` — added `#_layout-topbar { height: 100% }` so the topbar Navbar fills its `h-14` wrapper and vertically centers its content.
- `gateway/webui/static/css/app.css` — compiled tailwind output (regenerated; do not hand-edit).

## Verification
- `templ generate` — clean.
- `go build ./...` (from `gateway/`) — `BUILD_OK`.
- `task build` (from `gateway/`) — templ + tailwind + `go build -o memory .` succeeded.
- `grep '#_layout-topbar' webui/static/css/app.css` — confirmed the new rule compiled into the output.
- Note: visual alignment was verified by DOM/CSS reasoning only — the DevTools browser could not log in (Zitadel OIDC, `AUTH_MODE=session`) and this model cannot read screenshots. A 1px `border-b` on the topbar wrapper leaves ~0.5px sub-pixel difference vs the sidebar header; considered negligible.

## Open questions / follow-ups
- Confirm visually in a signed-in browser that the sidebar logo and topbar elements are centered at the same height (see `verify-sidebar-logo-alignment` task).

## Tasks
- [verify-sidebar-logo-alignment](../tasks/verify-sidebar-logo-alignment.md) — manual browser check of the logo/topbar vertical alignment.
