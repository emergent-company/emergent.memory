# 2026-09-08 — Org landing: projects table with multi-select, bulk delete, row menu

## Goal

Turn the org landing page's project list (`/orgs/:id`) into a data table that supports
multi-select + bulk actions (delete), a per-row "3-dots" action menu, and reuse of the
app's existing go-daisy/DaisyUI patterns.

## Outcome

Done. Six commits (`fad54c2` → `e428ec8`):

- Backend delete wiring: `MemoryClient.DeleteProject` (calls memory `DELETE /api/projects/{id}`, async 202, body ignored), `MemoryBackend.DeleteProject` interface entry, a single `uiDeleteProjects` handler on `POST /projects/delete` (reads repeated `projectId` from form or query + `orgId` for the redirect; single deletes arrive via HTMX, bulk via a plain form), route registration, and `uiActivateProject` made HTMX-aware via `render.RedirectAfterMutation`.
- UI: `OrgLandingPage` populated branch is now a table — select-all + per-row checkboxes (Alpine `count` state on the wrapping form), a bulk "Delete selected" toolbar shown when selection > 0, and a per-row 3-dot menu (Open / Delete). Narrow select column; project name rendered without a leading icon.
- The row menu went through three iterations: go-daisy `ActionMenu` (clipped by table overflow) → go-daisy `Popover` (top-layer, but a visible positioning jump) → **native `popover="auto"`** with capture-phase `toggle` positioning (final, no jump).

## Decisions

- **Native popover API for row menus (D20)** — go-daisy `Popover` positions via `requestAnimationFrame` (a visible right→left jump) and has no bottom-left corner placement; its `Exclusive` scope is per-root so it can't close sibling-row menus. Native `popover="auto"` gives top-layer (never clipped), mutual exclusivity (one open), light-dismiss + Esc, and synchronous capture-phase positioning paints once at the final spot.
- **One delete endpoint, two shapes** — `POST /projects/delete` serves both the row menu (HTMX `HXPost` with `?projectId=&orgId=`) and the bulk form (plain POST with repeated `projectId` + hidden `orgId`), redirecting via `render.RedirectAfterMutation` so HTMX and plain requests both behave.
- **Row menu positioned bottom-left of the trigger** — below + to the left, flipping above near the viewport bottom, clamped to the viewport; computed in JS from `getBoundingClientRect` at `toggle` (open).

## Changes

- `gateway/memory.go` — `MemoryClient.DeleteProject(ctx, projectID)` after `DeleteOrg`.
- `gateway/backend.go` — `DeleteProject` added to the `MemoryBackend` interface.
- `gateway/org_context.go` — `orgPageData.FlashMsg`/`FlashErr`; `uiOrg` surfaces `?deleted=` count flash; new `uiDeleteProjects` handler.
- `gateway/main.go` — `e.POST("/projects/delete", s.uiDeleteProjects)`.
- `gateway/project_ui.go` — `uiActivateProject` success redirect now `render.RedirectAfterMutation` (HTMX-aware).
- `gateway/org_context.templ` — `OrgLandingPage` table; removed `orgProjectActivateRow`; native-popover row menu + positioning script; `@flashToasts`.
- `gateway/handlers_test.go`, `gateway/org_context_test.go` — `fakeMemory.DeleteProject` + `deletedProjectIDs`/`deleteProjectErr`; `TestUIDeleteProjects` (bulk plain, single HTMX, empty selection).
- `gateway/webui/static/css/app.css` — regenerated (tailwind picked up new utility classes).

## Verification

- `templ generate -f org_context.templ` — clean.
- `go build ./...` — passed (my files; intermittently blocked mid-session by an unrelated parallel lane's in-flight `add-account-avatar` work — `oidc.go sc.AvatarUrl`, `backend.go:140` syntax — which that lane later fixed).
- `go test ./...` — `TestUIDeleteProjects` (3 subtests), `TestUIOrgLanding*`, `TestUIDeleteOrg`, `TestUIActivateProjectFromOrgContext`, `TestListProjects*` all pass. One pre-existing unrelated failure `TestRenderProjectSwitcherScriptNotInMenu` (from the project-switcher lane; since fixed by that lane's test update).
- Browser verification — **not done by this session** (app served on the user's tailnet tunnel; no browser access). See `verify-projects-table-browser`.

## Open questions / follow-ups

- Memory's project delete is async (202) — the project may still appear briefly on the post-delete redirect, while the flash reads "Project deleted." Consider softening the flash ("Deletion started") or polling, or accept the eventual-consistency.
- The hand-rolled native-popover dropdown deviates from "every UI element is a go-daisy component" (spec 04). See `go-daisy-popover-corner-placement` for the upstream fix + migration back.

## Tasks

- [verify-projects-table-browser](../tasks/verify-projects-table-browser.md) — manual browser verification of the projects table + delete flow.
- [go-daisy-popover-corner-placement](../tasks/go-daisy-popover-corner-placement.md) — contribute corner placement + sync positioning to go-daisy `Popover`, then migrate the row menu back.
