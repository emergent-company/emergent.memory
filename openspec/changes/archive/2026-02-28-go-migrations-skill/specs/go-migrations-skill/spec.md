## ADDED Requirements

### Requirement: Agent can create a new migration file
The skill SHALL instruct the agent to generate a new Goose migration file using `go run ./cmd/migrate -c create <name>` from `apps/server-go/`, producing a sequentially-numbered `.sql` file in `apps/server-go/migrations/`.

#### Scenario: Agent creates a migration for a new table
- **WHEN** the user asks the agent to create a migration for a new table
- **THEN** the agent runs `go run ./cmd/migrate -c create <snake_case_name>` from `apps/server-go/`
- **THEN** a new file named `{version}_{name}.sql` appears in `apps/server-go/migrations/`

### Requirement: Agent writes correct Goose SQL
The skill SHALL instruct the agent to write both `Up` and `Down` blocks using correct Goose directives, schema-qualified table names, and idempotent SQL constructs.

#### Scenario: Agent writes a migration with a new table
- **WHEN** the agent writes the SQL for the migration
- **THEN** the file contains `-- +goose Up` and `-- +goose Down` blocks
- **THEN** all table names are prefixed with `kb.` or `core.`
- **THEN** `CREATE TABLE` uses `IF NOT EXISTS` and `DROP TABLE` uses `IF EXISTS`

#### Scenario: Agent writes a migration with a concurrent index
- **WHEN** the migration requires `CREATE INDEX CONCURRENTLY`
- **THEN** the agent adds `-- +goose NO TRANSACTION` before the `Up` block

### Requirement: Agent applies and verifies migrations
The skill SHALL instruct the agent to apply migrations with `task migrate:up` and confirm success with `task migrate:status`.

#### Scenario: Agent applies a pending migration
- **WHEN** the agent runs `task migrate:up`
- **THEN** the agent checks the output for errors
- **THEN** the agent runs `task migrate:status` to confirm the new version is applied

### Requirement: Agent can roll back a migration
The skill SHALL instruct the agent to roll back the most recent migration using `task migrate:down` when asked or when testing the down path.

#### Scenario: Agent rolls back and re-applies to verify round-trip
- **WHEN** the user asks to test the migration round-trip
- **THEN** the agent runs `task migrate:up`, then `task migrate:down`, then `task migrate:up`
- **THEN** the agent confirms status is the same after the round-trip

### Requirement: Agent handles existing-database onboarding
The skill SHALL instruct the agent to use `mark-applied` when a database already has the schema and the migration should be recorded without re-running.

#### Scenario: Marking the baseline as applied on an existing database
- **WHEN** the database already has the schema but no `goose_db_version` entries
- **THEN** the agent runs `go run ./cmd/migrate -c mark-applied -v 1` from `apps/server-go/`
- **THEN** the agent runs `task migrate:status` to confirm the baseline is recorded

### Requirement: Agent never edits an already-applied migration
The skill SHALL explicitly prohibit editing a migration that has already been applied. Corrections MUST be made in a new migration file.

#### Scenario: User asks to fix a bug in an applied migration
- **WHEN** the user asks to change an already-applied migration
- **THEN** the agent declines to edit it
- **THEN** the agent creates a new migration that applies the corrective change
