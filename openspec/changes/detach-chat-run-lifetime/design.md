## Context

A chat turn's agent run is bound to the `/api/chat/stream` request context: `chat.StreamChat` takes `c := c.Request().Context()` and passes it to `streamAgentChat` → `agentExecutor.Execute(ctx, req)`. When the browser↔gateway SSE connection drops, the gateway's `chat()` handler returns on the failed write, `body.Close()` cancels the upstream `POST /api/chat/stream` request, and the server request context is cancelled — the executor's `beforeModelCb` observes `ctx.Err() != nil`, aborts the run with `agent stopped: context canceled`, and `kb.agent_runs` is stamped `failed`. The stop button was not used; no server limit was hit (step 5/30, per-step watchdog clear).

The stop button (`webui/static/js/chat.js` `stopTurn`) does two things: `aborter.abort()` (cancels the fetch → the same request-context cancellation) and `POST /api/chat/runs/:runId/cancel`. The cancel endpoint (`agents.Handler.CancelRun`) only writes `kb.agent_runs.status = cancelled`; nothing in the executor observed that write, so the actual stop relied entirely on the client abort. Decoupling the run from the request context therefore requires a stop signal that does not depend on the request context.

## Goals / Non-Goals

**Goals:**

- A transient SSE drop must not fail the run; the run continues to completion (or the watchdog) and its progress is persisted.
- The run stays bounded — the per-step watchdog and `defaultRunTimeout` are unchanged.
- An explicit cancel must still stop the run, and must be recorded as `cancelled`, not a server fault.
- Do not regress the `#1134` `TransportEnforced`/`runDispatchContext` boundary or the `#1072` per-step watchdog.

**Non-Goals:**

- No client reconnect/re-attach path for an in-flight stream (a separate client-side capability; see Risks).
- No per-step database polling for a cancel flag (multi-instance cancel is a residual limitation; see Risks).
- No change to the direct-LLM (non-agent) chat path.

## Decisions

### D1 — Detach at the chat HTTP boundary, not globally

`DetachedRunContext` wraps `context.WithoutCancel`, preserving values (auth, project, trace span) and dropping only cancellation. It is applied in `chat.StreamChat` only. Global detaching was rejected: the worker pool stops in-flight runs on shutdown via its own cancellable context, and detaching there would make `WorkerPool.Stop()` hang up to the watchdog budget. Other non-detached surfaces keep their current request-bound lifetime.

### D2 — Explicit cancel routed by run id through an in-flight registry

Because the request context no longer reaches a detached run, `AgentExecutor` keeps a `runCancelRegistry` keyed by run id. `runPipeline` registers its cancellable context after the watchdog is armed and unregisters on every exit path. `AgentExecutor.Cancel(runID, reason)` invokes the handle; `agents.Handler.CancelRun` calls it first and falls back to `repo.CancelRun` when the run is not in flight (already terminal, or in another process).

### D3 — Honest terminal status

A registry-initiated cancel records the first reason (`userCancelReason`) on the handle. The before-model callback and the post-loop cancellation block read it: an explicit stop finalizes as `RunStatusCancelled` via `CancelRunWithSteps` (status `cancelled`, reason in `error_message`, reached step count), while watchdog/doom-loop cancellations remain `RunStatusError`. The event-error path now breaks to the post-loop block when `ctx.Err() != nil` instead of stamping a generic pipeline error, so the terminal status always reflects why the run stopped.

### D4 — Bounded and discoverable

The detached lifetime is still bounded by `newStepWatchdog` (request timeout > agent `default_timeout` > `defaultRunTimeout`). The run row and its messages are persisted exactly as before and remain visible/resumable through the existing run-history surfaces; `StreamChat` logs that the run was detached.

## Risks / Trade-offs

- [Multi-instance cancel] The in-flight registry is per process. If a cancel request is served by a different instance than the one running the chat, `Cancel` returns false and the run continues (the DB status is marked cancelled but the executor does not observe it). Single-node deployments (dev, the reported environment) are unaffected; a durable cancel flag observed at step boundaries is the follow-up if horizontal scaling is needed. → File an issue; out of scope here.
- [Client UX] The browser still finalizes the aborted turn locally; the completion is visible after a transcript refresh. A client re-attach path is deliberately out of scope.
- [Handler goroutine] A detached run keeps the HTTP handler goroutine alive until the run finishes; it is bounded by the watchdog and holds no client socket.
