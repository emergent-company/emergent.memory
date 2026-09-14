# Audit context-changing navigations that leave shell chrome stale under hx-boost

**Status:** done
**Created:** 2026-09-08
**Source:** [2026-09-08-prg-toast-titles](../sessions/2026-09-08-prg-toast-titles.md)

## What

hx-boost swaps only `#main-content`. Session-context chrome (topbar org/project
switcher, org sidebar, account state) lives outside it, so any navigation or
POST that changes the session context must be a true full-load PRG or the shell
shows stale context.

Org delete was fixed this way (`hx-boost="false"` on the danger-zone form, see
commit `8c8dea4`). Audit the remaining flows:

- org / project switching via GET links into an org context (`/orgs/:id`, picker
  rows) — do these re-issue the session and full-load, or boost-swap leaving the
  topbar pointing at the previous context?
- account switch / sign-out flows
- any other handler that mutates session context then redirects

## Why

Users get a shell that disagrees with the page content (the deleted-org bug that
broke `org-delete-ui`). Deterministic fix per flow: full-load PRG
(`hx-boost="false"` + native `onsubmit` confirm where a confirm is wanted) or an
`HX-Refresh` on the redirect target.

## Depends on

- D19 / D23 in [docs/spec/00-vision.md](../spec/00-vision.md)

## Notes

- Precedent: project-transfer modalShell and the org-delete danger-zone form
  already opt out of boost.
- Test harness: `org-delete-ui.spec.ts` (deleted org must vanish from the
  topbar) and the org/project switching read specs.

## Resolution (2026-09-09)

Full audit of every context-changing flow (org/project switch, account switch,
sign-out, create flows, invite accept/decline, avatar/profile) against the
boost scope (`<main id="main-content" hx-boost hx-target="#main-content">`
in `ui.templ`). Most flows are already safe:

- Project switch (topbar rows + org-landing "Open" + `/orgs` rows) → shell-native
  or `RedirectAfterMutation` (HX-Redirect full load).
- Org entry (`GET /orgs/:id` via topbar/server redirects) → shell-native full load.
- Account switch / sign-out (`account_menu.templ`, rendered only in the topbar)
  → shell-native forms.
- Org delete + avatar modals + transfer modal → already `hx-boost="false"`.

One real defect found and fixed:

- **Org creation** — the `/orgs/new` form lives inside the boosted
  `#main-content` and `uiCreateOrg` issued a plain 303. A boosted submit was
  followed by htmx as a GET that swapped only `#main-content`, leaving the
  topbar org switcher + sidebar on the old org. Fixed by switching
  `uiCreateOrg`'s success redirect to `render.RedirectAfterMutation`
  (`gateway/org_members_ui.go`) — HTMX requests now get `HX-Redirect` (full
  page load into the new org context), plain POSTs keep the 303. Unit test
  added in `gateway/org_members_ui_test.go` (HTMX → 200 + HX-Redirect).
  Orphaned `POST /orgs/:id/activate` route (`main.go`/`project_ui.go`) noted
  but left untouched (no UI references it; separate cleanup).
