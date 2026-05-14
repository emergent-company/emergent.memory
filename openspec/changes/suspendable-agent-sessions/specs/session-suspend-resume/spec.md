## ADDED Requirements

### Requirement: SuspendSignal primitive
The executor SHALL define a `SuspendSignal` struct with fields `Reason` (string enum: `awaiting_human`, `awaiting_child`), `QuestionID` (string, set for `awaiting_human`), `WaitingForRunID` (string, set for `awaiting_child`), and `PendingToolCallID` (string). Any tool that needs to pause execution SHALL communicate its intent by setting state on a shared signal carrier (`AskPauseState` for `ask_user`; `CoordinationToolDeps.SuspendSignal` for spawn) rather than returning an error.

#### Scenario: ask_user tool signals suspension
- **WHEN** the `ask_user` tool creates a question and calls `RequestPause(questionID)`
- **THEN** `AskPauseState.ShouldPause()` returns `true` and `QuestionID()` returns the created question ID

#### Scenario: executor catches SuspendSignal after tool call
- **WHEN** `afterToolCb` runs and `AskPauseState.ShouldPause()` is `true`
- **THEN** the executor constructs a `SuspendSignal{Reason: "awaiting_human", QuestionID: ..., PendingToolCallID: <function call ID>}` and breaks the pipeline loop

### Requirement: suspend_context persisted on pause
The executor SHALL write the `SuspendSignal` as a JSONB `suspend_context` on the `kb.agent_runs` row before calling `PauseRun`. The schema SHALL be `{"reason": string, "question_id": string|null, "waiting_for_run_id": string|null, "pending_tool_call_id": string|null}`. The column SHALL be nullable; null means the run was not suspended via this mechanism.

#### Scenario: suspend_context written on ask_user pause
- **WHEN** a run pauses due to `ask_user`
- **THEN** the run's `suspend_context` in the DB has `reason: "awaiting_human"`, a non-empty `question_id`, and a non-empty `pending_tool_call_id`

#### Scenario: suspend_context written on child-wait pause
- **WHEN** a run pauses because a spawned child is suspended
- **THEN** the run's `suspend_context` has `reason: "awaiting_child"` and `waiting_for_run_id` set to the child run ID

#### Scenario: suspend_context is null for non-suspended runs
- **WHEN** a run completes or fails normally
- **THEN** `suspend_context` is null on that run row

### Requirement: History replay on Resume
When `Resume` is called, the executor SHALL reconstruct full conversation history by walking the `ResumedFrom` chain, loading all `kb.agent_run_messages` rows for each run in chronological order, and converting them to `[]*genai.Content`. The executor SHALL then append a `genai.Content{Role: "tool"}` part containing the `FunctionResponse` for `pending_tool_call_id` from `suspend_context` before calling `runPipeline`. No synthetic "here is what happened" text prompt SHALL be injected.

#### Scenario: History replay reconstructs model turns
- **WHEN** a prior run has messages with `function_calls` in their JSONB `Content`
- **THEN** replay produces `genai.Content{Role: "model", Parts: [FunctionCallPart{...}]}` entries in the reconstructed history

#### Scenario: History replay reconstructs tool turns
- **WHEN** a prior run has messages with `function_responses` in their JSONB `Content`
- **THEN** replay produces `genai.Content{Role: "tool", Parts: [FunctionResponsePart{...}]}` entries in the reconstructed history

#### Scenario: Pending tool result injected at end of history
- **WHEN** resume is triggered with a human response for question Q and `suspend_context.pending_tool_call_id` is `call-123`
- **THEN** the last entry appended to the history before `runPipeline` is `genai.Content{Role: "tool", Parts: [FunctionResponse{ID: "call-123", Name: "ask_user", Response: {answer: <human response>}}]}`

#### Scenario: Resume with no matching pending_tool_call_id falls back gracefully
- **WHEN** `pending_tool_call_id` in `suspend_context` does not match any stored function call
- **THEN** the executor logs a warning and falls back to text injection; the run does not fail at resume time

### Requirement: Step count frozen while suspended
The executor SHALL NOT increment the step counter during the period a run is in `paused` status. The resumed run SHALL inherit `priorRun.StepCount` as its starting step count so wall-clock pause time does not consume the step budget.

#### Scenario: Resumed run inherits step count
- **WHEN** a run paused at step 5 and is resumed
- **THEN** the new resumed run starts with `initial_step_count = 5` and the step budget is `MaxTotalStepsPerRun - 5`

### Requirement: Spawn cascade suspend propagation
When `executeSingleSpawn` detects that a child run has status `paused`, it SHALL set `CoordinationToolDeps.SuspendSignal` to `SuspendSignal{Reason: "awaiting_child", WaitingForRunID: childRunID}`. The parent executor's `afterToolCb` SHALL detect this signal and pause the parent run with the appropriate `suspend_context`.

#### Scenario: Parent pauses when child pauses
- **WHEN** a parent agent spawns a child agent and that child run enters `paused` status
- **THEN** the parent run also enters `paused` status with `suspend_context.reason = "awaiting_child"` and `suspend_context.waiting_for_run_id = <child run ID>`

#### Scenario: Parent wakes when child completes
- **WHEN** a child run transitions to `completed` and a parent run is paused with `waiting_for_run_id = <child run ID>`
- **THEN** the server enqueues a resume job for the parent run, injecting the child's output as the tool result

### Requirement: Automatic parent wakeup on child completion
The server SHALL check on every run completion whether any parent run is suspended with `suspend_context->>'waiting_for_run_id' = completedRunID`. If found, the server SHALL enqueue a resume for the parent, constructing a `FunctionResponse` for the spawn tool call with the child's output as the result.

#### Scenario: No parent exists for completed run
- **WHEN** a run completes and no parent is waiting for it
- **THEN** no resume is enqueued and no error is returned

#### Scenario: Parent resume enqueued synchronously with child completion
- **WHEN** a child run completes and a parent is found waiting
- **THEN** the parent resume is enqueued before the child run's completion response is returned to the caller
