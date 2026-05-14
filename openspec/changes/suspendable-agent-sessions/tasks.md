## 1. Database Migration

- [ ] 1.1 Create migration: add `suspend_context jsonb` column to `kb.agent_runs` (nullable, no default)
- [ ] 1.2 Add `SuspendContext` field (`map[string]any` JSONB) to `AgentRun` entity in `entity.go`
- [ ] 1.3 Add `UpdateSuspendContext(ctx, runID string, ctx map[string]any) error` to `Repository`

## 2. SuspendSignal Type

- [ ] 2.1 Define `SuspendSignal` struct in `agents` package: `Reason`, `QuestionID`, `WaitingForRunID`, `PendingToolCallID` fields
- [ ] 2.2 Define `SuspendReason` string enum constants: `SuspendReasonAwaitingHuman`, `SuspendReasonAwaitingChild`
- [ ] 2.3 Add `SuspendSignal *SuspendSignal` field to `CoordinationToolDeps`

## 3. Executor: Suspend Catch in runPipeline

- [ ] 3.1 In `afterToolCb`: after `ask_user` tool returns, if `AskPauseState.ShouldPause()`, construct `SuspendSignal{Reason: awaiting_human, QuestionID, PendingToolCallID: functionCallID}` and store on run-local state
- [ ] 3.2 In `afterToolCb`: after any spawn tool returns, if `CoordinationToolDeps.SuspendSignal` is set, propagate it to run-local suspend state
- [ ] 3.3 After `afterToolCb` sets suspend state: call `repo.UpdateSuspendContext`, then `repo.PauseRun`, then break pipeline loop
- [ ] 3.4 Ensure step count is NOT incremented between suspend signal detection and `PauseRun`

## 4. Executor: History Replay on Resume

- [ ] 4.1 Write `collectRunChain(ctx, runID) ([]string, error)` that walks `ResumedFrom` to return ordered list of run IDs (oldest first)
- [ ] 4.2 Write `loadHistoryFromMessages(ctx, runIDs []string) ([]*genai.Content, error)` that calls `FindMessagesByRunID` per run and converts JSONB `Content` to `genai.Content` slices
- [ ] 4.3 Implement JSONB → `genai.Content` conversion: `text` key → model text part; `function_calls` key → model function call parts; `function_responses` key → tool function response parts; pair by `id` field
- [ ] 4.4 In `Resume`: read `priorRun.SuspendContext` to extract `pending_tool_call_id` and tool name
- [ ] 4.5 In `Resume`: build history via `loadHistoryFromMessages`, append `FunctionResponse` part for `pending_tool_call_id` with the human answer as response body
- [ ] 4.6 In `Resume`: pass reconstructed history into ADK session/runner initialization before calling `runPipeline`
- [ ] 4.7 Handle missing `pending_tool_call_id` gracefully: log warning, fall back to text injection

## 5. Spawn Cascade

- [ ] 5.1 In `executeSingleSpawn`: after child run finishes, check child run status; if `paused`, set `CoordinationToolDeps.SuspendSignal{Reason: awaiting_child, WaitingForRunID: childRunID}`
- [ ] 5.2 In `executeSingleSpawn`: return a result map that `afterToolCb` can inspect (child run ID, child status) so parent suspend context can reference correct spawn tool call ID
- [ ] 5.3 Write `FindParentAwaitingChild(ctx, childRunID string) (*AgentRun, error)` repository method that queries `suspend_context->>'waiting_for_run_id' = $1 AND status = 'paused'`
- [ ] 5.4 In run completion path (after `runPipeline` or `FailRun`): call `FindParentAwaitingChild`; if found, enqueue resume job for parent with child output as injected tool result

## 6. HandleRespondToQuestion: Return resume_run_id

- [ ] 6.1 Extract `CreateRunWithOptions` call from `Resume` into a shared helper so it can be called synchronously before launching the goroutine
- [ ] 6.2 In `HandleRespondToQuestion`: create the new continuation run synchronously, get its ID
- [ ] 6.3 Set `AgentQuestionDTO.ResumeRunID` to the new run ID before returning HTTP response
- [ ] 6.4 Launch `Resume` goroutine with the already-created run (pass new run into `Resume` to avoid double-creation)

## 7. ACP Resume Endpoint: return resume_run_id

- [ ] 7.1 Update `ACPHandler.ResumeRun` response to include `resume_run_id` field in the returned run object
- [ ] 7.2 Update `GET /acp/v1/agents/:name/runs/:runId` response DTO to include `resume_run_id` when the run has a continuation

## 8. Bench Fix

- [ ] 8.1 In `bench/domain-test/run_domain_test.py`: remove the explicit `POST .../resume` call after responding to a question — `HandleRespondToQuestion` already resumes
- [ ] 8.2 Update bench polling logic to read `resume_run_id` from the respond response and poll that run ID for completion

## 9. Tests

- [ ] 9.1 Unit test: `loadHistoryFromMessages` round-trips JSONB messages to correct `genai.Content` types for text, function_calls, function_responses
- [ ] 9.2 Unit test: `SuspendSignal` set by `AskPauseState.RequestPause` is correctly caught in `afterToolCb` and produces correct `suspend_context` JSONB
- [ ] 9.3 Integration test: 3-turn HITL scenario — run pauses at `ask_user`, human responds, resumed run sees full history and completes correctly
- [ ] 9.4 Integration test: spawn cascade — parent pauses when child pauses; parent resumes when child completes
