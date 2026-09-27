## Why

A chat turn's agent run is bound to the `/api/chat/stream` HTTP request context. When the browser↔gateway SSE connection drops for any reason (reload, navigation, network blip), the cancellation propagates gateway → `POST /api/chat/stream` → executor and hard-fails the run with `agent stopped: context canceled`. The user sees a failed turn although nothing was wrong server-side, and the work done up to that point is lost (issue #1149, evidencing run `613e86c3-f32c-426b-b97a-fa0f8ff34eec`).

## What Changes

- Detach a chat agent run's context from the request context: run the executor on a value-preserving, cancellation-free context so a dropped SSE consumer does not abort the run. The run stays bounded by the existing per-step watchdog (`defaultRunTimeout`) and its progress is persisted to the run record as before.
- Route an explicit cancel (`POST /api/chat/runs/:runId/cancel`) by run id through the executor's in-flight run registry, so the stop button still stops a run whose request context no longer reaches it.
- Record an explicit stop honestly: terminal status `cancelled` with the reached step count, not a server `context canceled` fault.
- Make terminal run transitions race-safe: guard every terminal write on a non-terminal status so a cancel and a completion cannot overwrite each other, and report the cancel as applied only when the row actually transitioned.

## Capabilities

### New Capabilities

<!-- None. -->

### Modified Capabilities

- `agent-run-liveness`: a run's lifetime may be detached from the HTTP request that triggered it; explicit cancel is routed by run id and records a cancelled status.

## Impact

- `apps/server/domain/chat/handler.go` — `StreamChat` starts the agent run on a detached context.
- `apps/server/domain/agents/executor.go` — `DetachedRunContext`, per-run cancel registry (`AgentExecutor.Cancel`), and cancelled-status finalization.
- `apps/server/domain/agents/handler.go` — `CancelRun` routes the in-flight cancel through the executor before falling back to the persisted row.
- `apps/server/domain/agents/repository.go` — `CancelRunWithSteps` records a stop as `cancelled`.
- No schema migration: uses the existing `kb.agent_runs` columns.
