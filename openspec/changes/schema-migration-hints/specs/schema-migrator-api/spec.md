## ADDED Requirements

### Requirement: Migration preview endpoint returns risk assessment without modifying data
The system SHALL expose `POST /api/schemas/projects/:projectId/migrate/preview` that computes a full per-object migration plan between two schema versions and returns risk assessment results without making any data changes.

#### Scenario: Preview migration between two schema versions
- **WHEN** a user calls the preview endpoint with valid `from_schema_id` and `to_schema_id`
- **THEN** the server SHALL return a list of per-object migration results with `risk_level`, `dropped_props`, `coerced_props`, `issues`
- **THEN** no data in `kb.graph_objects` or `kb.graph_edges` SHALL be modified
- **THEN** the response SHALL include an aggregate `overall_risk_level` (highest risk across all objects)

#### Scenario: Preview with no objects of affected types
- **WHEN** the project has no objects of any type defined in the schema
- **THEN** the preview SHALL return an empty results list and `overall_risk_level: "safe"`

### Requirement: Migration execute endpoint applies SchemaMigrator with risk gate
The system SHALL expose `POST /api/schemas/projects/:projectId/migrate/execute` that runs the full `SchemaMigrator` migration: type renames, property renames, field archiving for dropped properties, and schema_version updates. Declared `removed_properties` in migration hints SHALL suppress warnings for those drops (data is still archived).

#### Scenario: Execute migration with safe risk level
- **WHEN** the migration has `overall_risk_level: "safe"` or `"cautious"`
- **THEN** the migration SHALL proceed and return objects_migrated, objects_failed, per-object details
- **THEN** `schema_version` SHALL be updated on all successfully migrated objects

#### Scenario: Execute migration blocked by dangerous risk level without force
- **WHEN** any object has `risk_level: "dangerous"` or `"risky"` with dropped fields
- **WHEN** `force` is false
- **THEN** the endpoint SHALL return 409 with `"migration_blocked"` and the list of blocking objects
- **THEN** no data SHALL be modified

#### Scenario: Execute migration with force flag overrides risk gate
- **WHEN** `force: true` is set
- **THEN** the migration SHALL proceed for all objects regardless of risk level
- **THEN** dropped property data SHALL be archived in `kb.graph_objects.migration_archive`

#### Scenario: Declared removed_properties suppress warnings
- **WHEN** a property is listed in the schema's `migrations.removed_properties`
- **WHEN** that property is dropped during migration
- **THEN** no warning issue SHALL be emitted for that property in the migration result
- **THEN** the property data SHALL still be archived for rollback

#### Scenario: Execute migration exceeds max_objects limit
- **WHEN** the number of affected objects exceeds `max_objects` (default 10,000)
- **THEN** the endpoint SHALL return 400 with "object count exceeds limit"

#### Scenario: Migration run recorded in schema_migration_runs
- **WHEN** a migration executes successfully
- **THEN** a record SHALL be written to `kb.schema_migration_runs` with start time, end time, objects migrated, objects failed

### Requirement: Migration rollback restores property data and optionally the type registry
The system SHALL expose `POST /api/schemas/projects/:projectId/migrate/rollback` that restores dropped property data from `migration_archive`. When `restore_type_registry: true`, the rollback SHALL also restore the type registry to the prior state.

#### Scenario: Rollback to a previously archived version (data only)
- **WHEN** objects have archive entries for the requested `to_version`
- **WHEN** `restore_type_registry` is false or omitted
- **THEN** the rollback SHALL restore the `dropped_data` from the archive onto each object's `properties`
- **THEN** the archive entry SHALL be removed from `migration_archive`
- **THEN** `schema_version` SHALL be updated back to the archived `from_version`

#### Scenario: Full rollback with type registry restoration
- **WHEN** `restore_type_registry: true` is set
- **THEN** the property data restore AND the type registry restore SHALL execute atomically in a single transaction
- **THEN** the `from_version` schema's types SHALL be re-installed in the registry
- **THEN** any type registry entries unique to the `to_version` schema SHALL be removed
- **THEN** if any step fails, the entire rollback SHALL be rolled back atomically

#### Scenario: Rollback attempted with no archive
- **WHEN** objects have no `migration_archive` entries for the requested `to_version`
- **THEN** the endpoint SHALL return 404 with "no migration archive found for version `<to_version>`"

### Requirement: Migration archive commit operation prunes old archive entries
The system SHALL expose `POST /api/schemas/projects/:projectId/migrate/commit` that removes all `migration_archive` entries up to and including a given version from all objects in the project. After commit, rollback to those versions is no longer possible.

#### Scenario: Commit prunes archive entries through specified version
- **WHEN** a user calls commit with `through_version: "1.1.0"`
- **THEN** all `migration_archive` entries where `to_version <= "1.1.0"` SHALL be removed from all project objects
- **THEN** the response SHALL include the count of objects modified and entries removed

#### Scenario: Commit with no archive entries to prune
- **WHEN** no objects have archive entries at or below the specified version
- **THEN** the endpoint SHALL return a success response with `objects_modified: 0`

### Requirement: Async migration job status endpoint
The system SHALL expose `GET /api/schemas/projects/:projectId/migration-jobs/:jobId` for polling the status of an async migration job.

#### Scenario: Poll job status while running
- **WHEN** a migration job is in progress
- **THEN** the endpoint SHALL return `status: "running"`, current `objects_migrated`, `objects_failed`, and the hop being processed

#### Scenario: Poll job status after completion
- **WHEN** a migration job has completed
- **THEN** the endpoint SHALL return `status: "completed"` with final `objects_migrated`, `objects_failed`, and per-hop results

#### Scenario: Poll job status after failure
- **WHEN** a migration job has failed
- **THEN** the endpoint SHALL return `status: "failed"` with `error` message and the last completed hop

### Requirement: Duplicate migration job prevention
The system SHALL not enqueue a new migration job if an identical job (same project, from_schema_id, to_schema_id) is already pending or running.

#### Scenario: Duplicate assign while job is pending
- **WHEN** a migration job for `(projectID, v1.0.0, v1.1.0)` is already pending
- **WHEN** another assign request triggers the same migration
- **THEN** the existing job ID SHALL be returned instead of creating a new job

### Requirement: MCP tools for migration operations
The system SHALL expose five MCP tools: `schema-migrate-preview`, `schema-migrate-execute`, `schema-migrate-rollback`, `schema-migrate-commit`, `schema-migration-job-status`.

#### Scenario: Agent previews schema migration via MCP
- **WHEN** an agent calls `schema-migrate-preview` with `project_id`, `from_schema_id`, `to_schema_id`
- **THEN** the tool SHALL return the risk assessment in MCP tool result format

#### Scenario: Agent monitors async migration job via MCP
- **WHEN** an agent calls `schema-migration-job-status` with `project_id`, `job_id`
- **THEN** the tool SHALL return current status, progress, and any errors

### Requirement: CLI subcommands for migration operations
The system SHALL extend `memory schemas migrate` with `preview`, `execute`, `rollback`, `commit`, and `job` subcommands.

#### Scenario: CLI migration preview shows suggested migrations block
- **WHEN** a user runs `memory schemas migrate preview --project <id> --from <schema-id> --to <schema-id>`
- **THEN** the CLI SHALL print a per-type risk table AND a suggested `migrations` YAML block the user can paste into their schema file

#### Scenario: CLI assign streams migration job progress on TTY
- **WHEN** a user runs `memory schemas assign` interactively (TTY)
- **WHEN** a migration job is enqueued
- **THEN** the CLI SHALL automatically stream job progress until completion

#### Scenario: CLI assign returns job ID on non-TTY
- **WHEN** `memory schemas assign` output is piped
- **WHEN** a migration job is enqueued
- **THEN** the CLI SHALL print the job ID and exit without polling

### Requirement: Property-level diff in schemas diff command with suggested migrations block
The `memory schemas diff` command SHALL report property-level changes per type, and at the end print a suggested `migrations` YAML block auto-populated from the diff.

#### Scenario: Diff shows added and removed properties within a type
- **WHEN** a type exists in both versions but has different properties
- **THEN** the CLI SHALL print per-type property diffs: added, removed, and type-changed properties

#### Scenario: Diff outputs suggested migrations block
- **WHEN** any property-level differences are detected
- **THEN** the CLI SHALL print a suggested `migrations` block in YAML with `removed_properties` pre-filled from the diff
