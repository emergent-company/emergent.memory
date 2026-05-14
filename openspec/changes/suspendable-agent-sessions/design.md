## Context

Agent runs that invoke `ask_user` or spawn child agents that hit `ask_user` currently resume via a prompt-injection hack: the executor creates a *new* `AgentRun` row on each resume, wires it to the prior via `ResumedFrom`, and calls `runPipeline` with no session continuity — the ADK session is cold, history is absent, and the tool result is passed as a synthesized text turn. This produces hallucination risk, inflates token cost, and prevents spawn cascades (a child pausing causes the parent to see a `"status: paused"` text string from the spawn tool and make a wrong decision).

Three bugs today block reliable HITL:
1. **Double-resume**: `HandleRespondToQuestion` already launches `Resume` in a goroutine; callers that also `POST /acp/.../resume` after responding cause a race.
2. **Wrong run ID to poll**: `Resume` creates a *new* run; bench polled the original paused run ID and never saw `completed`.
3. **Spawn cascade gap**: `CoordinationToolDeps` has no `PauseState`; child pause is invisible to parent.

The underlying issue is that pause/resume is wired at the HTTP/tool boundary rather than as a first-class session primitive.

## Goals / Non-Goals

**Goals:**
- `SuspendSignal` type propagated from any tool → caught by `runPipeline` → suspends run and serializes context to `suspend_context` JSONB column
- History replay on `Resume`: walk `ResumedFrom` chain, rebuild `[]*genai.Content` from `kb.agent_run_messages`, inject pending tool result, continue — no synthetic prompt
- `ask_user` rewritten to return `SuspendSignal` (tool still creates the question + notification; suspend happens in executor, not tool)
- Spawn cascade: `executeSingleSpawn` detects child `SuspendSignal`, propagates up; parent suspends with `{waiting_for_run_id: childRunID}`; child completion wakes parent
- `HandleRespondToQuestion` returns `resume_run_id` in response DTO so clients poll the correct run
- Step count frozen while suspended (wall-clock pause time not charged)
- Fix bench double-resume: remove second `POST .../resume` call from `run_domain_test.py`

**Non-Goals:**
- Checkpointing arbitrary mid-LLM-inference state (we replay, we do not snapshot the model)
- Multi-tenant or distributed suspend/resume across separate server instances
- Changing ACP run model IDs or external run-lifecycle semantics beyond `resume_run_id` addition

## Decisions

### 1. SuspendSignal as a typed value, not an error

**Decision:** Tools return `(map[string]any, error)` today. `SuspendSignal` will be a separate sentinel checked *after* the tool call returns successfully. The tool sets `PauseState.RequestPause(...)` (existing pattern for `ask_user`) **or** for spawn cascade the spawn tool stores a `SuspendSignal` struct on `CoordinationToolDeps` and returns a normal map result. `runPipeline`'s `afterToolCb` inspects the signal and breaks the loop.

**Why not error?** An error aborts the run; we want the tool result persisted and the run to enter `paused` cleanly.

**Alternative considered:** Return a sentinel string like `"__SUSPEND__"` in the result map. Rejected: fragile, leaks into LLM context if not stripped.

### 2. suspend_context JSONB column on kb.agent_runs

**Decision:** Add `suspend_context jsonb` to `kb.agent_runs`. Schema:
```json
{
  "reason": "awaiting_human" | "awaiting_child",
  "question_id": "...",       // awaiting_human
  "pending_tool_call_id": "...", // function_call ID to match on resume
  "pending_tool_name": "...",
  "waiting_for_run_id": "..."   // awaiting_child
}
```

**Why JSONB?** Suspend reasons will grow; schema-free column avoids migrations per new reason. Existing `AgentRun` entity already uses JSONB for `TriggerMetadata`.

**Alternative considered:** Separate `agent_run_suspend_contexts` table. Rejected: overkill for a single row per paused run.

### 3. History replay from kb.agent_run_messages

**Decision:** On `Resume`, walk the `ResumedFrom` chain to collect all prior run IDs, call `FindMessagesByRunID` for each, reconstruct `[]*genai.Content` in chronological order. Inject the pending tool result as a `FunctionResponse` part matching `pending_tool_call_id`, then call `runPipeline` with the reconstructed session.

**Content reconstruction rules:**
- Message with `text` key → `genai.Content{Role: "model", Parts: [TextPart]}`
- Message with `function_calls` key → `genai.Content{Role: "model", Parts: [FunctionCallPart, ...]}`
- Message with `function_responses` key → `genai.Content{Role: "tool", Parts: [FunctionResponsePart, ...]}`

Pair `function_calls` and `function_responses` by the `id` field stored in each JSONB entry.

**Why not re-send the original `Input` as context?** LLM re-invents decisions already made; replay gives exact fidelity.

**Alternative considered:** Store full `genai.Content` blobs in a new column. Rejected: `kb.agent_run_messages` already has the data; duplicating it adds storage and divergence risk.

### 4. ask_user tool sets PauseState only; executor owns suspend

**Decision:** `ask_user` tool continues to call `deps.PauseState.RequestPause(questionID)` and returns `{question_id, status: "pausing"}`. The executor's `afterToolCb` detects `ShouldPause()`, writes `suspend_context` (reason=`awaiting_human`, `question_id`, `pending_tool_call_id`), calls `PauseRun`, and breaks the loop. No structural change to the tool's return type.

**Why:** Minimal blast radius — `AskPauseState` pattern already exists and works. Suspend becomes the executor's responsibility, not the tool's.

### 5. Spawn cascade via CoordinationToolDeps.SuspendSignal

**Decision:** Add `SuspendSignal *SuspendSignal` field to `CoordinationToolDeps`. `executeSingleSpawn` checks the child result's run status; if `paused`, populates `CoordinationToolDeps.SuspendSignal` with `{reason: awaiting_child, waiting_for_run_id: childRunID}`. The parent executor's `afterToolCb` detects this, writes `suspend_context`, and pauses.

**Wakeup trigger:** When a child run transitions to `completed`, the repository/service layer checks `kb.agent_runs` for any parent paused with `suspend_context->>'waiting_for_run_id' = childRunID` and enqueues a resume job.

**Alternative considered:** Polling loop in parent. Rejected: wastes step budget, fails under long child runs.

### 6. resume_run_id returned from HandleRespondToQuestion

**Decision:** `HandleRespondToQuestion` launches `Resume` in a background goroutine (current behaviour). Before launching, it creates the new run record synchronously (extract `CreateRunWithOptions` from `Resume`) and returns `resume_run_id` in the `AgentQuestionDTO` response. This is a **breaking change** — clients must switch to polling `resume_run_id`.

**Why not return run ID from the goroutine?** HTTP response must return before goroutine completes; run ID must be known at response time.

**Migration note:** Old clients that poll the original paused run ID will see it transition to `running` (existing `MarkRunResumed` behaviour) but never `completed`. Document the break in the ACP run lifecycle spec.

## Risks / Trade-offs

| Risk | Mitigation |
|------|-----------|
| History replay message ordering wrong → LLM confused | Order by `created_at` + run chain order; add integration test replaying a 3-turn HITL scenario |
| `pending_tool_call_id` mismatch → injected FunctionResponse orphaned | Log warning + fall back to text injection if ID not found; surface in run summary |
| Child wakeup trigger fires before parent `suspend_context` written (race) | Write `suspend_context` synchronously in `afterToolCb` before returning; wakeup query checks `status = paused` |
| Double-resume race (existing bug) | Remove second `POST .../resume` from bench; document that `HandleRespondToQuestion` is the only resume path for HITL |
| Spawn cascade depth > 1 (grandchild pauses) | Each level propagates independently; each paused ancestor stores `waiting_for_run_id`; wakeup is recursive |
| Large message histories (1000+ messages) slow replay | `FindMessagesByRunID` query adds `LIMIT`; cap replay at last N turns configurable via env; log warning when truncated |

## Migration Plan

1. **Migration**: add `suspend_context jsonb` column to `kb.agent_runs` (nullable, no default).
2. **Deploy**: column is additive; old code ignores it; zero downtime.
3. **Code**: implement `SuspendSignal`, update `afterToolCb`, rewrite `Resume` replay, add spawn cascade, update `HandleRespondToQuestion` to return `resume_run_id`.
4. **Bench fix**: remove double-resume call from `run_domain_test.py`; update polling to use `resume_run_id`.
5. **Rollback**: column remains null for old runs; old resume path (prompt injection) can be feature-flagged via env var `SUSPEND_RESUME_ENABLED=true` if needed.

## Open Questions

- **ADK session initialisation with pre-built history**: does the ADK `Runner` accept a pre-populated `[]*genai.Content` slice at session start, or must we construct a custom `SessionService`? Need to verify against `google.golang.org/adk` internals before implementation.
- **Child wakeup trigger location**: should the "wake parent on child complete" logic live in `executor.go` (inline after `runPipeline` returns) or in `repository.go` as a DB trigger / job enqueue? Inline is simpler but couples executor to parent lookup.
- **Step budget on resume**: should the resumed run inherit `priorRun.StepCount` (current behaviour) or reset? Current behaviour is correct per proposal but needs explicit test.
