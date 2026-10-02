## Purpose

Defines the compiled-types contract for board-enabled object types: the
board/work configuration a schema pack declares (in either the array or the map
storage format) must survive compilation, so consumers such as the gateway board
can derive lane order and the type preview from the schema rather than a
hard-coded list.

## ADDED Requirements

### Requirement: Compiled types preserve board work config for array-form object-type schemas

The compiled-types path (`GetCompiledTypesByProject`) SHALL preserve the
object-driven work configuration declared on an object type — `boardEnabled`,
`allowedStatuses`, `skipEmbeddings`, `skipExtraction`, and
`excludeFromSearch` — regardless of whether the pack's `object_type_schemas`
column is stored in the array format (user files and blueprint sample YAML such
as `blueprints/task-board/schemas/task-board.yaml`) or the map format (blueprint
seeds). This SHALL match the fields the runtime extraction normalisation already
preserves. The compiled output for a board-enabled `Task` type declared as an
array entry SHALL carry `boardEnabled: true` and its declared
`allowedStatuses`.

#### Scenario: Array-form board type compiles with its work config

- **WHEN** a schema pack stores object types as a JSON array and one entry
  declares `boardEnabled: true`, `allowedStatuses: [ready, in_progress, review,
  revision, blocked, done]`, and the operational skip flags
- **THEN** the compiled object type for that entry carries `boardEnabled: true`,
  the declared `allowedStatuses`, and the skip flags

#### Scenario: Map-form board type compiles with its work config

- **WHEN** a schema pack stores object types as a JSON map and one entry
  declares a board work config
- **THEN** the compiled object type carries the same field values, so the two
  storage formats are consistent

#### Scenario: Scope-key alias handling is unaffected

- **WHEN** an array entry declares both `scopeKey` and its `scope_key` alias
- **THEN** the canonical `scopeKey` wins and the snake-case alias does not
  survive the array-to-map normalisation
