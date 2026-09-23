## Why

The stale-run reaper (`Repository.MarkStaleRunsAsError`, driven by `StaleRunReaper`) keyed a run's liveness solely off `kb.agent_runs.started_at`. Any run still `working` (the DB value for `RunStatusRunning`) longer than the 30-minute threshold was terminal-failed, even while actively making progress. Long but legitimate runs — large sandbox builds, many MCP round-trips — were falsely marked `failed`.

Adding a per-step `last_step_at` heartbeat alone is not sufficient: the executor's per-step callbacks only fire at model/tool boundaries, so workspace provisioning before the pipeline, and any single long blocking model or tool call, still had no heartbeat and could be reaped. A run whose executor goroutine died must still be reaped, so the heartbeat has to be tied to the executor's lifetime rather than to a particular code path.

## What Changes

- Add `kb.agent_runs.last_step_at timestamptz` (nullable, no default) as the run's activity heartbeat, mapped as `AgentRun.LastStepAt`.
- `Repository.TouchRun` writes `last_step_at = now()` for rows still in `working` status (a heartbeat that races a terminal transition cannot resurrect liveness on a finished run).
- `Repository.StartRunHeartbeat` runs a run-lifetime ticker that refreshes `last_step_at` every `defaultRunHeartbeatInterval` (2 min, well under the 30-min threshold) until stopped. All three executor entry points (`Execute`, `ExecuteWithRun`, `Resume`) start it before workspace provisioning and stop it on return, so provisioning and long blocking phases count as activity.
- `MarkStaleRunsAsError` keys the idle check off `COALESCE(last_step_at, started_at)`: a heartbeating run is spared, while a run whose heartbeat stopped — or one that never heartbeated — is still reaped once the threshold elapses.
- `MarkOrphanedRunsAsError` (startup recovery) is unchanged: it still fails all `working` rows on boot.
- Keep the DB-backed test schema in sync (`internal/testdb/schema.sql`) and add a DB-backed regression test plus DB-free heartbeat tests.

## Capabilities

### New Capabilities
- `agent-run-liveness`: the agent-run heartbeat/reaper contract — what counts as an active run, which runs may be terminal-failed, and the guarantee that genuinely abandoned runs are still reaped.

### Modified Capabilities
- None.

## Impact

### Code Changes
- `apps/server/migrations/00177_add_agent_runs_last_step_at.sql`: add nullable `last_step_at` to `kb.agent_runs`.
- `apps/server/domain/agents/entity.go`: map `LastStepAt`.
- `apps/server/domain/agents/repository.go`: `TouchRun` (running-only status guard), `StartRunHeartbeat`, reaper `COALESCE(last_step_at, started_at)` predicate.
- `apps/server/domain/agents/stale_run_reaper.go`: `defaultRunHeartbeatInterval`.
- `apps/server/domain/agents/executor.go`: start run-lifetime heartbeat in `Execute`, `ExecuteWithRun`, `Resume` before provisioning; keep per-step touches as fast-path refreshes.
- `apps/server/internal/testdb/schema.sql`: add `last_step_at` so DB-backed tests match the migration.
- Tests: `stale_run_reaper_test.go` (DB-free SQL/heartbeat), `stale_run_reaper_db_test.go` (DB-backed behaviour).

### Operational Impact
- A long run whose executor goroutine is alive is no longer falsely failed; the reaper only fires once the heartbeat (or start, for never-heartbeat runs) is older than the threshold.
- A worker that dies mid-run stops ticking and is still reaped after the threshold — no immortal `working` rows.
- Trade-off: the heartbeat tracks executor-goroutine liveness, not forward progress. A goroutine that is alive but deadlocked/blocked forever now keeps its run fresh and is no longer reaped by the idle sweep; that failure mode needs per-operation timeouts, not the reaper. Unclean shutdowns remain covered by `MarkOrphanedRunsAsError` at startup.
- Cost: one `UPDATE kb.agent_runs` per active run every 2 minutes while the run is alive.
