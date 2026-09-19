## Context

`DatabaseBackupTask` (`apps/server/domain/scheduler/database_backup_task.go`) runs on the scheduler's `database_backup` cron (default `0 0 7 * * *`, `domain/scheduler/config.go:105`) and shells a bare `pg_dump -Fc`, streaming stdout into MinIO bucket `database-backups` while recording rows in `kb.database_backups` (migration `00091`). Records are the only history surface: `GET /api/superadmin/database-backups` lists them and `/{id}/download` presigns the object.

The failure is a packaging mismatch, not a logic bug: the image's client major (16) is older than the database server major (17), which `pg_dump` refuses by design. Because it is a packaging mismatch, the durable fix is to make the two majors move together and to make a future mismatch impossible to miss.

## Goals / Non-Goals

**Goals**

- The stock self-hosted stack produces a real dump on its next scheduled run.
- A client/server major mismatch is impossible to reintroduce silently, and when it does occur it is loud, actionable, and visible on `/health`.
- Failed runs cannot accumulate forever and cannot leave untracked objects.

**Non-Goals**

- Implementing restore of `pg_dump` artifacts (they stay download-only).
- Changing project backup/restore (`domain/backups/`), which uses no external command.
- Replacing `pg_dump` with a container-exec or in-process approach: the server container has no access to the database container's binaries, and adding a Docker socket dependency to the backup path would widen the trust boundary.

## Decisions

### D1 — `PG_CLIENT_MAJOR` build arg instead of a hard-coded package name

Install `postgresql${PG_CLIENT_MAJOR}-client` with `ARG PG_CLIENT_MAJOR=17` in the runtime stage, and pass the arg from `build.sh` and `docker-compose.local.yml`. Rationale: the version is then a single named knob at the build boundary rather than a string buried in an `apk add` list, and the compose comment at the database image states the invariant for operators. A hard-coded `postgresql17-client` would fix today's instance of the bug while leaving the next drift just as invisible; the ARG gives reviewers and future bumps a place to look.

Alpine 3.21 repositories carry `postgresql17-client-17.11-r0` (verified), so the default needs no base-image change. The database image tag stays `pg17` — a mutable tag, but one whose major cannot change without a deliberate compose edit.

### D2 — Preflight inside the task, recorded on the backup row

`preflightPgDump` runs after the `running` row is inserted and before the dump: `exec.LookPath("pg_dump")`, parse `pg_dump --version`, read `current_setting('server_version_num')`, compare majors. On mismatch it returns an error naming both majors and `PG_CLIENT_MAJOR`, so the failure lands in `kb.database_backups.error` and is visible through the existing list endpoint without log access. Running it after the insert means the very first failed run is self-describing; running it before the insert would leave an empty history.

Parsing and comparison live in unexported pure functions (`pgMajorFromVersionNum`, `pgDumpMajorFromVersionOutput`, `checkPgDumpCompatible`) so they are unit-testable without a database or a `pg_dump` binary.

Alternative rejected: a startup-only check that refuses to boot. A version mismatch affects one background task, not the service; refusing to start would convert a degraded backup into a dead deployment.

### D3 — Retention runs on every path

`enforceRetention` moves out of the success-only branch. Retention deletes rows and objects older than 10 days regardless of status, which is the existing intent; the bug was that it was unreachable while backups failed. Errors stay log-only so a retention failure never masks a successful backup.

### D4 — Abort the upload on failure, then delete defensively

`runBackup` starts the MinIO upload concurrently with `pg_dump` (streaming by design, to avoid buffering a full dump). On `cmdErr != nil` the stdout pipe is closed with `pw.CloseWithError(cmdErr)` so the uploader observes a read failure and aborts mid-stream rather than finalizing a truncated or empty object. If the upload nevertheless reports success, the object under the computed key is deleted defensively, and any deletion error is logged — the original `pg_dump failed: … (stderr: …)` error is always the one returned, so diagnostics are never replaced by cleanup noise. Goroutine ordering is unchanged: wait for `pg_dump` → close writer (with error on failure) → close stderr writer → drain stderr → drain upload channel → close reader.

### D5 — Health check is optional (degraded), derived from the newest row

`/health` gains a `database_backup` check running `SELECT status, error, started_at FROM kb.database_backups ORDER BY created_at DESC LIMIT 1`. It is classified as an **optional** component, so a failing backup yields HTTP 200 `degraded` rather than 503: the API is still fully functional, and turning a configuration defect into a failed liveness/readiness probe would take the deployment down for a problem that does not affect request serving. Classification is an unexported pure function so all states are unit-tested.

Staleness uses a 6-hour threshold on `started_at` for `running`/`pending`, which is far above the normal dump duration and low enough to catch a wedged run within a day. A query error (for example, an older schema without `kb.database_backups`) is reported as `healthy` with an explanatory message: a probe must never manufacture an outage, and the table's absence is already covered by the backup task's own failure path.

## Risks / Trade-offs

- **Mutable `pg17` tag** — if the upstream `pgvector/pgvector:pg17` image ever retags to a new major while the compose file still says `pg17`, the preflight catches it immediately and reports the actionable message instead of failing obscurely.
- **`PG_CLIENT_MAJOR` default could drift from compose** — mitigated by the sync comment at the database image and by the runtime preflight, which fails loudly rather than silently.
- **Health probe cost** — one indexed `LIMIT 1` query per `/health` call, alongside the existing database ping. `kb.database_backups` is tiny and indexed on `created_at DESC`.
- **Cross-compilation cache** — changing the installed client package invalidates the runtime layer; a build-time cost only, no runtime effect.

## Migration Plan

No schema or API migration. Existing `failed` rows are harmless and age out under the corrected retention path. Operators rebuild/pull the patched `memory-server` image and the next scheduled run succeeds; the previous failures remain visible as history in `/api/superadmin/database-backups`.

## Open Questions

None blocking. A future change could add a `pg_dump`-artifact restore path (noted as out of scope in `backup-restore`) and wire the placeholder `/api/metrics/scheduler` endpoint to real task status.
