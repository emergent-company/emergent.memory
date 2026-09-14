# 2026-09-08 — Clickable project rows on the Organizations page

## Goal

Make clicking a project row on the Organizations page (`/orgs`) open (activate) that
project, instead of the row being inert.

## Outcome

Done — one commit `b4d9b03`. Clicking a project row now POSTs `/projects/activate` with the
project's id and lands on the project page (`/agents`), matching the sidebar switcher's
behavior.

## Decisions

- `orgProjectRow` becomes a submit form (hidden `projectId` + a full-row submit button)
  rather than a GET link or JS click handler — project activation mutates the session, so it
  must be a POST; this mirrors the existing `projectSwitchRow` form in `auth_ui.templ`.
- Extended `isOrgContextPath` to also match the exact `/orgs` path (not just the `/orgs/…`
  prefix) — a project switch from the org listing should open the project, not bounce back
  to `/orgs`.
- Clickable affordance via `cursor-pointer` + `group-hover` text brighten +
  `title="Open <name>"` tooltip — minimal and consistent with the existing static tree rows.

## Changes

- `gateway/org_members_ui.templ` — `orgProjectRow` rewritten as a
  `<form method="post" action="/projects/activate">` wrapping a full-row submit button (tree
  line, folder icon, name, and role badge all preserved).
- `gateway/org_members_ui_templ.go` — regenerated from the `.templ` source.
- `gateway/project_ui.go` — `isOrgContextPath` now returns true for `u.Path == "/orgs"` in
  addition to the `/orgs/…` prefix.
- `gateway/org_context_test.go` — `TestIsOrgContextPath` `/orgs` case flipped to `true`.
- `gateway/org_members_ui_test.go` — `TestRenderOrgsPage` asserts
  `action="/projects/activate"` and `name="projectId" value="p1"`, and that there is exactly
  one activate form per project (3 fixtures).

## Verification

- `PATH="/root/go/bin:$PATH" templ generate` — success.
- `go build ./...` (from `gateway/`) — clean.
- `go test ./...` (from `gateway/`) — ok.
- `golangci-lint run ./...` — 0 issues.
- Browser test skipped — dev server not started; `localhost` resolves to the user's machine.

## Open questions / follow-ups

- Manual browser verification outstanding (click a project row → lands on the project page).
  Tracked as `verify-orgs-project-row-click`.

## Tasks

- [verify-orgs-project-row-click](../tasks/verify-orgs-project-row-click.md) — manual browser
  pass over clickable project rows on `/orgs`.
