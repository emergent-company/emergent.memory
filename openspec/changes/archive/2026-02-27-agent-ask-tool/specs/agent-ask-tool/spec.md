## ADDED Requirements

### Requirement: Ask User Tool

The system SHALL provide an `ask_user` ADK tool that allows agents to pose a question to a human user with optional structured choices, pausing execution until the user responds.

#### Scenario: Agent asks a question with options

- **WHEN** an agent calls `ask_user` with a `question` string and an `options` array of `[{label, value}]`
- **THEN** the tool SHALL create an `agent_question` record with `status: pending`
- **AND** the tool SHALL signal the executor to pause the run on the next model callback

#### Scenario: Agent asks an open-ended question

- **WHEN** an agent calls `ask_user` with a `question` string and no `options` (or empty array)
- **THEN** the tool SHALL create an `agent_question` record with `status: pending` and empty options
- **AND** the user SHALL be able to respond with free-text input

#### Scenario: Tool returns confirmation before pause

- **WHEN** the `ask_user` tool executes
- **THEN** the tool SHALL return a success result to the agent containing the question ID and a message indicating the run will pause
- **AND** the `beforeModelCb` SHALL pause the run on its next invocation by calling `repo.PauseRun()`

### Requirement: Question Persistence

The system SHALL persist agent questions in a dedicated `kb.agent_questions` table with full lifecycle tracking.

#### Scenario: Question record created

- **WHEN** an agent calls `ask_user`
- **THEN** the system SHALL insert a row into `kb.agent_questions` with the `run_id`, `agent_id`, `project_id`, `question`, `options`, and `status: pending`

#### Scenario: Question answered

- **WHEN** a user responds to a pending question
- **THEN** the system SHALL update the question record with the `response`, `responded_by` user ID, `responded_at` timestamp, and `status: answered`

#### Scenario: Question cancelled on new question

- **WHEN** an agent calls `ask_user` and a previous question for the same run is still `status: pending`
- **THEN** the system SHALL set the prior question's status to `cancelled` before creating the new question

### Requirement: Question Notification

The system SHALL create a notification in the user's inbox when an agent asks a question.

#### Scenario: Notification with structured options

- **WHEN** an agent asks a question with options
- **THEN** the system SHALL create a notification with `type: agent_question`, `importance: important`, and `actions` populated from the question's options array
- **AND** the notification's `relatedResourceType` SHALL be `agent_question` and `relatedResourceID` SHALL be the question ID

#### Scenario: Notification for open-ended question

- **WHEN** an agent asks a question without options
- **THEN** the system SHALL create a notification with an `actionURL` linking to the question response page
- **AND** the `actions` array SHALL be empty

### Requirement: Question Response API

The system SHALL provide an API endpoint for users to respond to agent questions.

#### Scenario: Successful response to pending question

- **WHEN** a user sends `POST /api/projects/:projectId/agent-questions/:questionId/respond` with a `response` body
- **THEN** the system SHALL update the question, mark the notification as read with `actionStatus: completed`, and initiate an async resume of the paused agent run
- **AND** the endpoint SHALL return `202 Accepted` with the new run ID

#### Scenario: Response to already-answered question

- **WHEN** a user sends a response to a question with `status: answered`
- **THEN** the endpoint SHALL return `409 Conflict` with a message indicating the question has already been answered

#### Scenario: Response to question from wrong project

- **WHEN** a user sends a response to a question that does not belong to the specified project
- **THEN** the endpoint SHALL return `404 Not Found`

#### Scenario: Response while run still pausing

- **WHEN** a user responds to a question but the associated run's status is still `running` (pause not yet committed)
- **THEN** the endpoint SHALL return `409 Conflict` indicating the run has not yet paused

### Requirement: Resume with Answer Context

The system SHALL resume a paused agent run with the user's answer injected as context when a question is answered.

#### Scenario: Answer injected into resumed run

- **WHEN** a user responds to a question and the agent run is paused
- **THEN** the system SHALL call `executor.Resume()` with a `UserMessage` that includes the original question and the user's response
- **AND** the resumed run SHALL be linked to the prior run via `resumed_from`

#### Scenario: Resume runs asynchronously

- **WHEN** the response endpoint triggers a resume
- **THEN** the resume SHALL execute in a background goroutine
- **AND** the HTTP response SHALL return immediately without waiting for the agent to complete

### Requirement: Tool Registration

The `ask_user` tool SHALL be opt-in and only available to agents whose definition includes it.

#### Scenario: Agent definition includes ask_user

- **WHEN** an agent definition has `"ask_user"` in its `tools` array
- **THEN** the executor SHALL build and inject the `ask_user` tool into the agent's tool set

#### Scenario: Agent definition does not include ask_user

- **WHEN** an agent definition does not list `"ask_user"` in its `tools` array
- **THEN** the `ask_user` tool SHALL NOT be available to the agent

### Requirement: Question List API

The system SHALL provide API endpoints to list and retrieve agent questions.

#### Scenario: List questions for a run

- **WHEN** a user sends `GET /api/projects/:projectId/agent-runs/:runId/questions`
- **THEN** the system SHALL return all questions associated with that run, ordered by creation time

#### Scenario: List pending questions for a project

- **WHEN** a user sends `GET /api/projects/:projectId/agent-questions?status=pending`
- **THEN** the system SHALL return all pending questions across all agents in the project

### Requirement: Admin UI Question Card

The admin UI SHALL render agent questions inline within notifications and agent run detail views.

#### Scenario: Notification renders response buttons

- **WHEN** a notification of type `agent_question` with options is displayed
- **THEN** the UI SHALL render clickable buttons for each option that send the response via the API

#### Scenario: Notification renders text input

- **WHEN** a notification of type `agent_question` without options is displayed
- **THEN** the UI SHALL render a text input field with a submit button

#### Scenario: Run detail shows Q&A history

- **WHEN** a user views an agent run detail page
- **THEN** the UI SHALL display all questions asked during the run with their status and responses
