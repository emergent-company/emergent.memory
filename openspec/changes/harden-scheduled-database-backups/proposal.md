## Why

The scheduled full-database backup (`scheduler.database_backup`) has never produced a single file in the stock self-hosted stack. `deploy/self-hosted/Dockerfile.server` installs the PostgreSQL **16** client while `deploy/self-hosted/docker-compose.yml` ships `pgvector/pgvector:pg17`; `pg_dump` refuses to dump a server newer than itself, so every scheduled run aborts with `aborting because of version mismatch`. Nothing surfaces this: the scheduler logs an `ERROR` and continues, the superadmin backup list returns `failed` rows that look like history, and the `database-backups` bucket stays empty. The likely discovery moment is a restore attempt.

Three defects compound the mismatch rather than being merely cosmetic:

1. Retention (`enforceRetention`) only runs on the success path, so failed rows accumulate forever — the 10-day retention never applies precisely when every run fails.
2. `runBackup` starts the MinIO upload before `pg_dump` is known to have produced bytes; on immediate failure the pipe closes empty and an untracked 0-byte object can land in the bucket with no `storage_key` recorded.
3. No health signal distinguishes a healthy deployment from one whose only backup mechanism has never worked.

The client/server pairing can also drift apart again silently, because the database tag (`pg17`) and the installed client are unrelated.

## What Changes

- **Match the client to the database:** introduce a `PG_CLIENT_MAJOR` build arg (default `17`) in `Dockerfile.server`, install `postgresql${PG_CLIENT_MAJOR}-client`, and wire the arg through the build paths (`build.sh`, `docker-compose.local.yml`) with a sync comment at the compose database image.
- **Fail loudly on mismatch:** the backup task runs a `pg_dump` ↔ server version preflight before dumping. A mismatch is persisted on the backup record with an actionable message (naming `PG_CLIENT_MAJOR`) instead of a raw exit-status error.
- **Retention always applies:** `enforceRetention` runs after every backup attempt, including failures, still log-only on error.
- **No empty/untracked objects:** on `pg_dump` failure the stdout pipe is closed with the error so the uploader aborts, and any object that nevertheless landed is deleted defensively.
- **Backup health is observable:** `/health` gains an optional `database_backup` check derived from the latest `kb.database_backups` row (failed → `unhealthy`, stale `running` → `unhealthy`, otherwise `healthy`). A failing backup degrades the service (HTTP 200 `degraded`) instead of returning 503, and is visible on the existing `/health` surface.

Out of scope: restoring `pg_dump` artifacts (still download-only), per-project backup/restore (unaffected — `domain/backups/` shells no external command), and scheduler metric wiring (`/api/metrics/scheduler` remains a placeholder).

## Capabilities

### New Capabilities

- `database-backups`: scheduling, version-compatibility preflight, storage and retention of the superadmin full-database `pg_dump` backups, plus their health reporting.

### Modified Capabilities

<!-- No existing spec-level behavior changes: project backup/restore and backup export are untouched. -->

## Impact

- `deploy/self-hosted/Dockerfile.server` — `PG_CLIENT_MAJOR` ARG (default 17), `postgresql${PG_CLIENT_MAJOR}-client`
- `deploy/self-hosted/build.sh`, `deploy/self-hosted/docker-compose.local.yml` — pass the build arg
- `deploy/self-hosted/docker-compose.yml` — sync comment on the `pgvector/pgvector:pg17` image
- `apps/server/domain/scheduler/database_backup_task.go` — version preflight, retention on all paths, aborted/empty-upload guard
- `apps/server/domain/scheduler/database_backup_task_test.go` — new unit tests for the version helpers
- `apps/server/domain/health/handler.go` — `database_backup` optional check + classifier
- `apps/server/domain/health/handler_test.go` — classifier tests
- `deploy/self-hosted/README.md` — document the client/server pairing and the new health check
- No API shape change, no migration, no change to project backup/restore behavior.
