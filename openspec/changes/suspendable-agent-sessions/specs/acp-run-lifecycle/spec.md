## MODIFIED Requirements

### Requirement: Resume run (human-in-the-loop)
The system SHALL expose `POST /acp/v1/agents/:name/runs/:runId/resume` that accepts a JSON body with `message` (the human's response) and optional `mode` (`sync`, `async`, `stream`). The run MUST be in `input-required` status. The server SHALL resume execution using `AgentExecutor.Resume()` with the same mode semantics as run creation. On resume, the executor SHALL replay full conversation history from `kb.agent_run_messages` (walking the `ResumedFrom` chain) and inject the human response as a `FunctionResponse` for the `pending_tool_call_id` stored in `suspend_context` — no synthetic prompt injection. The response SHALL include `resume_run_id` identifying the new continuation run ID. **Clients MUST poll `resume_run_id`, not the original paused run ID, to observe run completion.**

#### Scenario: Resume a paused run with sync mode
- **WHEN** a client sends `POST /acp/v1/agents/my-agent/runs/<runId>/resume` with `{"message": [{"content_type": "text/plain", "content": "Yes, proceed"}], "mode": "sync"}` and the run is in `input-required` status
- **THEN** the server blocks until the resumed run completes and returns HTTP 200 with the final run state including `resume_run_id`

#### Scenario: Resume response includes resume_run_id
- **WHEN** `HandleRespondToQuestion` is called or the ACP resume endpoint is used
- **THEN** the response body contains `resume_run_id` set to the new continuation run's ID
- **THEN** clients that poll the original paused run ID see it in `running` status (not `completed`)

#### Scenario: Resume uses history replay not prompt injection
- **WHEN** a paused run with 10 prior conversation turns is resumed with a human answer
- **THEN** the ADK session is initialized with all 10 prior turns reconstructed from `kb.agent_run_messages`
- **THEN** the human answer is injected as a `FunctionResponse` for `pending_tool_call_id`
- **THEN** no synthetic "here is what happened" text message appears in the conversation

#### Scenario: Resume a run not in input-required status returns 422
- **WHEN** a client sends a resume request for a run with status `completed` or `working`
- **THEN** the server responds with HTTP 422 Unprocessable Entity

## ADDED Requirements

### Requirement: Get run returns resume_run_id for resumed runs
`GET /acp/v1/agents/:name/runs/:runId` SHALL include `resume_run_id` in the response when the run was paused and has since been resumed, identifying the active continuation run.

#### Scenario: Get a resumed run includes resume_run_id
- **WHEN** a client gets a run that was paused and then resumed
- **THEN** the response includes `resume_run_id` pointing to the active continuation run ID

#### Scenario: Get an active run omits resume_run_id
- **WHEN** a client gets a run that has never been paused
- **THEN** `resume_run_id` is absent or null in the response
