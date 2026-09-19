# Tasks

## 1. Align the packaged PostgreSQL client with the database image

- [ ] 1.1 In `deploy/self-hosted/Dockerfile.server` runtime stage (`FROM alpine:3.21`), add `ARG PG_CLIENT_MAJOR=17` with a comment stating the major must match the database image major in the compose files
- [ ] 1.2 Replace `postgresql16-client` with `postgresql${PG_CLIENT_MAJOR}-client` in the runtime `apk add` list
- [ ] 1.3 Pass `--build-arg "PG_CLIENT_MAJOR=${PG_CLIENT_MAJOR:-17}"` in `deploy/self-hosted/build.sh`, matching the existing arg style
- [ ] 1.4 Add `build.args.PG_CLIENT_MAJOR: ${PG_CLIENT_MAJOR:-17}` to the `server` build block in `deploy/self-hosted/docker-compose.local.yml`
- [ ] 1.5 Add a sync comment beside `image: pgvector/pgvector:pg17` in `deploy/self-hosted/docker-compose.yml` referencing `PG_CLIENT_MAJOR`

## 2. Preflight the pg_dump ↔ server version pairing

- [ ] 2.1 Add pure helpers to `apps/server/domain/scheduler/database_backup_task.go`: `pgMajorFromVersionNum(int) int`, `pgDumpMajorFromVersionOutput(string) (int, error)`, `checkPgDumpCompatible(dumpMajor, serverMajor int) error` with an actionable mismatch message naming `PG_CLIENT_MAJOR`
- [ ] 2.2 Add `(*DatabaseBackupTask).preflightPgDump(ctx)`: `exec.LookPath("pg_dump")`, `pg_dump --version`, `SELECT current_setting('server_version_num')::int`, compare, log versions on success
- [ ] 2.3 Call the preflight in `Run()` after the `running` record insert and before `runBackup`, assigning the error so a mismatch is persisted on the record

## 3. Retention and stream cleanup

- [ ] 3.1 Restructure `Run()` so `enforceRetention` runs after every attempt (success or failure), still log-only on error, and `backupErr` is still returned
- [ ] 3.2 In `runBackup`, close the stdout pipe with `pw.CloseWithError(cmdErr)` on `pg_dump` failure so the uploader aborts instead of finalizing an empty object
- [ ] 3.3 Defensively delete the computed object key when `pg_dump` failed but the upload reported success; log a warning on any deletion error; never replace the `pg_dump failed: … (stderr: …)` error
- [ ] 3.4 Confirm goroutine ordering is unchanged (wait cmd → close writer/CloseWithError → close stderr writer → drain stderr → drain upload channel → close reader) with no deadlock

## 4. Surface backup health

- [ ] 4.1 Add `classifyDatabaseBackup(status string, errMsg *string, startedAt *time.Time, now time.Time) Check` to `apps/server/domain/health/handler.go`: `completed` → healthy; `failed` → unhealthy with truncated message; stale `running`/`pending` (>6h) → unhealthy; fresh running → healthy; unknown → healthy
- [ ] 4.2 Add a concurrent `database_backup` check in `runChecks` querying the newest `kb.database_backups` row (`status, error, started_at`); no rows → healthy `no backups recorded yet`; query error → healthy with `backup status unavailable: <err>`
- [ ] 4.3 Add `"database_backup"` to `optionalComponents` in `Health` so a failing backup degrades (HTTP 200) rather than returning 503

## 5. Tests

- [ ] 5.1 New `apps/server/domain/scheduler/database_backup_task_test.go`: table tests for `pgMajorFromVersionNum`, `pgDumpMajorFromVersionOutput` (valid + malformed), `checkPgDumpCompatible` (equal, older client, newer client)
- [ ] 5.2 Extend `apps/server/domain/health/handler_test.go` with `classifyDatabaseBackup` cases: completed, failed (truncation), stale running, fresh running, unknown status

## 6. Documentation

- [ ] 6.1 In `deploy/self-hosted/README.md`, document the `PG_CLIENT_MAJOR` ↔ database image coupling, the failure mode of a mismatch, the `/health` `database_backup` check (`degraded` when failing), and that failed rows are visible at `GET /api/superadmin/database-backups`

## 7. Verify

- [ ] 7.1 `gofmt -l ./domain/scheduler ./domain/health` reports no files (module root `apps/server`)
- [ ] 7.2 `go build ./...` from `apps/server` passes
- [ ] 7.3 `go vet ./domain/scheduler/... ./domain/health/...` passes
- [ ] 7.4 `go test ./domain/scheduler/... ./domain/health/...` passes
- [ ] 7.5 `sh -n deploy/self-hosted/build.sh` passes; `docker compose -f deploy/self-hosted/docker-compose.yml config` renders if Docker is available
- [ ] 7.6 Manual/structural check: `grep -n "postgresql" deploy/self-hosted/Dockerfile.server` shows `${PG_CLIENT_MAJOR}-client` and no remaining hard-coded 16
