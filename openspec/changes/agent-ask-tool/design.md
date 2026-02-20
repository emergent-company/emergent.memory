## Context

Emergent agents execute via `AgentExecutor` (`domain/agents/executor.go`) which builds an ADK pipeline with tools from `ToolPool`, runs it via the ADK `runner.Run()` loop, and persists all messages and tool calls in real-time to `kb.agent_run_messages` and `kb.agent_run_tool_calls`.

The executor already supports **pause/resume**:

- Pausing: The `beforeModelCb` calls `repo.PauseRun()` when step limits are exceeded (executor.go:438), setting `agent_runs.status = 'paused'`. The runner loop exits naturally after the callback returns a synthetic response.
- Resuming: `Resume()` (executor.go:172) creates a new run record linked via `resumed_from`, carries forward the cumulative `step_count`, and runs a fresh ADK pipeline. The resumed run gets the same tools and system prompt but starts with a fresh in-memory ADK session.

The coordination tools (`spawn_agents`) already support `resume_run_id` for resuming paused sub-agents.

The notification system (`domain/notifications/`) has a rich entity with `actions` (JSONB array), `actionStatus`, `sourceType/sourceID`, `relatedResourceType/relatedResourceID`, and `actionURL` -- sufficient for structured question/response UI.

Sessions are in-memory (`session.InMemoryService()`) and not persisted across resumes. Each resumed run starts with only the `UserMessage` from the `ExecuteRequest` -- there is no replay of prior conversation history.

## Goals / Non-Goals

**Goals:**

- Allow agents to pause execution and ask users a question with optional structured choices
- Persist questions and responses in a dedicated table for auditability and UI rendering
- Surface questions to users via the existing notification system
- Auto-resume the paused agent when a user responds, injecting the answer as context
- Make the feature opt-in: agents only get the `ask_user` tool if configured in their definition

**Non-Goals:**

- Real-time bidirectional agent-user chat (this is a single question/response per pause, not a conversation)
- Multi-user collaborative responses (one user answers per question)
- Replaying full prior conversation history into the resumed run (current resume behavior starts fresh)
- Timeout/expiry behavior for unanswered questions (can be added later)
- WebSocket push notifications for questions (polling via existing notification system is sufficient for v1)

## Decisions

### Decision 1: Pause mechanism -- context cancellation via `afterToolCb`

When the `ask_user` tool executes, the run must stop. Two approaches were considered:

**Option A (chosen): afterToolCb sets a flag, beforeModelCb checks it and pauses.**
The `ask_user` tool sets `ae.askPauseRequested` (an atomic bool on a per-run context struct). The next `beforeModelCb` invocation checks this flag, calls `repo.PauseRun()`, and returns a synthetic LLM response -- identical to the existing step-limit pause. The runner exits naturally.

**Option B (rejected): ask_user cancels the context.**
This would mark the run as `error` in the context-cancellation handler (executor.go:580-607), losing the semantic distinction between "paused waiting for input" and "failed/timed out."

Rationale: Option A reuses the proven step-limit pause pathway with zero changes to the runner or event loop. The only new code is a flag check in `beforeModelCb` and a setter in the tool.

### Decision 2: Question persistence -- new `kb.agent_questions` table

A dedicated table rather than embedding questions in `agent_run_messages` because:

- Questions have structured metadata (options array, response, status, responder) that doesn't fit the generic `content JSONB` of messages
- The response API needs to query pending questions directly (by run_id, by user, by status)
- Notifications reference `relatedResourceType = 'agent_question'` and `relatedResourceID` for deep linking

Schema:

```sql
CREATE TABLE kb.agent_questions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id UUID NOT NULL REFERENCES kb.agent_runs(id),
    agent_id UUID NOT NULL REFERENCES kb.agents(id),
    project_id UUID NOT NULL,
    question TEXT NOT NULL,
    options JSONB DEFAULT '[]',    -- [{label, value, description?}]
    response TEXT,
    responded_by UUID,             -- user ID
    responded_at TIMESTAMPTZ,
    status TEXT NOT NULL DEFAULT 'pending', -- pending, answered, expired, cancelled
    notification_id UUID,          -- link to the notification created
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

### Decision 3: Answer injection -- prepend to `UserMessage` on resume

When a user answers a question, the response handler:

1. Updates `kb.agent_questions` with the response
2. Loads the paused run
3. Calls `executor.Resume()` with an `ExecuteRequest` where `UserMessage` is constructed as:

```
Previously you asked: "<question>"
The user responded: "<answer>"

Continue from where you left off.
```

This approach was chosen over alternatives:

- **Replay full message history (rejected)**: Would require switching from `session.InMemoryService()` to a persistent session store and reconstructing ADK-compatible message formats from `agent_run_messages`. Significant complexity for v1.
- **Inject as system prompt (rejected)**: The answer is user input, not a system directive. Placing it in the user message is semantically correct for the LLM.

### Decision 4: Notification integration -- reuse existing actions JSONB

Questions with options map to the notification's `actions` field:

```json
{
  "title": "Agent question from <agent_name>",
  "message": "<question text>",
  "type": "agent_question",
  "sourceType": "agent_run",
  "sourceID": "<run_id>",
  "relatedResourceType": "agent_question",
  "relatedResourceID": "<question_id>",
  "importance": "important",
  "actions": [
    { "label": "Mercury (planet)", "value": "planet" },
    { "label": "Mercury (element)", "value": "element" }
  ]
}
```

For open-ended questions (no options), the notification links to a response page via `actionURL`.

### Decision 5: Response API -- new endpoint on the agents handler

`POST /api/projects/:projectId/agent-questions/:questionId/respond`

```json
{
  "response": "planet"
}
```

This endpoint:

1. Validates the question belongs to the project and is `status: pending`
2. Updates the question with the response and responder
3. Marks the notification as read and sets `actionStatus: 'completed'`
4. Loads the agent + definition + paused run
5. Calls `executor.Resume()` in a goroutine (non-blocking response to user)
6. Returns `202 Accepted` with the new run ID

The response handler lives in the agents domain (not notifications) because it triggers execution.

### Decision 6: Tool registration -- opt-in via agent definition tools list

The `ask_user` tool is registered like coordination tools: the executor checks if the agent definition's `tools` array includes `"ask_user"`. If so, it builds and injects the tool. This follows the existing pattern for `spawn_agents` and `list_available_agents`.

## Risks / Trade-offs

**[Risk] Agent resumes with no memory of prior work** → The current resume creates a fresh ADK session, so the agent only sees the injected user message with the Q&A context. It doesn't remember what it was doing before pausing. Mitigation: The `UserMessage` includes the original task description plus the Q&A exchange. For v1 this is acceptable; v2 could persist and replay the ADK session.

**[Risk] Race condition: user responds while run is still pausing** → The `ask_user` tool returns, but the runner hasn't fully exited yet when the user's response arrives. Mitigation: The response endpoint checks `agent_runs.status = 'paused'` before resuming. If still `running`, return `409 Conflict` and let the frontend retry.

**[Risk] Multiple questions per run** → An agent could call `ask_user` multiple times before pausing (unlikely since the pause happens on the next model call, but possible with fast tool chains). Mitigation: Only the last pending question for a run is the active one. Earlier questions are auto-cancelled when a new one is created for the same run.

**[Risk] Notification system has no Create method exposed to other domains** → The notification `Service` only has read/update methods. Mitigation: Add a `Create(ctx, *Notification) error` method to the notification service and repository, or have the agents domain insert directly via its own repository (simpler, follows the pattern of other domains that write to their own tables).

**[Trade-off] No WebSocket push for v1** → Users won't see questions appear in real-time; they'll see them on the next notification poll. This is acceptable for background agents where response latency of seconds is fine.
