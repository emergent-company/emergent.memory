## MODIFIED Requirements

### Requirement: Built-in blueprints are seeded into the global registry at startup

The server SHALL seed the platform's built-in packs (currently `agent-notes`, `dreaming-agent`,
`operator`, `task-board`, `session-message-types`) as global, published blueprints (`project_id`
NULL, `origin="builtin"`) at startup, idempotently by `(name, version)`. Re-running the seeder on a
later boot SHALL update the existing rows rather than create duplicates.

#### Scenario: Built-ins exist after startup

- **WHEN** the server starts
- **THEN** a global published blueprint with `origin="builtin"` exists for each built-in pack

#### Scenario: Seeding is idempotent

- **WHEN** the server starts a second time with the same pack definitions
- **THEN** no duplicate built-in blueprint rows are created
