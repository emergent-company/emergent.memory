## Why

The archived `gate-kill-switch-all-agent-entry-points` change (PR #1647) closed
the kill-switch gaps on the HTTP trigger and A2A send/resume paths, but its
requirement "Callers SHALL NOT leave a pre-created run stuck `running` when
execution is disabled" enumerates only "the HTTP trigger endpoint, the A2A
send/resume paths". The keyed agent-share path was missed.

`ShareService.StreamMessage` (`apps/server/domain/agents/share_service.go`)
pre-creates a share run with status `running` via `CreateShareRunIfUnderLimit`,
then calls `runner.Execute`. With `AGENT_EXECUTION_ENABLED=false` the executor
returns `ErrExecutionDisabled`, and the function returned the error **without
failing the run** — so the share run stayed `running` until the 30-minute
`StaleRunReaper`, which then reported the misleading "run exceeded idle timeout
(likely abandoned by client)".

## What Changes

- **Refuse before creating the share run.** `ShareService.StreamMessage` probes
  the kill switch (via a new `shareRunner` interface exposing
  `EnsureExecutionEnabled`) before budget reservation and share-run creation,
  returning a clean `503 agent_execution_disabled` apperror — mirroring
  `Handler.TriggerAgent`.
- **Reconcile a mid-flight flip.** `ShareRepo` gains `FailRunWithSteps`, and
  `failRunIfExecutionDisabled` is generalised from `*Repository` to a small
  `runFailer` interface so the share path reuses it to mark a pre-created share
  run terminal when `Execute` returns `ErrExecutionDisabled` after the
  pre-check.
- **Tests.** A disabled kill switch creates no share run; a disabled race marks
  the pre-created share run terminal with the disabled reason.

## Capabilities

### Modified Capabilities

- `agent-execution`: the "Callers SHALL NOT leave a pre-created run stuck
  `running` when execution is disabled" requirement now covers the agent-share
  `StreamMessage` path — refuse before creating the share run, and mark a
  pre-created share run failed when execution is disabled.
