## ADDED Requirements

### Requirement: Schema assignment history is visible per project
The `memory schemas history` command (new subcommand) SHALL list all schema assignments for the current project, including those that have been uninstalled, ordered by `installed_at` descending. This requires:
1. A Goose DB migration adding `removed_at TIMESTAMPTZ DEFAULT NULL` to `kb.project_schemas`.
2. `DeleteAssignment` changes to set `removed_at = NOW()` instead of hard-deleting the row.
3. A new server endpoint `GET /api/schemas/projects/:projectId/history` returning all rows.
4. A new `memory schemas history` CLI subcommand.

#### Scenario: History shows active and removed schemas
- **WHEN** user runs `memory schemas history`
- **THEN** the output includes all schemas ever installed, with columns: Name, Version, Installed At, Removed At (empty if still active)
- **AND** active schemas show `—` in the Removed At column

#### Scenario: JSON output available
- **WHEN** user runs `memory schemas history --output json`
- **THEN** the output is a JSON array of assignment objects with fields: `schema_id`, `schema_name`, `schema_version`, `installed_at`, `removed_at` (null if active)

#### Scenario: History is scoped to current project
- **WHEN** user runs `memory schemas history` with `--project <id>` flag (or uses active project from config)
- **THEN** only assignments for that project are returned

#### Scenario: Empty history
- **WHEN** no schemas have ever been installed for the project
- **THEN** the CLI outputs "No schema history found." and exits successfully

#### Scenario: `memory schemas list` still shows only active schemas
- **WHEN** user runs `memory schemas list`
- **THEN** uninstalled schemas (with `removed_at IS NOT NULL`) do NOT appear in the output
