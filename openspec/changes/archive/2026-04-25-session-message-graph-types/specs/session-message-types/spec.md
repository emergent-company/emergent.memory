## ADDED Requirements

### Requirement: Session schema pre-registered at startup
The system SHALL pre-register a `Session` schema type in the schema registry at server startup with the following properties: `title` (string), `started_at` (datetime), `ended_at` (datetime, optional), `message_count` (integer, default 0), `summary` (string, optional), `agent_version` (string, optional). The upsert MUST be idempotent — safe to run on every boot.

#### Scenario: Session schema available on fresh project
- **WHEN** a new project is created
- **THEN** the `Session` type SHALL be available for object creation without manual schema setup

#### Scenario: Startup idempotency
- **WHEN** the server restarts with an existing Session schema
- **THEN** the schema MUST NOT be duplicated or cause an error

### Requirement: Message schema pre-registered at startup
The system SHALL pre-register a `Message` schema type with properties: `role` (enum: user/assistant/system), `content` (string), `sequence_number` (integer), `timestamp` (datetime), `token_count` (integer, optional), `tool_calls` (JSON array, optional). The schema MUST mark `content` as the embedding target field.

#### Scenario: Message schema available on fresh project
- **WHEN** a new project is created
- **THEN** the `Message` type SHALL be available without manual setup

### Requirement: System embedding policy for Message content
The system SHALL auto-register a system-level embedding policy for the `Message` type targeting the `content` field. This policy MUST be applied to all projects and MUST NOT be deletable by users.

#### Scenario: Message content auto-embedded
- **WHEN** a Message object is created with a non-empty `content` property
- **THEN** an embedding MUST be generated for the `content` field using the project's active embedding model
