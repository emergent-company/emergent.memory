## Why

The run-cancel registry added by #1149/#1165 (`AgentExecutor.Cancel` → per-process `runCancelRegistry`) is per-process. In a multi-instance deployment a cancel served by instance A cannot stop a run executing on instance B: the row is marked `cancelled` while the run keeps going (side effects continue, and under #1165's guarded writes its later terminal write is a silent no-op). The archived design for #1149 documented this as the follow-up limitation (issue #1166).

## What Changes

- Make cancellation **durable in Postgres**: cancelling a run is a guarded UPDATE that moves a non-terminal row to the existing intermediate `cancelling` status, visible to every instance. No new column and no migration — the status vocabulary and guard already exist from #1165.
- The executing instance **observes its own persisted status at step boundaries** (pipeline start, before each model step, and before its terminal write) and stops, then takes the guarded terminal transition to `cancelled`.
- Make terminal writes **cancel-aware**: a success/skip/failure that races a committed cancel resolves the run to `cancelled` in the same atomic UPDATE (`CASE WHEN status = 'cancelling' THEN 'cancelled' ELSE <intended> END`), so a committed cancel can never be turned into `completed` or `failed`, even if it lands between the executor's status read and its write.
- Resolve **orphaned `cancelling` rows**: startup recovery finalizes every `cancelling` row to `cancelled`, and the stale-run reaper finalizes idle `cancelling` rows to `cancelled` — a committed cancel always resolves to `cancelled`, never to a failure.
- The per-process registry remains as a **fast-path notification** only (it promptly unblocks a goroutine in the same process); correctness no longer depends on it.

## Capabilities

### New Capabilities

<!-- None. -->

### Modified Capabilities

- `agent-run-liveness`: cancellation is durable and cross-instance; the executor observes the persisted `cancelling` status at step boundaries; terminal writes are cancel-aware; orphaned `cancelling` runs are finalized.

## Impact

- `apps/server/domain/agents/repository.go` — `RequestRunCancellation`, `RunIsCancelling`, `FinalizeCancellingRuns`; cancel-aware terminal writers (`CompleteRun*`, `FailRun*`, `SkipRun`); guarded `SetRunCancelling`.
- `apps/server/domain/agents/executor.go` — `durablyCancelling` observation at pipeline start, per-step (before-model callback), and post-loop; the cancel registry stays as a fast-path notification.
- `apps/server/domain/agents/handler.go` — `CancelRun` commits the durable `cancelling` intent and reports it; local registry is notified best-effort.
- `apps/server/domain/agents/stale_run_reaper.go`, `module.go` — finalize orphaned/stale `cancelling` rows as `cancelled`.
- No schema migration: reuses the existing `kb.agent_runs.status` values (`working`, `cancelling`, `cancelled`).
