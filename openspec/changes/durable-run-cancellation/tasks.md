## 1. Durable cancel primitive

- [x] 1.1 Add `Repository.RequestRunCancellation` — a guarded UPDATE moving a non-terminal run to `cancelling`, returning whether a row was transitioned (idempotent for an already-`cancelling` row).
- [x] 1.2 Add `Repository.RunIsCancelling` — the observation read used at step boundaries; a missing run reports false.
- [x] 1.3 Add `Repository.FinalizeCancellingRuns(notBefore)` — guarded `cancelling` → `cancelled` for orphan/stale resolution.
- [x] 1.4 Guard `SetRunCancelling` to non-terminal rows.

## 2. Cancel-aware terminal writes

- [x] 2.1 `setTerminalStatus` + `durableCancelReasonSet` helpers: terminal writers resolve `status = CASE WHEN status = 'cancelling' THEN 'cancelled' ELSE <intended> END`.
- [x] 2.2 Apply to `CompleteRun`, `CompleteRunWithSteps`, `SkipRun`, `FailRun`, `FailRunWithSteps`; failure writers record the user-cancel reason when a cancel wins.

## 3. Executor observation

- [x] 3.1 Add `AgentExecutor.durablyCancelling` (nil-safe).
- [x] 3.2 Observe at pipeline start (queued runs), each step boundary (before-model callback), and post-loop before the terminal decision.
- [x] 3.3 Keep the per-process registry as a fast-path notification only.

## 4. Endpoint

- [x] 4.1 `Handler.CancelRun` commits the durable `cancelling` intent via `RequestRunCancellation`, notifies the local registry best-effort, and reports `cancelled:true`/`cancelled:false` plus the current status based on the guarded transition.

## 5. Orphan resolution

- [x] 5.1 Startup `registerOrphanRecovery` finalizes every `cancelling` row to `cancelled`.
- [x] 5.2 Stale-run reaper finalizes idle `cancelling` rows to `cancelled`.

## 6. Tests

- [x] 6.1 DB: two-instance (separate repositories) completion after a cancel on the other instance finalizes `cancelled`, not `completed`.
- [x] 6.2 DB: failure after a cancel finalizes `cancelled`.
- [x] 6.3 DB: cancel of an already-terminal run changes nothing (no clobber of a success).
- [x] 6.4 DB: repeated cancel is idempotent; `RunIsCancelling` observation; missing-run observation.
- [x] 6.5 DB: orphan/stale `cancelling` runs finalize to `cancelled`; fresh ones are spared by the threshold.
- [x] 6.6 DB executor seam: a run executing on instance B, with no local notification, stops after instance A commits a durable cancel (final status `cancelled`).
- [x] 6.7 Handler DB: running run reports `cancelling` intent; a later executor completion keeps `cancelled`; already-completed stays `success` with `cancelled:false`.
- [x] 6.8 Existing #1165 terminal-transition tests still pass.
- [x] 6.9 Fail-first RED→GREEN captured for observation disabled and for cancel-aware CASE disabled.

## 7. Verify

- [x] 7.1 `go build ./...` and `go vet` in `apps/server`.
- [x] 7.2 `task lint` green (0 issues).
- [x] 7.3 DB-backed `go test` for `domain/agents`, `domain/chat`, `domain/scheduler` with a hermetic throwaway Postgres (`REQUIRE_DB=1`).
- [x] 7.4 `gofmt -l` clean; `openspec validate --all --strict` green.
