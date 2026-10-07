## 1. Update job execution

- [x] 1.1 Implement `action = "update"` in the existing job runner: resolve the
      applied version for `(project, blueprint_name)` and re-fetch upstream via
      the SSRF-guarded fetcher (decrypted PAT for private repos).
- [x] 1.2 No-op when upstream is not newer (version/checksum unchanged) →
      `succeeded` with `result.noop = true`; never create a duplicate version.
- [x] 1.3 Otherwise create + publish a new version carrying the recorded
      `source_kind`/`source_url`/`source_ref` provenance and apply it (merge) to
      the project.
- [x] 1.4 Idempotent under retry: treat duplicate name+version create (`409`) as
      already-created and continue to publish/apply.
- [x] 1.5 Reuse the single `FOR UPDATE SKIP LOCKED` claim loop and the existing
      attempts/backoff; do not add a second runner.

## 2. Job-status exposure

- [x] 2.1 Extend `GET /api/blueprints/:name/updates` with the latest job
      (`latestJob`: action, status, attempts, lastError, createdAt/startedAt/
      finishedAt, result).

## 3. API

- [x] 3.1 `POST /api/blueprints/:name/updates/apply` → enqueue an `update` job
      and return its status (same auth guards).
- [x] 3.2 Reject a duplicate enqueue while an update job is pending/running with
      `409`.
- [x] 3.3 Swagger annotations for the new route.

## 4. CLI

- [x] 4.1 `memory blueprints update <name>` enqueues the update job.
- [x] 4.2 `--wait` polls `GET .../updates` until terminal and prints the outcome.

## 5. Web UI

- [x] 5.1 Gateway client + handler + route for apply and `latestJob`.
- [x] 5.2 Live Update action on repo-imported installed rows; reflect
      queued/running/succeeded/failed from `latestJob` (persistent across reloads).

## 6. Tests

- [x] 6.1 DB-backed: newer upstream → new version created (with provenance) +
      published + applied + job `succeeded`.
- [x] 6.2 DB-backed: up-to-date → `noop`/`succeeded`, no new version.
- [x] 6.3 DB-backed: failure → `failed` + attempts/backoff.
- [x] 6.4 Handler/API tests for `POST .../apply` and `latestJob` in `GET .../updates`.
- [x] 6.5 Gateway: client/handler/render tests for the Update action
      (available/pending/running/succeeded/failed, 409 flash, reload enrichment).

## 7. Verification

- [x] 7.1 `go build ./...` from `apps/server`.
- [x] 7.2 `./scripts/lint-ratchet.sh` within baselines.
- [x] 7.3 `go test ./domain/blueprints/... ./domain/scheduler/...` (DB-backed) passes.
- [x] 7.4 `gofmt -l` clean on changed files.
- [x] 7.5 `openspec validate add-blueprint-update-apply --strict` passes.
- [x] 7.6 No migration required (confirmed), or a unique migration number above `00225` + Goose well-formed.
