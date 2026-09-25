## ADDED Requirements

### Requirement: Active agent runs are not reaped while heartbeating

The stale-run reaper SHALL key a run's idleness off its most recent activity, `COALESCE(kb.agent_runs.last_step_at, kb.agent_runs.started_at)`, evaluated against the configured stale threshold (`staleRunThreshold`, 30 minutes). A run in `working` (running) status whose most recent activity is within the threshold SHALL NOT be terminal-failed, regardless of how long ago it started.

#### Scenario: Long but active run survives the sweep

- **WHEN** a run is in `working` status, started more than the threshold ago, and its `last_step_at` is within the threshold
- **THEN** the reaper leaves its status untouched and does not stamp it with the idle-timeout error message

#### Scenario: Run that never heartbeated falls back to start time

- **WHEN** a run is in `working` status and its `last_step_at` is NULL, with `started_at` older than the threshold
- **THEN** the reaper treats `started_at` as its last activity and terminal-fails it

### Requirement: Heartbeat covers the whole executor lifetime

The executor SHALL refresh a run's `last_step_at` for as long as its goroutine is alive, including workspace provisioning and sandbox build that run before the pipeline, and single long blocking model or tool calls that do not cross a per-step boundary. Every executor entry point (`Execute`, `ExecuteWithRun`, `Resume`) SHALL start the run-lifetime heartbeat before provisioning and stop it when the entry point returns.

#### Scenario: Long provisioning phase is not reaped

- **WHEN** an agent run spends longer than the stale threshold provisioning its workspace, with the executor goroutine still alive
- **THEN** `last_step_at` is refreshed during that phase and the run is not reaped

#### Scenario: Heartbeat stops with the executor

- **WHEN** an executor entry point returns (normally or on error)
- **THEN** its run-lifetime heartbeat stops writing, so a later sweep can still reap the run if it is left non-terminal

### Requirement: Genuinely abandoned runs are still reaped

A run whose heartbeat has stopped — a dead or abandoned executor goroutine — SHALL still be terminal-failed once `COALESCE(last_step_at, started_at)` is older than the stale threshold. The heartbeat SHALL NOT make `working` rows immortal.

#### Scenario: Worker died mid-run

- **WHEN** a run is in `working` status and its `last_step_at` (or `started_at` when never heartbeated) is older than the stale threshold
- **THEN** the reaper marks it failed, sets `completed_at`, and records the idle-timeout error message

#### Scenario: Heartbeat only touches running rows

- **WHEN** a heartbeat write races a run that has already reached a terminal status
- **THEN** the write is a no-op, because `TouchRun` is restricted to rows still in running status

### Requirement: Never-started queued runs are never terminal-failed

The stale-run reaper SHALL only consider runs in running status. A run that has been enqueued but never started SHALL NOT be terminal-failed by the idle-timeout sweep.

#### Scenario: Queued run aged past the threshold stays queued

- **WHEN** a run is in a non-running (queued/submitted) status and older than the stale threshold
- **THEN** the reaper leaves its status untouched

### Requirement: Startup orphan recovery is unchanged

`MarkOrphanedRunsAsError`, which runs at server startup, SHALL continue to fail all runs left in running status by an unclean shutdown, independent of the idle-timeout heartbeat predicate.

#### Scenario: Server restart fails in-flight runs

- **WHEN** the server starts after an unclean shutdown with runs still in running status
- **THEN** those runs are marked failed with the restart error message, without consulting `last_step_at`
