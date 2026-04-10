## ADDED Requirements

### Requirement: Create run with async mode
The system SHALL expose `POST /acp/v1/agents/:name/runs` that accepts a JSON body with `message` (array of ACP message parts), `mode` (`sync`, `async`, or `stream`), and optional `session_id`. When `mode` is `async`, the server SHALL create the run, enqueue it for execution, and return HTTP 202 with the run object including `id` and status `submitted`.

#### Scenario: Async run returns 202 immediately
- **WHEN** a client sends `POST /acp/v1/agents/my-agent/runs` with `{"message": [{"content_type": "text/plain", "content": "Hello"}], "mode": "async"}`
- **THEN** the server responds with HTTP 202 and a run object with `status: "submitted"` and a valid `id`

#### Scenario: Async run with invalid agent returns 404
- **WHEN** a client sends `POST /acp/v1/agents/nonexistent/runs` with mode `async`
- **THEN** the server responds with HTTP 404

#### Scenario: Async run without agents:write scope returns 403
- **WHEN** a client sends `POST /acp/v1/agents/my-agent/runs` with a token that lacks `agents:write` scope
- **THEN** the server responds with HTTP 403

### Requirement: Create run with sync mode
When `mode` is `sync` (or omitted — `sync` is the default), the server SHALL block until the run completes and return HTTP 200 with the final run object including `output` messages and terminal status (`completed`, `failed`, or `input-required`).

#### Scenario: Sync run blocks until completion
- **WHEN** a client sends `POST /acp/v1/agents/my-agent/runs` with `{"message": [{"content_type": "text/plain", "content": "Summarize"}], "mode": "sync"}`
- **THEN** the server blocks until the run finishes and responds with HTTP 200 and the run object with `status: "completed"` and `output` containing the agent's response

#### Scenario: Sync run that pauses returns input-required
- **WHEN** a sync run pauses because the agent asks a question via `ask_user`
- **THEN** the server returns HTTP 200 with `status: "input-required"` and an `await_request` object containing the question

#### Scenario: Sync run that fails returns 200 with failed status
- **WHEN** a sync run encounters an error during execution
- **THEN** the server responds with HTTP 200 with `status: "failed"` and an `error` object describing the failure

### Requirement: Create run with stream mode
When `mode` is `stream`, the server SHALL set SSE headers immediately, start execution in a background goroutine, and stream ACP SSE events inline on the POST response body. The stream SHALL end with a terminal run event (`run.completed`, `run.failed`, `run.cancelled`, or `run.awaiting`).

#### Scenario: Stream run returns SSE events inline
- **WHEN** a client sends `POST /acp/v1/agents/my-agent/runs` with `{"message": [...], "mode": "stream"}`
- **THEN** the server responds with `Content-Type: text/event-stream` and streams events with `event:` and `data:` fields

#### Scenario: Stream emits run lifecycle events
- **WHEN** a streamed run starts and completes successfully
- **THEN** the client receives events in order: `run.created`, `run.in-progress`, one or more `message.part` events, `message.completed`, `run.completed`

#### Scenario: Stream emits awaiting event on pause
- **WHEN** a streamed run pauses because the agent asks a question
- **THEN** the client receives `run.awaiting` as the terminal event with `await_request` in the data

### Requirement: ACP SSE event types
The system SHALL emit the following SSE event types during streamed runs: `run.created`, `run.in-progress`, `run.awaiting`, `run.completed`, `run.failed`, `run.cancelled`, `message.created`, `message.part`, `message.completed`, `generic`, `error`. Each event SHALL have an `event:` field with the event type and a `data:` field with JSON payload.

#### Scenario: Text delta events use message.part
- **WHEN** the agent generates text tokens during a streamed run
- **THEN** each token is emitted as a `message.part` event with `{"part": {"content_type": "text/plain", "content": "<token>"}}`

#### Scenario: Tool call events use message.part with trajectory metadata
- **WHEN** the agent makes a tool call during a streamed run
- **THEN** tool call start/end are emitted as `message.part` events with `TrajectoryMetadata` containing `tool_name`, `tool_input`, and `tool_output`

#### Scenario: Error during stream emits error event
- **WHEN** an unrecoverable error occurs during a streamed run
- **THEN** the server emits an `error` event with error details and then `run.failed`

### Requirement: Get run by ID
The system SHALL expose `GET /acp/v1/agents/:name/runs/:runId` that returns the current state of a run including `id`, `agent_name`, `status` (mapped to ACP status), `created_at`, `updated_at`, `output` (if completed), `await_request` (if paused), and `error` (if failed).

#### Scenario: Get a completed run
- **WHEN** a client sends `GET /acp/v1/agents/my-agent/runs/<runId>` for a completed run
- **THEN** the server responds with HTTP 200 and the run object with `status: "completed"` and `output` messages

#### Scenario: Get a paused run includes await_request
- **WHEN** a client sends `GET /acp/v1/agents/my-agent/runs/<runId>` for a paused run with a pending question
- **THEN** the response includes `status: "input-required"` and `await_request` with `question_id`, `question`, and `options`

#### Scenario: Get run with wrong agent name returns 404
- **WHEN** a client sends `GET /acp/v1/agents/wrong-agent/runs/<runId>` where the run belongs to a different agent
- **THEN** the server responds with HTTP 404

### Requirement: Cancel run
The system SHALL expose `DELETE /acp/v1/agents/:name/runs/:runId` that initiates cancellation of an active run. The run status SHALL transition to `cancelling` immediately, then to `cancelled` once execution actually stops.

#### Scenario: Cancel a running run
- **WHEN** a client sends `DELETE /acp/v1/agents/my-agent/runs/<runId>` for a run with status `working`
- **THEN** the server responds with HTTP 200 and the run object with `status: "cancelling"`
- **THEN** the run eventually transitions to `status: "cancelled"`

#### Scenario: Cancel an already completed run returns 409
- **WHEN** a client sends `DELETE /acp/v1/agents/my-agent/runs/<runId>` for a run with status `completed`
- **THEN** the server responds with HTTP 409 Conflict

#### Scenario: Cancel a submitted (queued) run
- **WHEN** a client sends `DELETE /acp/v1/agents/my-agent/runs/<runId>` for a run with status `submitted`
- **THEN** the server responds with HTTP 200 and the run transitions directly to `cancelled`

### Requirement: Resume run (human-in-the-loop)
The system SHALL expose `POST /acp/v1/agents/:name/runs/:runId/resume` that accepts a JSON body with `message` (the human's response) and optional `mode` (`sync`, `async`, `stream`). The run MUST be in `input-required` status. The server SHALL resume execution using `AgentExecutor.Resume()` with the same mode semantics as run creation.

#### Scenario: Resume a paused run with sync mode
- **WHEN** a client sends `POST /acp/v1/agents/my-agent/runs/<runId>/resume` with `{"message": [{"content_type": "text/plain", "content": "Yes, proceed"}], "mode": "sync"}` and the run is in `input-required` status
- **THEN** the server blocks until the resumed run completes and returns HTTP 200 with the final run state

#### Scenario: Resume a non-paused run returns 409
- **WHEN** a client sends `POST /acp/v1/agents/my-agent/runs/<runId>/resume` for a run that is not in `input-required` status
- **THEN** the server responds with HTTP 409 Conflict

#### Scenario: Resume with stream mode
- **WHEN** a client resumes a paused run with `mode: "stream"`
- **THEN** the server streams SSE events inline just like a streamed run creation

### Requirement: Get run event log
The system SHALL expose `GET /acp/v1/agents/:name/runs/:runId/events` that returns the complete event history for a run as a JSON array (not SSE). Events SHALL be ordered by creation time ascending. Each event includes `type`, `data`, and `created_at`.

#### Scenario: Get events for a completed run
- **WHEN** a client sends `GET /acp/v1/agents/my-agent/runs/<runId>/events` for a completed run
- **THEN** the server responds with HTTP 200 and a JSON array of events from `run.created` through `run.completed`

#### Scenario: Get events for a run with no events returns empty array
- **WHEN** a client sends `GET /acp/v1/agents/my-agent/runs/<runId>/events` for a freshly created async run that hasn't started yet
- **THEN** the server responds with HTTP 200 and `[]`

### Requirement: ACP status mapping
Run statuses SHALL be mapped from Memory internal statuses to ACP statuses: `queued` → `submitted`, `running` → `working`, `paused` → `input-required`, `success` → `completed`, `error` → `failed`, `cancelling` → `cancelling`, `cancelled` → `cancelled`, `skipped` → `completed` (with skip reason in metadata).

#### Scenario: Running status maps to working
- **WHEN** a run has Memory status `running`
- **THEN** the ACP response shows `status: "working"`

#### Scenario: Paused status maps to input-required
- **WHEN** a run has Memory status `paused`
- **THEN** the ACP response shows `status: "input-required"`

#### Scenario: Skipped maps to completed with metadata
- **WHEN** a run has Memory status `skipped` with skip reason "duplicate trigger"
- **THEN** the ACP response shows `status: "completed"` with metadata indicating it was skipped

### Requirement: ACP message format
Run output and input messages SHALL use the ACP message format: `role` (`user` or `agent`), `parts` (array of `MessagePart`). Each `MessagePart` SHALL have `content_type` (MIME type) and `content` (string). Text content SHALL use `content_type: "text/plain"`. Tool calls SHALL be represented as parts with `metadata` of type `TrajectoryMetadata` containing `tool_name`, `tool_input` (JSON string), and `tool_output` (JSON string).

#### Scenario: Text message translated to ACP format
- **WHEN** a Memory run message has `role: "assistant"` and `content: {"text": "Hello world"}`
- **THEN** the ACP representation is `{"role": "agent", "parts": [{"content_type": "text/plain", "content": "Hello world"}]}`

#### Scenario: Tool call translated with trajectory metadata
- **WHEN** a Memory run has a tool call with `tool_name: "search"`, `input: {"query": "test"}`, `output: {"results": []}`
- **THEN** the ACP representation includes a part with `metadata: {"type": "trajectory", "tool_name": "search", "tool_input": "{\"query\":\"test\"}", "tool_output": "{\"results\":[]}"}`

### Requirement: Event persistence
All ACP SSE events emitted during a run (regardless of mode) SHALL be persisted to the `kb.acp_run_events` table with `run_id`, `event_type`, `data` (JSONB), and `created_at`. This enables the `GET /runs/:runId/events` endpoint to return the full history.

#### Scenario: Sync run events are persisted
- **WHEN** a sync run completes
- **THEN** the `kb.acp_run_events` table contains the full sequence of events for that run

#### Scenario: Async run events are persisted
- **WHEN** an async run executes in the background and completes
- **THEN** the `kb.acp_run_events` table contains all events from creation to completion

### Requirement: Run request with session_id
The `POST /acp/v1/agents/:name/runs` request body SHALL accept an optional `session_id` field. When provided, the created run SHALL be linked to the specified ACP session via the `acp_session_id` column on `kb.agent_runs`.

#### Scenario: Run created with session_id is linked
- **WHEN** a client creates a run with `session_id: "<session-uuid>"`
- **THEN** the run's `acp_session_id` is set to that UUID and the run appears in the session's history

#### Scenario: Run created without session_id has null session
- **WHEN** a client creates a run without a `session_id` field
- **THEN** the run's `acp_session_id` is null

#### Scenario: Run with non-existent session_id returns 400
- **WHEN** a client creates a run with a `session_id` that does not exist
- **THEN** the server responds with HTTP 400 Bad Request
