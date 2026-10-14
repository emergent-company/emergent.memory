# Spec: session-message-types

## Purpose

The platform's conversation primitives for graph objects — the `Session` and `Message` object types and the `has_message` relationship, shipped as the built-in `session-message-types` blueprint pack that materializes only when a project applies the blueprint — together with the system embedding policy that auto-embeds `Message.content` in every project.

## Requirements

### Requirement: System embedding policy for Message content
The system SHALL auto-register a system-level embedding policy for the `Message` type targeting the `content` field. This policy MUST be applied to all projects and MUST NOT be deletable by users.

#### Scenario: Message content auto-embedded
- **WHEN** a Message object is created with a non-empty `content` property
- **THEN** an embedding MUST be generated for the `content` field using the project's active embedding model

### Requirement: Session schema provided by the built-in blueprint
The platform SHALL ship the Session/Message conversation primitives as a built-in blueprint pack named
`session-message-types` (version `1.0.0`), seeded as a global published blueprint (`project_id` NULL,
`origin='builtin'`) by the blueprints builtin seeder. The `Session` object type SHALL materialize ONLY
when the blueprint is applied to a project: applying the blueprint SHALL create (or reuse) a global
`source='builtin'` schema pack and assign it to the project, registering the `Session` type. There is
no global system pack, no read-time resolution, and no `is_system` concept. The type SHALL define:
`title` (string), `started_at` (datetime), `ended_at` (datetime, optional), `message_count` (integer,
default 0), `summary` (string, optional), `agent_version` (string, optional).

#### Scenario: Session schema available after blueprint apply
- **WHEN** the `session-message-types` blueprint is applied to a project
- **THEN** the `Session` type SHALL be available for object creation via a `kb.project_schemas`
  assignment to the global `source='builtin'` pack

#### Scenario: No auto-provisioning at project creation or boot
- **WHEN** a project is created (or the server boots) without the `session-message-types` blueprint
  applied
- **THEN** no `kb.project_schemas` row SHALL exist for the `session-message-types` pack, and the
  `Session` type SHALL NOT appear in the project's compiled types

#### Scenario: Seeding is idempotent
- **WHEN** the server restarts with the built-in blueprint already seeded
- **THEN** the blueprint row SHALL NOT be duplicated or cause an error

### Requirement: Message schema provided by the built-in blueprint
The built-in `session-message-types` blueprint SHALL define a `Message` object type that materializes
only when the blueprint is applied. The type SHALL define: `role` (enum: user/assistant/system),
`content` (string), `sequence_number` (integer), `timestamp` (datetime), `token_count` (integer,
optional), `tool_calls` (JSON array, optional). The schema MUST mark `content` as the embedding target
field.

#### Scenario: Message schema available after blueprint apply
- **WHEN** the `session-message-types` blueprint is applied to a project
- **THEN** the `Message` type SHALL be available for object creation and its `content` property SHALL
  carry `x-embedding-target: true`

### Requirement: has_message relationship provided by the built-in blueprint
The built-in `session-message-types` blueprint SHALL define a `has_message` relationship type linking
`Session` (source) to `Message` (target), materialized only when the blueprint is applied.

#### Scenario: has_message available after blueprint apply
- **WHEN** the `session-message-types` blueprint is applied to a project
- **THEN** the `has_message` relationship type SHALL appear in the project's compiled relationship
  types with `sourceType=Session` and `targetType=Message`
