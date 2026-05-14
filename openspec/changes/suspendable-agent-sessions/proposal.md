## Why

Agent runs that hit long-running tools (`ask_user`, spawned sub-agents awaiting human input, external approvals) currently time out or get stuck — the executor has no way to suspend itself mid-tool-call, serialize state, and wake up later with the tool result injected. This makes human-in-the-loop workflows unreliable and spawned-agent chains that hit `ask_user` impossible to complete.

## What Changes

- **New suspend/resume primitive at the session level**: a run can suspend mid-execution with a `SuspendSignal` from any tool, persisting the exact execution context (pending tool call id + args) so it can resume from the exact point.
- **History replay on resume**: on wake, the executor replays full conversation history from `kb.agent_run_messages` into the ADK session, then injects the pending tool result and continues — no "here's what happened" prompt hack.
- **`ask_user` rewritten as a suspending tool**: instead of blocking or signaling an out-of-band pause, it returns a `SuspendSignal{reason: awaiting_human, question_id}` that the executor catches.
- **Spawn cascade**: when a spawned sub-agent suspends, the spawn tool propagates the `SuspendSignal` up so the parent also suspends, storing `{waiting_for_run_id}`. When the child completes, the server automatically wakes the parent and injects the child result.
- **`suspend_context` column on `kb.agent_runs`**: stores the pending tool call id, tool name, and wakeup condition so the executor can reconstruct state on resume.
- **Step count frozen while suspended**: wall-clock pause time does not consume step budget.
- **New `awaiting_child` suspend reason** alongside existing `awaiting_human`.
- **`HandleRespondToQuestion` returns `resume_run_id`** in response so clients can poll the correct run after resume. **BREAKING** for clients that relied on polling the original paused run ID.

## Capabilities

### New Capabilities

- `session-suspend-resume`: Core suspend/resume primitive — `SuspendSignal` type, executor catch logic, `suspend_context` DB column, history replay from `kb.agent_run_messages`, automatic wakeup on condition resolution.

### Modified Capabilities

- `acp-run-lifecycle`: Resume endpoint semantics change — now does true state replay instead of prompt injection. `GET /runs/:runId` on a resumed run returns `resume_run_id` pointing to the active continuation run.

## Impact

- `apps/server/domain/agents/executor.go` — suspend catch in `runPipeline`, history replay in `Resume`
- `apps/server/domain/agents/entity.go` — `suspend_context` jsonb field on `AgentRun`
- `apps/server/domain/agents/dto.go` — `ResumeRunID` on `AgentQuestionDTO`
- `apps/server/domain/agents/coordination_tools.go` — spawn cascade: propagate `SuspendSignal` from child to parent
- `apps/server/domain/agents/handler.go` — `HandleRespondToQuestion` returns `resume_run_id`
- `apps/server/migrations/` — new migration adding `suspend_context` column
- `bench/domain-test/run_domain_test.py` — update to poll `resume_run_id` after respond
