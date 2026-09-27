## Context

#1165 made every terminal run transition a guarded UPDATE restricted to non-terminal rows (`submitted`, `working`, `input-required`, `cancelling`), so competing terminal writers cannot overwrite one another. The cancel endpoint used that guard to move a run straight to `cancelled`, then notified a per-process registry (`AgentExecutor.Cancel`) to unblock the in-flight goroutine. That registry is the only thing that stops a detached run, and it is process-local: a cancel served by instance A cannot stop a run executing on instance B. The row ends `cancelled` while B keeps executing — the exact failure #1166 reports.

## Decision 1 — Durable signal: the existing `cancelling` status

Cancelling a run writes the intermediate `cancelling` status with a guarded UPDATE (`RequestRunCancellation`):

```
UPDATE kb.agent_runs SET status = 'cancelling'
WHERE id = $1 AND status IN ('submitted','working','input-required','cancelling')
```

This is authoritative in Postgres and visible to every instance. It is idempotent (a row already `cancelling` is matched again and reported accepted) and returns `false` only when the run is already terminal, so the endpoint can keep basing its response on `RowsAffected`.

No migration: `cancelling` is already in the shared `runstatus` vocabulary and already used by the ACP two-step cancel path. Adding a new column was rejected — the status column already models exactly this lifecycle stage.

## Decision 2 — Observation points on the executing instance

The executing instance reads the authoritative status (`RunIsCancelling`) at:

- **pipeline start** — a queued run cancelled before a worker claimed it stops before doing any work;
- **each step boundary** (the before-model callback) — a long run observes a cancel committed by any instance and cancels its own context, so the existing cancellation block finalizes it;
- **post-loop** — a cancel that landed after the last step boundary (or during a long tool call) is observed before the terminal decision, so this instance reports `cancelled`, not `success`.

Observation is a single indexed primary-key read per step, which is the cost the archived design deferred and #1166 now requires. A read error is treated as "not cancelling": a transient DB problem must not abort a run, and the cancel-aware terminal write (Decision 3) still preserves the endpoint's promise.

The per-process registry (`AgentExecutor.Cancel`) stays as a fast-path: the cancel endpoint still notifies it so a goroutine in the same process unblocks immediately instead of waiting for its next step boundary. It is a notification, never the authority.

## Decision 3 — Race safety: cancel-aware terminal writes

Once the row is `cancelling`, a committed cancel must never become `completed` or `failed`. The completion/skip/failure writers keep the #1165 guard (which includes `cancelling`) but resolve the terminal status *inside the same UPDATE*:

```
SET status = CASE WHEN status = 'cancelling' THEN 'cancelled' ELSE <intended> END
```

Postgres evaluates the right-hand side against the pre-UPDATE row, so the decision is atomic with the write. This closes the check-then-write window between the executor's status observation and its terminal write:

- cancel UPDATE first → row `cancelling`; the completion UPDATE reads `cancelling` and writes `cancelled`;
- completion UPDATE first → row `completed`; the cancel UPDATE's guard excludes `completed`, affects zero rows, and the endpoint reports `cancelled:false` with status `completed`.

Either way exactly one terminal transition is applied and the row's terminal status matches what the endpoint reported. The failure writers likewise use the CASE for `error_message`, recording the user-cancel reason when a cancel wins.

## Decision 4 — Orphan resolution

A committed cancel whose executing instance is gone must still reach `cancelled`, never `failed`, and must not sit in `cancelling` forever:

- **startup recovery** (`registerOrphanRecovery` → `FinalizeCancellingRuns(ctx, 0)`) finalizes every `cancelling` row, because any execution from the previous process is gone;
- **stale-run reaper** finalizes `cancelling` rows idle past `staleRunThreshold`, so a run actively winding down is not clobbered.

## Risks / Trade-offs

- **One extra DB read per step.** Bounded and indexed; only runs carrying no cancel pay a cheap `SELECT`. Rejected the alternative of polling a cancel flag column (same cost) because `cancelling` already exists and already maps to the ACP `TASK_STATE_WORKING` intermediate.
- **`cancelling` is now reachable from more writers** (any instance's cancel endpoint, A2A). `SetRunCancelling` is now guarded to non-terminal rows so it cannot overwrite a finished run.
- **Queued-run cancel.** A `submitted` run moved to `cancelling` is finalized either by the worker at pipeline start or, if never claimed, by startup recovery / the reaper.
- **Per-process registry retained.** It is dead weight for cross-instance correctness but still useful for prompt in-process stops; keeping it avoids a larger refactor and preserves the #1149 detached-run fast path.
