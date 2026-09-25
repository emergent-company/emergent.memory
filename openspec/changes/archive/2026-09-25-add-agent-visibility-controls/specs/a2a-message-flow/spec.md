## MODIFIED Requirements

### Requirement: Send message endpoint

The system SHALL expose `POST /message:send` accepting a `SendMessageRequest` with a required `message` (`role`, `parts[]`, `messageId`). The server SHALL respond with either a `task` or a `message`, and SHALL honour `configuration.returnImmediately`: when false (default) the request blocks until the task reaches a terminal or interrupted state; when true it returns as soon as the task is created.

When the message's `metadata` carries a skill selector (`skillId`, or its tolerated alias `skill_id`), the server SHALL resolve the target agent definition by slug with the following visibility precedence: an `external` definition is preferred; a `project` definition is an accepted fallback (resolvable by slug, but not advertised in the A2A agent card); an `internal` definition is NEVER resolvable via A2A. An unknown or `internal`-only slug SHALL return HTTP 400 with a `SKILL_NOT_FOUND` detail reason and SHALL NOT fall back to the CLI assistant. When the metadata carries no skill selector, the server SHALL use the project's general-purpose CLI assistant.

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

#### Scenario: External skill slug is preferred

- **WHEN** a message carries a `skillId` that matches both an `external` and a `project` definition
- **THEN** the server resolves the `external` definition

#### Scenario: Project skill slug resolves by slug but is not advertised

- **WHEN** a message carries a `skillId` that matches only a `project` definition
- **THEN** the server resolves that `project` definition and executes it, even though it is not advertised in the A2A agent card

#### Scenario: Internal skill slug is never resolvable

- **WHEN** a message carries a `skillId` that matches only an `internal` definition
- **THEN** the server responds with HTTP 400 and a `SKILL_NOT_FOUND` detail reason, and does not fall back to the CLI assistant
