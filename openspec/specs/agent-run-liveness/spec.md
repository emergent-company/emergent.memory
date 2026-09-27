# agent-run-liveness Specification

## Purpose
Defines how agent runs are kept alive and reaped: a run heartbeat that shields active runs from the stale-run reaper, heartbeat coverage across the whole executor lifetime, reaping of only genuinely abandoned runs, never terminal-failing never-started queued runs, and unchanged startup orphan recovery.

## Requirements

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

### Requirement: Run lifetime can be detached from the triggering request

The executor SHALL expose a way to run an agent on a context that carries the triggering request's values but not its cancellation. A surface that starts a run whose lifetime must outlive its HTTP request (the chat SSE surface) SHALL use it, so cancelling the request context detaches the stream consumer instead of aborting the run. The run SHALL remain bounded by the existing per-step watchdog and SHALL continue to persist its run record and messages.

#### Scenario: Request cancellation does not abort a detached run

- **WHEN** a chat agent run is started on the detached context and the SSE request context is cancelled while the run is in flight
- **THEN** the run completes with status `success` and its run record is persisted as `success`

#### Scenario: Request-bound runs still abort

- **WHEN** a run is executed directly on a request context (not detached) and that context is cancelled mid-run
- **THEN** the run terminates with status `failed` (the pre-fix binding, retained for non-detached surfaces)

### Requirement: Explicit cancel stops an in-flight detached run by id

The executor SHALL track in-flight runs by run id and expose an explicit cancel by run id. The agent-run cancel endpoint SHALL apply the cancel as a guarded terminal transition of the persisted run row, then route a best-effort stop through the executor so an in-flight, detached run winds down promptly. An explicit stop SHALL be recorded as terminal status `cancelled` with the reached step count and the user-cancel reason — never as a `context canceled` server fault. The registry entry SHALL be removed when the run finishes, and a stale removal SHALL NOT evict a newer run that reused the id.

#### Scenario: Explicit cancel stops a detached run

- **WHEN** an explicit cancel is issued for an in-flight detached run
- **THEN** the run row is transitioned to `cancelled` with `error_message` equal to the user-cancel reason, the executor's registered cancel context is cancelled, and the run finishes with status `cancelled`

#### Scenario: Cancel reason is honest

- **WHEN** a run stops because of an explicit cancel
- **THEN** its terminal status is `cancelled`, not `failed`, and no `context canceled` server-fault message is recorded

#### Scenario: Cancel of a non-in-flight run uses the guarded row transition

- **WHEN** an explicit cancel is issued for a run that is not currently executing in this process
- **THEN** the guarded row transition is applied (it succeeds for a non-terminal row) and the executor reports no in-flight match; the endpoint response reflects whether the row actually transitioned

### Requirement: Terminal transitions are race-safe and authoritative

Every terminal run transition (success, failure, skipped, cancelled) SHALL be guarded to rows that have not yet reached a terminal state, so competing terminal writers cannot overwrite one another. The first writer wins and the later writer is a no-op. The cancel endpoint SHALL report whether it actually transitioned the run, and its response SHALL match the run's terminal status: a cancel that arrives after the run already finished SHALL leave the row untouched and SHALL be reported as not cancelled. The endpoint SHALL NOT clobber a completed run into `cancelled`, nor SHALL a late completion turn a `cancelled` run into `success`.

#### Scenario: Cancel lands before the completion write

- **WHEN** a cancel transitions the run to `cancelled` and the executor's success write then races it
- **THEN** the success write is a no-op and the run's terminal status remains `cancelled`, matching the endpoint's response

#### Scenario: Cancel lands after the completion write

- **WHEN** a run already completed and a cancel is then issued
- **THEN** the guarded cancel changes no rows, the run stays `completed`, and the endpoint reports `cancelled: false` with the terminal status

#### Scenario: Concurrent cancel and complete

- **WHEN** a cancel and a completion execute concurrently against the same run
- **THEN** exactly one terminal transition is applied and the final status is `cancelled` if and only if the cancel reported that it transitioned the row
