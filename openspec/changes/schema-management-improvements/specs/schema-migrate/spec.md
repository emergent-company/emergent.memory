## ADDED Requirements

### Requirement: Migrate live type/property names across graph data
The `memory schemas migrate` command (new subcommand) SHALL send a migration request to the server that renames type names and/or property keys across live graph objects and edges. The server executes the changes atomically in a single transaction. A `--dry-run` flag returns counts without committing.

The server exposes a new endpoint: `POST /api/schemas/projects/:projectId/migrate`

Request body fields:
- `from_schema_id` (string, optional) — schema the data is currently using
- `to_schema_id` (string, optional) — schema the data should reflect after migration
- `renames` (array) — explicit rename instructions: `[{from_type, to_type}, {from_type, from_property, to_property}]`
- `dry_run` (bool) — if true, return counts without committing

Response body fields:
- `objects_updated` (int)
- `edges_updated` (int)
- `renames_applied` (array of `{from, to, objects_affected, edges_affected}`)
- `dry_run` (bool)

#### Scenario: Rename an object type across all graph objects
- **WHEN** user runs `memory schemas migrate --rename-type OldFoo:NewFoo`
- **THEN** the server updates `kb.graph_objects.type_name` from `OldFoo` to `NewFoo` for all objects in the project
- **AND** the response reports the count of updated objects

#### Scenario: Rename a property key within a type
- **WHEN** user runs `memory schemas migrate --rename-property OldFoo.old_prop:OldFoo.new_prop`
- **THEN** the server updates the `properties` JSONB field for all `OldFoo` objects: key `old_prop` → `new_prop`
- **AND** the response reports the count of updated objects

#### Scenario: Dry run returns counts without changes
- **WHEN** user runs `memory schemas migrate --rename-type OldFoo:NewFoo --dry-run`
- **THEN** the server returns `{"objects_updated": N, "dry_run": true}` without committing any changes
- **AND** the CLI output prefixes each line with `[dry-run]`

#### Scenario: Zero-row rename triggers a warning
- **WHEN** a `--rename-type` or `--rename-property` instruction matches no rows
- **THEN** the CLI prints a warning: `WARNING: rename "OldFoo" → "NewFoo" affected 0 objects/edges`
- **AND** the command still exits successfully (0 affected is not an error)

#### Scenario: Multiple renames in one command
- **WHEN** user provides multiple `--rename-type` and/or `--rename-property` flags
- **THEN** all renames are sent in a single request and applied atomically
- **AND** the response lists each rename with its affected count

#### Scenario: No renames provided
- **WHEN** user runs `memory schemas migrate` with no rename flags
- **THEN** the CLI returns an error: "at least one --rename-type or --rename-property flag is required"
