## Context

The gateway (`/root/alfred/gateway`) is a thin proxy for agent chat: `POST /api/chat` → `memory.ChatStream` → `rewriteChatStream` transforms the SSE stream post-hoc. The agent loop and tools run in the memory backend (`emergent.memory`, a separate service; source is available read-only under `.slim/clonedeps/repos/emergent-company__emergent.memory/`).

The memory backend already ships a tool-permission engine that this change extends rather than rebuilds:

- `ToolPolicy{Confirm, Message, Disabled}` and `AgentDefinition.ToolPolicies map[string]ToolPolicy` (`agents/entity.go`). Absent = allow, `Confirm:true` = ask, `Disabled:true` = deny.
- Confirm gate in `executor.go` (`beforeToolCb`): an intercepted call creates an approve/reject question via `CreateAndEmitQuestion`, stores a `ToolConfirmPauseState`, and pauses the run; `HandleRespondToQuestion` → `injectToolResponse` resumes it ("approve" → execute + real result, otherwise → injected result).

Known gaps this change closes: reject is injected as a raw *error*; the approval question is emitted out-of-band (events service), never into the chat stream; no per-agent default policy; single-slot confirm (no batch); no approval audit; no revocation.

## Goals / Non-Goals

**Goals:**
- Extend the existing `ToolPolicy` engine; add a per-agent default policy.
- Structured, non-error rejection result carrying an optional human message.
- Approval prompt surfaced in-conversation (chat/side panel), not only out-of-band.
- Durable audit trail with redaction, retention, and a gateway view.
- Pending-approval revocation.
- Multiple concurrent approvals (phased — see Decisions).

**Non-Goals:**
- No parallel approval endpoint — reuse the existing question-respond route.
- No auto-timeout on pending approvals (never auto-deny).
- No "ask-once-per-pattern" grant model (one approval authorizes one call).
- No cross-run or cross-agent approval sharing.

## Decisions

1. **Extend `ToolPolicy`, don't build a parallel engine.** Map `allow` = absent, `deny` = `Disabled`, `ask` = `Confirm`, and add a `defaultPolicy` field (or a `Mode` enum) so absent entries no longer implicitly mean allow. *Alternative rejected:* a new gateway-side approval framework — it would duplicate enforcement and leave the real gate in memory anyway.

2. **Structured rejection result, not an error.** Reject injects `{policy_decision:"rejected", reason:<message>}` as the tool result. *Alternative rejected:* current error injection — LLMs read errors as "retry", not "stop this direction".

3. **Two sentinels, not three.** `timeout` (execution failure, retryable) vs `rejected` (human decision, optional `reason`). The "rejected vs rejected+message" split is dropped — it isn't reliably actionable for the model; `reason` is just an optional field on `rejected`.

4. **In-stream approval event, reusing the question transport.** Add an approval event to the executor's chat stream output (a `kind` discriminator on the existing question event, or a sibling `StreamEventApprovalRequest`), map it in the chat `streamCallback`, and pass it through the gateway's `rewriteChatStream`. The client renders an approval card distinct from question cards. *Alternative rejected:* a separate `approval_request` SSE channel + `POST /approvals/:id` endpoint — it duplicates the existing question SSE + respond plumbing.

5. **Approve/deny via the existing respond route.** Extend `respondQuestion`'s payload to carry `{approve, message}` (or reuse `{response:"approve"}`), mapping to memory's `HandleRespondToQuestion`. No new route surface in the gateway.

6. **Approvals wait forever.** No auto-timeout; never auto-deny (auto-deny fabricates a "rejected" decision that steers the agent wrongly). This is already the memory behavior — the stale reaper skips paused/input-required runs. Keep it, and add an explicit cancel path instead of a timeout.

7. **Multiple approvals: phased.** Phase 1 keeps the current single-approval-per-turn model (one `SuspendContext` / `ToolConfirmPauseState`). Phase 2 (batch-per-turn) requires the runtime to support parallel tool dispatch in one assistant message and a per-slot approvals table replacing the single suspend slot; resume is all-or-nothing (execute approved, inject rejects in slot order). Deferred until the parallel-dispatch prerequisite is confirmed.

8. **Audit tables (memory side) + gateway view.** New `approvals` / `approval_decisions` tables recording `(run_id, project_id, agent, tool, args_summary, decision, message, user, timestamp)`, with argument redaction (store summary, never raw values), retention, and project-scoped RBAC. The gateway renders an audit view from this API.

9. **Revocation.** Give a pending approval a `cancelled` state; cancelling resolves the pause with the call treated as not taken. A cancelled generation propagates to the paused run.

10. **Policy snapshot + authz.** Policy is read from the agent definition snapshot at run start; mid-run policy edits apply to subsequent tool calls only. The gateway enforces project-scope + role on the approve and audit endpoints (it already has auth via `auth.go`/`token.go`).

## Risks / Trade-offs

- **Batch-per-turn prerequisite unverified.** Current confirm is single-slot and `CancelPendingQuestionsForRun` cancels prior pending questions, so "multiple approvals" can't ride the existing question table. → Defer to Phase 2; do not ship a half-batch.
- **Unbounded paused runs / pending approvals.** "Wait forever" means paused runs and their approvals accumulate. → Cap outstanding approvals per project; surface a "pending approvals" list; rely on revocation, not timeout.
- **Default-allow auto-executes new tools.** Today any tool granted membership (or newly added) runs silently. → `defaultPolicy` with a documented, explicit default (default-allow preserved for existing agents; new assistant agent can default-deny).
- **Reject-as-error habit.** The model has been trained on the current error injection; switching to a structured result needs a system-prompt instruction ("a `rejected` result means stop this direction, do not retry"). → Coordinate the prompt change with the result-shape change.
- **Secrets in audit.** Tool args can hold tokens/keys. → Store redacted summaries; never persist raw args.
- **Cross-repo dependency.** Memory-side changes must land before gateway passthrough is meaningful. → Tasks are split into memory-side (external) and gateway-side (this repo); gateway tasks gate on memory-side availability.

## Phase 2: Batch approvals — per-slot design

### Current single-slot

- `ToolConfirmPauseState` (`ask_user_tool.go:47`) holds ONE confirmation via atomic single values (`requested`, `questionID`, `toolName`, `toolArgs`).
- `beforeToolCb` (`executor.go:1686`) → `RequestConfirm(...)` (single slot), returns `{status:"awaiting_confirmation"}`.
- `afterToolCb` (`executor.go:1887`) → builds ONE `SuspendSignal{AwaitingToolConfirm, QuestionID, PendingToolCallID, PendingToolName, PendingToolConfirmArgs}` → `UpdateSuspendContext` + `PauseRun`.
- `beforeModelCb` (`executor.go:1664`) has a backup pause check.

### Problem

ADK runner (`adk v1.4.0`, `internal/llminternal/base_flow.go:1012 handleFunctionCalls`) dispatches N function calls from one LLM turn in PARALLEL goroutines (`sync.WaitGroup` + `go func` per fnCall, lines 1027–1031). For N `Confirm` tools: N concurrent `RequestConfirm` → last-writer-wins, N−1 lost; N concurrent `afterToolCb` → N concurrent `PauseRun`/`UpdateSuspendContext` → corrupt `suspend_context`.

### Target design

1. **Concurrent-safe batch accumulator** — replace single values with a mutex-protected `[]ToolConfirmation{QuestionID, FunctionCallID, ToolName, ToolArgs}`. `beforeToolCb` appends under mutex.
2. **Batch SuspendSignal** — `SuspendSignal` gains `PendingToolConfirmations []ToolConfirmation` (serialized in `suspend_context` JSONB). Single fields retained for `ask_user`/client-tool (backward compat).
3. **Pause at `beforeModelCb`** — remove the per-tool `afterToolCb` confirm pause; `beforeModelCb` fires after all tools in the turn complete (ADK WaitGroup done), the correct single sync point to build the batch `SuspendSignal` and pause once.
4. **All-or-nothing resume** — `agent_tool_approvals` is the decision source of truth (one record per confirmation). `HandleRespondToQuestion`/`HandleCancelQuestion` update the record and resume ONLY when every confirmation in the batch is non-pending. On resume, `injectToolResponse` emits N FunctionResponses (execute approved, inject `{policy_decision:"rejected"|"cancelled"}` in slot order) keyed by each function_call_id.

### Review resolutions (oracle, verified against source)

- **Sync point:** `beforeModelCb` fires after step N's `wg.Wait()` (`base_flow.go:1174`) → batch complete there. Move `UpdateSuspendContext` into it.
- **Concurrency:** `beforeToolCb` runs inside each parallel goroutine; mutex slice append is correct.
- **Resume coordination:** NOT table polling (no batch key, non-atomic re-query). Use `suspend_context` JSONB as batch source of truth + `BEGIN; SELECT … FOR UPDATE` on the run row for exactly-once resume. `agent_tool_approvals` stays audit-only.
- **N-part injection:** one event, N `FunctionResponse` parts (matches ADK `mergeParallelFunctionResponseEvents`). Must fix `injectToolResponse` `Author`/`Role` (currently tool-name/tool → ADK textualizes as foreign).
- **Backward compat:** slice defaults empty; no migration; keep single fields for in-flight runs.
- **Critical race:** `CreateAndEmitQuestion` cancels sibling pending questions (`ask_user_tool.go:131`) → N parallel confirms cancel each other. Make question creation batch-aware.
- **Implementation order:** SuspendSignal batch → mutex accumulator → question-batch fix → pause-in-beforeModelCb → N-part injection → FOR UPDATE resume → tests.
