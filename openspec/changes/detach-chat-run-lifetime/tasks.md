## 1. Detach the chat run lifetime

- [x] 1.1 Add `agents.DetachedRunContext(parent)` (value-preserving, cancellation-free).
- [x] 1.2 In `chat.StreamChat`, start the agent-backed run on `DetachedRunContext(ctx)` (direct-LLM path unchanged).

## 2. Explicit cancel routed by run id

- [x] 2.1 Add a per-run cancel registry on `AgentExecutor`, registered in `runPipeline` and unregistered on every exit.
- [x] 2.2 Add `AgentExecutor.Cancel(runID, reason) bool`; route `agents.Handler.CancelRun` through it before falling back to the persisted row.
- [x] 2.3 Record an explicit stop as `cancelled` with the reached step count (`CancelRunWithSteps`), not `failed`, and use the recorded reason in the before-model and post-loop cancellation paths.

## 3. Tests

- [x] 3.1 Unit: `DetachedRunContext` survives parent cancellation and preserves values.
- [x] 3.2 Unit: cancel registry cancels a registered run, records the reason, is keyed safely across reuse.
- [x] 3.3 DB: a detached run completes (`success`) when the request context is cancelled mid-run.
- [x] 3.4 DB: a request-bound run still fails on request cancellation (RED companion documenting the old binding).
- [x] 3.5 DB: an explicit cancel stops a detached run and is recorded `cancelled` with the user-cancel reason.

## 4. Verify

- [x] 4.1 `go build ./...` and `go vet` in `apps/server`.
- [x] 4.2 Targeted DB-backed `go test` for `domain/agents` and `domain/chat` (`REQUIRE_DB=1`).
- [x] 4.3 Fail-first RED→GREEN captured for detach-vs-abort and for explicit cancel.
- [x] 4.4 `gofmt -l` clean; repo lint ratchet green.
