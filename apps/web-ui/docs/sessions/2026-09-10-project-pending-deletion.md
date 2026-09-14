# 2026-09-10 — Project pending-deletion (safe/async delete)

## Goal
Improve project deletion in the gateway UI: add a confirmation before deleting, show
visible progress, and investigate/support Memory's async deletion so projects are first
marked "to be deleted" with a UI badge, a cancel path, and real-time feedback.

## Outcome
Done — full stack merged (deploy left to the user).

- Memory: PR [#421](https://github.com/emergent-company/emergent.memory/pull/421) squashed to `main` as `4822a7a`.
- Gateway: PR [#64](https://github.com/emergent-company/memory.web-ui/pull/64) squashed to `master` as `ae57b18`.
- Deploy to dev (rebuild/publish `:dev` images, run migration) intentionally left to the user.

Investigation found Memory already soft-marked `deleted_at` but then hard-deleted in an
in-process goroutine (no durability, no status surface, pending rows hidden immediately).
This change makes that lifecycle durable and visible.

## Decisions
- **Grace-period soft-delete, not immediate hard delete** — `DELETE` sets `deletion_scheduled_for = now + grace`; a scheduler sweep purges later. Enables a visible badge, cancel/restore, and restart safety (no orphaned soft-deletes).
- **Durable scheduler sweep, not a goroutine** — `project_deletion_sweep` survives restarts and reaps backfilled orphans.
- **Defaults:** grace `1h` (`PROJECT_DELETION_GRACE_PERIOD`), sweep `1m` (`PROJECT_DELETION_SWEEP_INTERVAL`), cron override `PROJECT_DELETION_SWEEP_SCHEDULE`.
- **Idempotent delete** — repeat `DELETE` returns `202 {alreadyPending:true}`; new `POST /api/projects/:id/restore` cancels within grace.
- **Pending surfaced explicitly** — `GET /api/projects?include_pending=true`; default list stays `deleted_at IS NULL`.
- **Per-project guarded purge** (not one bulk `DELETE`) — isolates a poison-pill cascade; the guarded re-check (`deletion_scheduled_for <= now()`) closes the cancel/reschedule TOCTOU.
- **Styled confirm dialog replaces native `hx-confirm`**; pending rows are excluded from delete selection; the page auto-refreshes (~4s) only while something is pending.
- **Deferred, tracked:** object-level authz on delete/restore, and Swagger regen (pre-existing generator drift).

## Changes
Memory (`emergent.memory`, `apps/server/`):
- `domain/projects/{entity,repository,service,handler,routes,module}.go` — `DeletionStatus`/`DeletionScheduledFor` DTO, `MarkPendingDeletion`/`GetDeletionState`/`CancelPendingDeletion`, `RequestDeletion`/`CancelDeletion`, `Restore` handler + `POST /:id/restore`, `include_pending` in list, env-wired grace period.
- `domain/scheduler/{tasks,module,config}.go` — `ProjectDeletionTask` (`project_deletion_sweep`), Go-duration interval config.
- `migrations/00147_project_pending_deletion.sql` — `deletion_scheduled_for` column + partial index + backfill of old soft-deletes + reversible `Down`.
- `domain/projects/deletion_test.go`, `domain/scheduler/project_deletion_task_test.go` — DB-free tests.

Gateway (`memory.web-ui`, `gateway/`):
- `memory.go` / `backend.go` — `ProjectRef.DeletionStatus`/`DeletionScheduledFor` + `PendingDeletion()`, `ListProjectsIncludingPending`, `RestoreProject`.
- `org_context.go` / `main.go` — org page loads pending projects, `uiRestoreProject` + `POST /projects/restore`, flash copy.
- `org_context.templ` (+ regenerated `webui/static/css/app.css`) — styled confirm dialog (row + bulk), busy state, "Scheduled for deletion" badge + purge time, cancel action, auto-refresh.
- `tests/e2e/specs/projects/projects-table-ui.spec.ts` — updated to the dialog + pending/restore flow.
- `docs/tasks/project-delete-authorization.md` + `BACKLOG.md` — tracked authz follow-up.

## Verification
- Memory: `gofmt -l domain/projects domain/scheduler` clean; `go build ./...` OK; `go test -short -count=1 ./domain/projects/... ./domain/scheduler/...` OK; `go vet ...` clean. (Full `./domain/projects` needs `memtest-db` because upstream added a DB-backed transfer suite.)
- Gateway: `templ generate ./...`, `go build ./...`, `go test ./...`, `golangci-lint run ./...` — all green. e2e spec typechecks and lists; full e2e run requires the backend deploy.
- CI: memory `Server Go CI` ran post-merge; gateway `CI`/`Build & Publish` jobs fail immediately on GitHub-hosted runners (account-wide Actions billing block, tracked as `ci-actions-billing-blocker`) — not a code failure.

## Open questions / follow-ups
- **Deploy:** rebuild/publish `ghcr.io/emergent-company/memory-server:dev` and `.../memory-web-ui:dev` on `emergent-dev`, apply migration `00147`, restart `/opt/emergent-dev` compose, then run browser/e2e verification.
- Dev may want a shorter `PROJECT_DELETION_GRACE_PERIOD` to demo the purge transition.
- Memory branch rules block force-push; rebased work was re-pushed as a new branch (`omos/project-pending-deletion-rebased`) → PR #421 (superseding #420).
- Old remote branch `omos/project-pending-deletion` (memory) is the closed PR #420 branch; can be deleted.

## Tasks
- [project-delete-authorization](../tasks/project-delete-authorization.md) — object-level authz on delete/restore.
- [verify-project-deletion-deploy](../tasks/verify-project-deletion-deploy.md) — verify the shipped flow on dev.
- [memory-swagger-docs-regen](../tasks/memory-swagger-docs-regen.md) — regen memory swagger (pre-existing drift).
