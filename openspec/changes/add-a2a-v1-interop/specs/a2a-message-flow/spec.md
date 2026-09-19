## ADDED Requirements

### Requirement: Send message endpoint
The system SHALL expose `POST /message:send` accepting a `SendMessageRequest` with a required `message` (`role`, `parts[]`, `messageId`). The server SHALL respond with either a `task` or a `message`, and SHALL honour `configuration.returnImmediately`: when false (default) the request blocks until the task reaches a terminal or interrupted state; when true it returns as soon as the task is created.

#### Scenario: Synchronous send returns a completed task
- **WHEN** an authenticated client sends `POST /message:send` with a text part and `returnImmediately` unset
- **THEN** the server blocks until the run finishes and responds with a `task` whose `status.state` is `TASK_STATE_COMPLETED`

#### Scenario: Async send returns immediately
- **WHEN** a client sends `POST /message:send` with `configuration.returnImmediately: true`
- **THEN** the server responds without blocking with a `task` whose `status.state` is `TASK_STATE_SUBMITTED` or `TASK_STATE_WORKING`

#### Scenario: Empty message is rejected
- **WHEN** a client sends `POST /message:send` with no parts
- **THEN** the server responds with HTTP 400 and an `InvalidAgentResponseError`-free validation error envelope

#### Scenario: Unauthenticated send is rejected
- **WHEN** a client sends `POST /message:send` without a token
- **THEN** the server responds with HTTP 401

### Requirement: Task representation
The system SHALL represent every agent run as an A2A `Task` with `id`, `contextId`, `status`, and optionally `artifacts` and `history`. `Task.id` SHALL remain stable across a human-in-the-loop resume chain and SHALL NOT expose the internal resume run identifier.

#### Scenario: Task id is the run id
- **WHEN** a task is created for an internal run with id `<runId>`
- **THEN** the returned `Task.id` equals `<runId>`

#### Scenario: Task id is stable across resume
- **WHEN** a task in `TASK_STATE_INPUT_REQUIRED` is resumed with a follow-up message
- **THEN** the returned `Task.id` is unchanged from the original task

#### Scenario: Resume run id is not exposed
- **WHEN** a resume internally creates a chained run
- **THEN** no A2A response field contains the internal `resume_run_id` as `Task.id`

### Requirement: Task status mapping
The facade SHALL translate internal run status to exactly the A2A v1.0 `TaskState` vocabulary and SHALL never emit an internal status string on the wire.

| Internal status | A2A `TaskState` |
|---|---|
| `submitted` | `TASK_STATE_SUBMITTED` |
| `working` | `TASK_STATE_WORKING` |
| `completed` | `TASK_STATE_COMPLETED` |
| `failed` | `TASK_STATE_FAILED` |
| `input-required` | `TASK_STATE_INPUT_REQUIRED` |
| `cancelled` | `TASK_STATE_CANCELED` |
| `cancelling` | `TASK_STATE_WORKING` (message: cancellation requested) |
| `skipped` | `TASK_STATE_COMPLETED` (message: skip reason) |

#### Scenario: Cancelled maps to single-L CANCELED
- **WHEN** a run has internal status `cancelled`
- **THEN** the task's `status.state` is `TASK_STATE_CANCELED`

#### Scenario: Cancelling is never emitted as a state
- **WHEN** a run has internal status `cancelling`
- **THEN** the task's `status.state` is `TASK_STATE_WORKING` and no `cancelling` string appears in the response

#### Scenario: Skipped surfaces as completed with reason
- **WHEN** a run has internal status `skipped` with a skip reason
- **THEN** the task's `status.state` is `TASK_STATE_COMPLETED` and the skip reason is present in `status.message`

#### Scenario: Every emitted state is a valid A2A value
- **WHEN** any task is serialized across the full range of internal statuses
- **THEN** every `status.state` value is a member of the A2A v1.0 `TaskState` enum

### Requirement: Message parts translation
The system SHALL translate internal message content into A2A unified `Part` objects discriminated by member presence (`text`, `raw`, `url`, `data`) with no `kind` field. Text content SHALL map to a `text` part, and tool-call trajectories SHALL map to `data` parts.

#### Scenario: Assistant text becomes a text part
- **WHEN** a run completes with assistant text output
- **THEN** the task artifact contains a `part` with a `text` member

#### Scenario: Parts are discriminated by member, not kind
- **WHEN** any A2A message part is serialized
- **THEN** it contains one of `text`, `raw`, `url`, or `data` and contains no `kind` field

#### Scenario: Tool call is represented as a data part
- **WHEN** a run includes a tool call
- **THEN** an artifact contains a `data` part carrying the tool name and input/output

### Requirement: Context continuity
`Task.contextId` SHALL map to the existing session record (`kb.acp_sessions`). When a client sends a message without `contextId`, the server SHALL create a context lazily; when `contextId` is supplied, the server SHALL link the task to it.

#### Scenario: Context is created lazily
- **WHEN** a client sends a message without `contextId`
- **THEN** the response task has a non-empty `contextId`

#### Scenario: Supplied context is honoured
- **WHEN** a client sends a message with a valid `contextId`
- **THEN** the created task's `contextId` equals the supplied value

#### Scenario: Unknown context is rejected
- **WHEN** a client sends a message with a `contextId` that does not exist in the project
- **THEN** the server responds with HTTP 400

### Requirement: Get and list tasks
The system SHALL expose `GET /tasks/{id}` and project-scoped `GET /tasks`, requiring `agents:read`. `GET /tasks/{id}` SHALL support `historyLength`; `GET /tasks` SHALL support `contextId`, `status`, `pageSize`, and `pageToken` and return `tasks`, `nextPageToken`, `pageSize`, and `totalSize`.

#### Scenario: Get existing task
- **WHEN** an authenticated client sends `GET /tasks/{id}` for a task in its project
- **THEN** the server responds with HTTP 200 and the task representation

#### Scenario: Get missing task returns A2A not-found
- **WHEN** a client sends `GET /tasks/{id}` for an unknown id
- **THEN** the server responds with HTTP 404 and a `google.rpc.Status` envelope whose detail reason is `TASK_NOT_FOUND`

#### Scenario: Task from another project is not visible
- **WHEN** a client requests a task id belonging to a different project
- **THEN** the server responds with HTTP 404

#### Scenario: List is filtered by context
- **WHEN** a client sends `GET /tasks?contextId=<id>`
- **THEN** the response contains only tasks linked to that context

### Requirement: Cancel task endpoint
The system SHALL expose `POST /tasks/{id}:cancel` requiring `agents:write`. Cancelling a running task SHALL request cancellation of the underlying run and the task SHALL eventually report `TASK_STATE_CANCELED`. Cancelling a terminal task SHALL be rejected.

#### Scenario: Cancel a working task
- **WHEN** an authenticated client cancels a task in `TASK_STATE_WORKING`
- **THEN** the server acknowledges the cancellation and the underlying run transitions to cancelled

#### Scenario: Cancel a terminal task is rejected
- **WHEN** a client cancels a task already in `TASK_STATE_COMPLETED`
- **THEN** the server responds with HTTP 400 and a `TASK_NOT_CANCELABLE` detail reason

#### Scenario: Cancel requires agents:write
- **WHEN** a client cancels a task with a token lacking `agents:write`
- **THEN** the server responds with HTTP 403

### Requirement: Streaming message endpoint
The system SHALL expose `POST /message:stream` returning `text/event-stream`, where each `data:` line is a `StreamResponse` discriminated by exactly one of the members `task`, `message`, `statusUpdate`, or `artifactUpdate`. Events SHALL be emitted in order and SHALL NOT be reordered.

#### Scenario: Stream uses A2A wrapper members
- **WHEN** a client streams a message
- **THEN** every SSE payload contains one of `task`, `message`, `statusUpdate`, or `artifactUpdate` and none contains a `kind` field

#### Scenario: Working then completed status updates
- **WHEN** a run transitions from working to completed while streaming
- **THEN** the stream emits a `statusUpdate` for working followed by a terminal `statusUpdate` or `task` for completed, in that order

#### Scenario: Text deltas accumulate into an artifact
- **WHEN** the agent emits text deltas during a streaming run
- **THEN** the stream emits `artifactUpdate` events carrying growing text parts

#### Scenario: Stream ends at terminal state
- **WHEN** the run reaches a terminal A2A state
- **THEN** the server closes the SSE stream after emitting the terminal event

### Requirement: Subscribe to task endpoint
The system SHALL expose `POST /tasks/{id}:subscribe` (per the spec's HTTP binding section) allowing a client to attach to an existing task's event stream and receive ordered updates without resending the message.

#### Scenario: Subscribe streams existing task updates
- **WHEN** an authenticated client subscribes to an in-progress task
- **THEN** it receives subsequent `statusUpdate`/`artifactUpdate` events for that task

#### Scenario: Subscribe to unknown task returns not-found
- **WHEN** a client subscribes to an unknown task id
- **THEN** the server responds with HTTP 404 and a `TASK_NOT_FOUND` reason

### Requirement: Scopes for message flow
`POST /message:send`, `POST /message:stream`, and `POST /tasks/{id}:cancel` SHALL require `agents:write`. `GET /tasks/{id}`, `GET /tasks`, and `POST /tasks/{id}:subscribe` SHALL require `agents:read`.

#### Scenario: Read-only token cannot send
- **WHEN** a client sends a message with a token having only `agents:read`
- **THEN** the server responds with HTTP 403
