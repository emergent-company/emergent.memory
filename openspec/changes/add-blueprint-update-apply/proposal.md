## Why

Change 2 (`add-blueprint-update-checks`) detects and surfaces when an applied
GitHub-imported blueprint has a newer upstream version, but a user cannot act on
that signal. Change 3 adds the apply half: a user-triggered, durable background
job that fetches the newer upstream version, creates and publishes it, and
applies it to the project — plus job-status exposure so callers can follow
progress instead of guessing.

## What Changes

- Implement the `update` action in the existing durable job runner: resolve the
  applied version for `(project, blueprint)`, re-fetch upstream through the
  SSRF-guarded fetcher, no-op when already current, otherwise create + publish a
  new version carrying the recorded provenance and apply it to the project.
  Idempotent under retry; failures retry with the same attempts/backoff as
  `check`.
- `POST /api/blueprints/:name/updates/apply` enqueues an `update` job; a second
  enqueue while one is pending/running is rejected with `409`.
- `GET /api/blueprints/:name/updates` additionally returns the **latest job**
  (`latestJob`: action, status, attempts, lastError, timestamps, result), so
  clients can render persistent queued/running/succeeded/failed state.
- CLI: `memory blueprints update <name> [--wait]`.
- Web UI: a live **Update** action on repo-imported installed rows that triggers
  apply and reflects the latest job's status.

## Capabilities

### Modified Capabilities

- `blueprint-api`: user-triggered async apply of an available update, with
  idempotent/no-op semantics and update-job status exposure.
- `blueprint-gallery`: an Update action on the installed list that triggers apply
  and reflects job progress from `latestJob`.

## Impact

- **Server** (`apps/server`): `domain/blueprints` — `update_check.go` (update
  action), `update_check_store.go` (latest-job lookup), `handler.go`, `routes.go`,
  `entity.go`, `repository.go`. **No migration** — the existing
  `kb.blueprint_update_jobs` table already supports `action='update'`.
- **CLI** (`apps/cli`): `blueprints update` subcommand.
- **Web UI** (`apps/web-ui`): gateway client/handler/route for apply and
  `latestJob`, and the installed-row Update action.
- **Tests**: DB-backed apply (newer → created+published+applied; up-to-date →
  no-op; failure → retry/backoff) + handler/API tests + gateway tests.
