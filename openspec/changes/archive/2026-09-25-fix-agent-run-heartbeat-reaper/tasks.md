## 1. Heartbeat column and model

- [x] 1.1 Add migration `00177_add_agent_runs_last_step_at.sql` (`ALTER TABLE kb.agent_runs ADD COLUMN last_step_at timestamp with time zone`, nullable, no default; forward-only Up/Down)
- [x] 1.2 Map `AgentRun.LastStepAt *time.Time` (`bun:"last_step_at,type:timestamptz"`)
- [x] 1.3 Sync `apps/server/internal/testdb/schema.sql` `kb.agent_runs` with the new column so DB-backed tests match the migration

## 2. Heartbeat writes

- [x] 2.1 `Repository.TouchRun` sets `last_step_at = now()` restricted to `status = working` (running) rows
- [x] 2.2 `Repository.StartRunHeartbeat(runID, interval)` runs a run-lifetime ticker refreshing `last_step_at`, returning an idempotent `stop`
- [x] 2.3 Add `defaultRunHeartbeatInterval` (2 min) alongside the reaper threshold/interval constants
- [x] 2.4 Start the heartbeat in `Execute`, `ExecuteWithRun` and `Resume` before `provisionWorkspace`, stopping it on return
- [x] 2.5 Keep the existing per-step/per-tool touches as fast-path refreshes

## 3. Reaper predicate

- [x] 3.1 `MarkStaleRunsAsError` filters on `COALESCE(last_step_at, started_at) < cutoff`, still restricted to running rows
- [x] 3.2 `MarkOrphanedRunsAsError` remains unchanged (fails all running rows at startup)

## 4. Tests

- [x] 4.1 DB-free: reaper keys off `COALESCE(last_step_at, started_at)`, not `started_at` alone
- [x] 4.2 DB-free: orphan reaper still targets all running rows with no activity predicate
- [x] 4.3 DB-free: `TouchRun` sets `last_step_at` and restricts to running rows
- [x] 4.4 DB-free: `StartRunHeartbeat` writes periodically and stops when `stop()` is called (idempotent)
- [x] 4.5 DB-backed: a run with a fresh heartbeat survives a sweep; a run whose heartbeat stopped is reaped; a never-heartbeat run falls back to `started_at` and is reaped; a never-started queued run is untouched

## 5. Docs, spec and verification

- [x] 5.1 Add this OpenSpec change and its `agent-run-liveness` delta spec
- [x] 5.2 `go build ./...` in `apps/server`
- [x] 5.3 `go test -count=1 ./domain/agents/...`
- [x] 5.4 `golangci-lint run --new-from-rev=origin/main ./domain/agents/...`
- [x] 5.5 `gofmt -l` on touched files
- [x] 5.6 Migration 00177 apply/revert on a scratch database
