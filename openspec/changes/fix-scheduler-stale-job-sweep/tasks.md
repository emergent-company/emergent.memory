## 1. Sweep predicate

- [x] 1.1 Extract `cleanupStaleJobsQuery(cfg, cutoff)` returning the UPDATE statement + bind args, with no database dependency
- [x] 1.2 Reap only `processing`/`running` rows for tables with `started_at` (`started_at IS NOT NULL AND started_at < ?`)
- [x] 1.3 Retain a defensive `created_at < ?` branch for a swept table with no `started_at` column (none currently); `pending` is excluded in that branch too
- [x] 1.4 Confirm `pending` never appears in any generated statement
- [x] 1.5 Add `started_at` to `kb.email_jobs` (migration 00173) and stamp it at dequeue in `email/jobs.go`, so `kb.email_jobs` uses the same stale-start rule as the other four tables

## 2. Mass-reap visibility

- [x] 2.1 Add `massReapThreshold` to `StaleJobCleanupTask` with `Set`/`Get` and default 1000
- [x] 2.2 Add `StaleJobMassReapThreshold` to `Config` via `STALE_JOB_MASS_REAP_THRESHOLD`
- [x] 2.3 Wire the config field through `ProvideStaleJobCleanupTask`
- [x] 2.4 Emit `ERROR` log with `alert=mass_stale_reap` when a per-table reap exceeds the threshold
- [x] 2.5 Add `scheduler_stale_jobs_reaped_total{table}` counter (`metrics.go`)

## 3. Tests

- [x] 3.1 Unit test `cleanupStaleJobsQuery` for started_at tables: excludes `pending`, targets `processing`/`running`, one cutoff arg
- [x] 3.2 Unit test `cleanupStaleJobsQuery` for `kb.email_jobs`: excludes `pending`
- [x] 3.3 Unit test `cleanupTable` generated SQL via a DB-free fake driver
- [x] 3.4 Unit test mass-reap alert fires above the threshold and not below it
- [x] 3.5 Unit test `SetMassReapThreshold` clamping and per-table stale-minute override
- [x] 3.6 Unit test that the in-process `scheduler_stale_jobs_reaped_total` counter records reaped tables after a mass-reap run

## 4. Docs & spec

- [x] 4.1 Update `apps/server/domain/DOMAIN_GUIDE.md` task table
- [x] 4.2 Add this OpenSpec change and its `scheduler-stale-job-cleanup` delta spec

## 5. Verification

- [x] 5.1 `go build ./...` in `apps/server`
- [x] 5.2 `go test ./domain/scheduler/...`
- [x] 5.3 `golangci-lint run ./domain/scheduler/...` (scoped). Note: full-repo `golangci-lint run ./...` reports pre-existing issues unrelated to this change.
