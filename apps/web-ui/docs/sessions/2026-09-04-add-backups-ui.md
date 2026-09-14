# 2026-09-04 — Add Backups page to the gateway

## Goal
Implement the `add-backups-ui` OpenSpec change: a gateway web UI page to list/create/download/delete project backups (backed by Memory's `/api/v1` backups domain), surfacing status, progress, size, stats, and integrity checksums.

## Outcome
Done. Backups page implemented, verified, merged to `master`, and pushed. Parallel worktree lanes reconciled along the way.

- Implementation landed in an isolated worktree (`omos/add-backups-ui`) because `master` held a parallel session's dirty WIP in files the change needed to touch (`memory.go`, `main.go`, `backend.go`).
- `@fixer` produced the implementation but its session errored on the final report; the code was complete and I verified it independently (build + 30+ backups tests + templ + lint all clean) before trusting it.
- Merged cleanly (auto-merge, zero conflicts) after committing the pre-existing dirty WIP separately.

## Decisions
- **Isolated worktree for the implementation** — `master` had a live parallel session's uncommitted settings/providers work in shared files; a worktree kept the lanes from colliding.
- **New `backups.go` file instead of editing `memory.go`** — avoided the exact file the parallel lane was editing; kept the backups client methods self-contained.
- **Download = browser redirect to Memory's presigned URL, never followed by the client** — `http.ErrUseLastResponse` + read `Location`; preserves Memory's 1-hour signed-URL semantics and avoids proxying large archives.
- **404 on list degrades to "unavailable", not an error** — Memory gates the backup routes behind `Config.Features.Backups`; only 404 maps to unavailable, other failures surface as real errors.
- **No restore action wired** — Memory's restore is a hardcoded 501; the page shows a "not yet available" note instead of a failing button.
- **Commit the pre-existing dirty WIP (`add-org-members-ui` client methods) as its own commit before merging** — user said "commit everything"; kept it a separate, accurately-scoped commit rather than sweeping it into the backups merge.

## Changes
- `gateway/backups.go` (new) — `Backup` struct + `ListBackups`/`GetBackup`/`CreateBackup`/`DownloadBackup`/`DeleteBackup` client methods, org-id resolution from the active project, `uiBackups`/`uiBackupCreate`/`uiBackupDownload`/`uiBackupDelete` handlers, display helpers.
- `gateway/backups.templ` (new) — list + inline create form (`includeDeleted`/`includeChat`/`retentionDays`), status/progress/size/checksum badges, per-row download/delete, "no backups"/"unavailable"/error states, poll-while-creating.
- `gateway/backups_test.go`, `gateway/backups_ui_test.go` (new) — httptest client tests + templ render tests.
- `gateway/backend.go` — `MemoryBackend` interface gains the 5 backup methods.
- `gateway/main.go` — `/backups` UI routes (GET list, POST create, GET `:id/download`, POST `:id/delete`).
- `gateway/ui.go` — sidebar "Backups" entry.
- `gateway/handlers_test.go` — `fakeMemory` backup methods.
- `gateway/webui/static/css/app.css` — regenerated tailwind output (now includes `backups.templ` classes).

## Verification
- `go build ./...` (in `gateway/`) — pass.
- `go test ./...` — pass; 30+ backups-specific tests green. (Earlier pre-existing failures `TestRenderRunPageChatBubbles`/`TestRenderRunPageEmptyTranscript` were fixed by the parallel lane's `f72b08b`.)
- `templ generate` — clean, `updates=0`.
- `golangci-lint run ./...` — clean for changed files; 1 pre-existing finding (`auth_ui_test.go` De Morgan) remains.
- `git push origin master` — `417c80f..bdd2913` pushed.

## Open questions / follow-ups
- **Backups page not browser-tested** — task 4.2 of the change is deferred: needs a running Memory with `Config.Features.Backups` enabled + Zitadel sign-in (real `ZITADEL_*` values from `emergent-infra`).
- **`add-backups-ui` OpenSpec change is implemented but not archived** — `openspec/changes/add-backups-ui/tasks.md` checkboxes are still all `[ ]`; the change dir should be checked off and moved to `archive/`.
- **Spec page list is stale beyond this session** — `docs/spec/04-go-application.md` §3 "Web UI" omits several existing pages (Documents, Skills, Objects, Schedules, Usage, etc.) in addition to the newly-added Backups; only Backups was added here, the rest is pre-existing drift.
- A live parallel session is mid-flight on `add-org-members-ui` §2 (handlers/routes): untracked `org_members_handlers.go` + `org_members_handlers_test.go` and modified `auth_session_test.go`/`handlers_test.go`/`main.go` remain uncommitted on `master` — not mine, left untouched.

## Tasks
- [archive-add-backups-ui](../tasks/archive-add-backups-ui.md) — check off + archive the OpenSpec change.
- [verify-backups-ui-browser](../tasks/verify-backups-ui-browser.md) — manual browser verification against a live Memory.
