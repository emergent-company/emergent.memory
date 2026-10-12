# Spec: session-message-types

## Purpose

Schema pre-registration and embedding policies for Session and Message graph object types, applied at server startup across all projects.

## Requirements

### Requirement: Session schema pre-registered at startup
The system SHALL register a `Session` object type as a read-time-resolved **system** schema: a single
`kb.graph_schemas` row with `source='builtin'` and `is_system=true` (name `session-message-types`,
version `1.0.0`), upserted idempotently at server startup. The `Session` type SHALL be available in
every project through the compiled-types and extraction-schema paths WITHOUT a `kb.project_schemas`
row, and SHALL NOT be auto-assigned to projects. The type SHALL define: `title` (string),
`started_at` (datetime), `ended_at` (datetime, optional), `message_count` (integer, default 0),
`summary` (string, optional), `agent_version` (string, optional).

#### Scenario: Session schema available on fresh project
- **WHEN** a new project is created
- **THEN** the `Session` type SHALL be available for object creation without manual schema setup and
  without any `kb.project_schemas` row for the system pack

#### Scenario: System pack is not project-assigned
- **WHEN** the system pack is resolved for a project
- **THEN** no `kb.project_schemas` row SHALL exist for the `session-message-types` pack, and the type
  is still returned by the compiled-types and extraction-schema endpoints

#### Scenario: Startup idempotency
- **WHEN** the server restarts with an existing system `Session` schema
- **THEN** the schema MUST NOT be duplicated or cause an error

### Requirement: Message schema pre-registered at startup
The system SHALL register a `Message` object type as a read-time-resolved **system** schema
(`source='builtin'`, `is_system=true`), available in every project without a `kb.project_schemas` row.
The type SHALL define: `role` (enum: user/assistant/system), `content` (string), `sequence_number`
(integer), `timestamp` (datetime), `token_count` (integer, optional), `tool_calls` (JSON array,
optional). The schema MUST mark `content` as the embedding target field.

#### Scenario: Message schema available on fresh project
- **WHEN** a new project is created
- **THEN** the `Message` type SHALL be available without manual setup and without any
  `kb.project_schemas` row for the system pack

### Requirement: System embedding policy for Message content
The system SHALL auto-register a system-level embedding policy for the `Message` type targeting the `content` field. This policy MUST be applied to all projects and MUST NOT be deletable by users.

#### Scenario: Message content auto-embedded
- **WHEN** a Message object is created with a non-empty `content` property
- **THEN** an embedding MUST be generated for the `content` field using the project's active embedding model
