# 2026-09-05 — Fix project settings "memory returned no current project"

## Goal
Fix the Project Settings page error *"memory returned no current project"*, reported when opening `/settings` for a signed-in web session.

## Outcome
Done. Root cause found, fixed, tested, committed (`f5cb1db`), pushed to `master`.

The error came from `uiProjectSettings` → `MemoryClient.GetCurrentProject` → `GET /api/projects/current`, which is **token-bound** on the Memory side: it only returns a project when the bearer token is a project-scoped API key (`user.APITokenProjectID`). Web-console sessions use a Zitadel user access token, which is never project-bound, so Memory returned `project: null` → "memory returned no current project". This is the same class of bug as the backups fix (`0a537f6`): the token-bound current-project lookup is not a valid bridge for web sessions.

A second, compounding issue: the settings client methods (`ListAgentOverrides`, `SetAgentOverride`, `DeleteAgentOverride`, `GetProjectSetting`, `SetProjectSetting`, `DeleteProjectSetting`) used the static `m.projectID` instead of the session-aware `projectIDFor(ctx)`, so web-session reads/writes would have targeted the wrong (empty) project even after the load error was fixed.

## Decisions
- **Resolve the project by the session's active project id first** (`projectIDFor` → `GET /api/projects/:id`), falling back to the token-bound `GET /api/projects/current` only when no id is resolvable (API-key path) — mirrors the backups `backupsOrgID` session-first resolution.
- **Scope the settings client methods to the active project via `projectIDFor(ctx)`** — the static `m.projectID` was correct only for the single-project API-key path.
- **Keep `/current` as the fallback, not delete it** — an account-level API key with no static project still resolves (or returns null) through the token-bound path.

## Changes
- `gateway/settings.go` — `GetCurrentProject` rewritten: id-first (`projectIDFor` → `GET /api/projects/:id`), `/current` fallback; the six project-scoped settings methods switched from `m.projectID` to `m.projectIDFor(ctx)`.
- `gateway/settings_test.go` — `TestGetCurrentProject` updated for the id-first path; `TestGetCurrentProjectNoProject` switched to an empty static project to exercise the `/current` fallback; added `TestGetCurrentProjectSessionProject` and `TestGetProjectSettingSessionProject` (session project id wins over static).
- `openspec/changes/add-project-settings-ui/design.md` — decision #2 rewritten to document the session-first resolution and the token-bound `/current` limitation.

## Verification
- `go build ./...` (in `gateway/`) — pass.
- `go test ./...` — pass; new/updated tests green.
- `gofmt -l gateway/settings.go gateway/settings_test.go` — clean.
- `golangci-lint run ./...` — 0 issues.
- `git push origin master` — `81aef52..f5cb1db`.

## Open questions / follow-ups
- **Browser verification deferred** — the fix needs a live signed-in session (Zitadel + a Memory deployment with a real project) to confirm the page loads the project record and that remember/dedup/overrides read/write the correct project.
- **"No active project" UX** — after the fix, the "memory returned no current project" error only remains for the genuine case (web session with no active project AND no static project). That message is now misleading; a clearer "select a project first" state (or redirect to the project picker) is a possible follow-up.
- **`add-project-settings-ui` OpenSpec change not archived** — its `tasks.md` checkboxes remain and the change dir needs moving to `archive/`.

## Tasks
- [verify-project-settings-browser](../tasks/verify-project-settings-browser.md) — manual browser verification of the Project Settings page for a signed-in session.
- [archive-add-project-settings-ui](../tasks/archive-add-project-settings-ui.md) — check off + archive the OpenSpec change.
