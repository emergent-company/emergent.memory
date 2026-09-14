# 2026-09-05 — Fix backups org resolution for web sessions

## Goal
Investigate and fix the `/backups` page error: *"no organization is bound to the active project — backups are org-scoped in the connected Memory service"* (reported at `alfred-dev.tail0358fa.ts.net:8095/backups`).

## Outcome
Done. Root cause found, fixed, tested, committed (`0a537f6`), pushed to `master`.

The error affected **every signed-in user**, not just org-less projects. `backupsOrgID` resolved the org via `MemoryClient.GetCurrentProject` → `GET /api/projects/current`, which is **token-bound** on the Memory side: it only returns a project when the bearer token is a project-scoped API key (`user.APITokenProjectID`). Web-console sessions use a Zitadel user access token, which is never project-bound, so `GetCurrentProject` returned `nil` → `errBackupsNoOrg`. The unit tests missed it because they inject a fake `GetCurrentProject` that returns a project with an org.

A second, compounding bug: the project picker cleared the org on switch (`uiActivateProject` passed an empty org; `activateProject` set `claims.OrgID = ""`), so the session's own `OrgID` was also empty after switching projects.

## Decisions
- **Resolve org from the session context first** (session `OrgID` → project→org lookup → API-key `GetCurrentProject` fallback) — a user access token is never project-bound, so the token-bound current-project lookup is not a valid bridge for web sessions.
- **Recover the org via `ListProjects` when the session carries only the project id** — `ListProjects` already returns `ProjectRef.OrgID`; avoids re-expanding the session cookie with a second tenancy value (the D3 alternative-rejected rationale still holds).
- **Project picker recovers + persists the org on switch (best-effort)** — fixes the empty `X-Org-ID` after switching; an unresolved org still leaves it empty rather than erroring.
- **API-key path unchanged** — still resolves via the token-bound `GetCurrentProject`, which is authoritative for a project-scoped key.

## Changes
- `gateway/backups.go` — `backupsOrgID` rewritten: session context first (org → project listing → `errBackupsNoOrg`), API-key `GetCurrentProject` fallback; added `projectOrgID` helper (resolves a project's org via `ListProjects`).
- `gateway/project_handlers.go` — `activateProject` recovers the project's org via `projectOrgID` instead of unconditionally clearing it.
- `gateway/project_ui.go` — `uiActivateProject` recovers the org (best-effort) and passes it to `activateProjectInSession`.
- `gateway/backups_ui_test.go` — 4 new `backupsOrgID` tests (session org, session project, unknown project, no project).
- `gateway/project_handlers_test.go` — updated `TestActivateProjectReissuesSessionCookie` to assert org recovery; added `TestActivateProjectUnknownOrg`.
- `openspec/changes/add-backups-ui/design.md` — updated D3 to document the session-first resolution and the token-bound current-project limitation.

## Verification
- `go build ./...` (in `gateway/`) — pass.
- `go test ./...` — pass; all new/updated tests green (`TestBackupsOrgID*`, `TestActivateProject*`).
- `gofmt -l` on changed files — clean.
- `golangci-lint run ./...` — 0 issues on my files. (One pre-existing gofmt finding in `handlers_test.go` from a parallel session's `create-objects-from-ui` WIP — not mine, left untouched.)
- `git push origin master` — `4754208..0a537f6`.

## Open questions / follow-ups
- **Browser verification still deferred** — `verify-backups-ui-browser`; now unblocked for signed-in users (the org error was the blocker). Still needs a live Memory with `Config.Features.Backups` + Zitadel credentials.
- **`add-backups-ui` OpenSpec change still not archived** — `archive-add-backups-ui`; `tasks.md` checkboxes remain `[ ]` and the change dir needs moving to `archive/`.
- A parallel session committed `create-objects-from-ui` (`b2edb70`) concurrently; its `handlers_test.go`/`objects*` files are unrelated to this fix.

## Tasks
- [verify-backups-ui-browser](../tasks/verify-backups-ui-browser.md) — manual browser verification (now unblocked).
- [archive-add-backups-ui](../tasks/archive-add-backups-ui.md) — check off + archive the OpenSpec change.
