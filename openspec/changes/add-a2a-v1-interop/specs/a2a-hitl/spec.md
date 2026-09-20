## ADDED Requirements

### Requirement: Input-required pause
When an agent run pauses awaiting human input — whether from an `ask_user` question or a tool-approval gate — the corresponding A2A task SHALL report `status.state` = `TASK_STATE_INPUT_REQUIRED` with the prompt carried in `status.message` as an agent-role `Message`.

#### Scenario: Question surfaces as input-required
- **WHEN** a run pauses on an agent question
- **THEN** the task's `status.state` is `TASK_STATE_INPUT_REQUIRED` and `status.message.role` is `ROLE_AGENT`

#### Scenario: Tool approval surfaces as input-required
- **WHEN** a run pauses on a tool-approval gate
- **THEN** the task's `status.state` is `TASK_STATE_INPUT_REQUIRED` and the approval prompt is present in `status.message`

#### Scenario: Pause source is distinguishable
- **WHEN** a task is in `TASK_STATE_INPUT_REQUIRED`
- **THEN** the caller can distinguish a question pause from a tool-approval pause via task metadata

#### Scenario: Streamed input-required carries the discriminator
- **WHEN** a streaming run pauses for input
- **THEN** the emitted `statusUpdate` with `TASK_STATE_INPUT_REQUIRED` carries metadata distinguishing `ask_user` from `tool_approval` (the same `hitlMetadata` discriminator used by task reconstruction)

### Requirement: Resume by follow-up message
A client SHALL resume an interrupted task by sending a new `Message` with the same `taskId` (and no separate resume route). The server SHALL treat presence of `message.taskId` as the resume signal and SHALL reuse the same `Task.id`.

#### Scenario: Follow-up message resumes the task
- **WHEN** a client sends `POST /message:send` with `message.taskId` set to an interrupted task's id and a text part containing the answer
- **THEN** the server resumes the run and the returned task keeps the same `Task.id`

#### Scenario: Resume chain does not fork the task id
- **WHEN** a task is resumed and the internal engine chains a new run
- **THEN** all subsequent A2A responses continue to report the original `Task.id`

#### Scenario: Missing taskId starts a new task
- **WHEN** a client sends `POST /message:send` with no `message.taskId`
- **THEN** the server creates a new task rather than resuming an existing one

### Requirement: Resume guard for non-interrupted tasks
The system SHALL reject a resume attempt against a task that is not in `TASK_STATE_INPUT_REQUIRED`, and SHALL NOT silently create a new task.

#### Scenario: Resume of a completed task is rejected
- **WHEN** a client sends a follow-up message with the `taskId` of a task in `TASK_STATE_COMPLETED`
- **THEN** the server responds with HTTP 400 or 409 and does not create a new task

#### Scenario: Resume of an unknown task is rejected
- **WHEN** a client sends a follow-up message with an unknown `taskId`
- **THEN** the server responds with HTTP 404 and a `TASK_NOT_FOUND` reason

#### Scenario: Concurrent resume attempts are serialized
- **WHEN** two follow-up messages reference the same interrupted task concurrently
- **THEN** at most one resume is accepted and the other is rejected rather than double-answering the question

### Requirement: Streaming behavior across interruption
For `POST /message:stream`, the server MAY close the stream when the task reaches `TASK_STATE_INPUT_REQUIRED`; this deviation from the spec's "streams stay open across interruption" rule SHALL be documented and the client SHALL resume with a new streaming or synchronous request carrying the `taskId`.

#### Scenario: Stream closes at input-required
- **WHEN** a streaming run pauses for input
- **THEN** the server emits a terminal `statusUpdate` with `TASK_STATE_INPUT_REQUIRED` and closes the stream

#### Scenario: Client resumes with a new stream
- **WHEN** the client sends a new `POST /message:stream` carrying the interrupted `taskId` and the answer
- **THEN** the server continues the task and streams subsequent updates

### Requirement: Message identifier validation
The server SHALL reject a `Message` whose `contextId` and `taskId` are both supplied but inconsistent, and SHALL populate `contextId` on agent-role messages.

#### Scenario: Inconsistent context and task are rejected
- **WHEN** a client sends a `Message` whose `contextId` does not match the context of the supplied `taskId`
- **THEN** the server responds with HTTP 400

#### Scenario: Task-only message infers context
- **WHEN** a client sends a `Message` with only `taskId` set
- **THEN** the server infers the task's `contextId` and the response task carries it
