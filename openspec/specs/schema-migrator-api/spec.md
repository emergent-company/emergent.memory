# schema-migrator-api Specification

## Purpose
Defines the SchemaMigrator API surface: migration preview, execute, rollback, and archive commit with risk gates and archive handling, async job status, duplicate-job prevention, MCP tools and CLI subcommands, batch operations that bound object scans and stream page-by-page, and the migration archive append, restore, and consume contract.

## Requirements

### Requirement: Rollback consumes only the matched archive entry and preserves newer entries
The rollback operation SHALL remove only the archive entry it matches for the requested `to_version`. Newer archive entries SHALL remain intact, so a multi-hop history stays independently rollback-able.

#### Scenario: Out-of-order rollback preserves newer archive entries
- **GIVEN** an object with archive entries for `to_version: "2.0.0"` and `to_version: "3.0.0"`
- **WHEN** rollback is requested with `to_version: "2.0.0"`
- **THEN** the `to_version: "2.0.0"` entry SHALL be removed from `migration_archive`
- **THEN** the `to_version: "3.0.0"` entry SHALL remain, including its `dropped_data`

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

The `to_version` field SHALL name the target version of the migration being undone — it MUST match a migration archive's `to_version` (the version the objects were migrated *to*), and does NOT name the version to roll back to. The archive's `from_version` determines the version restored to. When `restore_type_registry: true` is requested and no migration matching `to_version` can be resolved, the endpoint SHALL fail with a clear 404 error instead of silently no-oping.

#### Scenario: Rollback to a previously archived version (data only)
- **WHEN** objects have archive entries for the requested `to_version` (the migration's target version)
- **WHEN** `restore_type_registry` is false or omitted
- **THEN** the rollback SHALL restore the `dropped_data` from the archive onto each object's `properties`
- **THEN** the archive entry SHALL be removed from `migration_archive`
- **THEN** `schema_version` SHALL be updated back to the archived `from_version`

#### Scenario: Full rollback with type registry restoration
- **WHEN** `restore_type_registry: true` is set
- **THEN** the property data restore AND the type registry restore SHALL execute atomically in a single transaction
- **THEN** the `from_version` schema's types SHALL be re-installed in the registry
- **THEN** registry rows owned by the from/to schema pair whose type name is not part of the from_version schema SHALL be removed — including rows left behind by an in-place type rename that changed `type_name` but kept the row's `schema_id`
- **THEN** registry rows owned by other schemas SHALL NOT be modified or removed
- **THEN** if any step fails, the entire rollback SHALL be rolled back atomically

#### Scenario: Rollback attempted with no resolvable migration
- **WHEN** `restore_type_registry: true` is requested
- **WHEN** no migration archive/migration matching the requested `to_version` can be resolved
- **THEN** the endpoint SHALL return a clear 404 error naming `to_version`

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

### Requirement: Batch migration operations load the migration archive column
The system SHALL load `kb.graph_objects.migration_archive` whenever it lists objects for a schema migrate or rollback operation. The archive column SHALL remain excluded from the default graph list/search projection so archive JSON is not exposed by those endpoints.

#### Scenario: Rollback lists objects with their archive
- **WHEN** `RollbackSchemaMigration` lists project objects
- **THEN** each returned object SHALL carry its persisted `migration_archive` entries
- **THEN** objects with at least one archive entry for the requested `to_version` SHALL NOT be skipped for an empty archive slice

#### Scenario: Forward migration lists objects with their archive
- **WHEN** `ExecuteSchemaMigration` lists objects of an affected type
- **THEN** each returned object SHALL carry its persisted `migration_archive` entries
- **THEN** `SchemaMigrator.MigrateObject` SHALL append the new archive entry to the existing entries

#### Scenario: Public list/search projection is unchanged
- **WHEN** a caller lists or searches graph objects without requesting the archive
- **THEN** the `migration_archive` column SHALL NOT be selected
- **THEN** the response SHALL NOT include archive JSON

### Requirement: Rollback restores archived property data and consumes the entry
The rollback operation SHALL restore the `dropped_data` of the matching archive entry onto the object's properties, update `schema_version` to the archive entry's `from_version`, and remove the consumed entry from `migration_archive`.

#### Scenario: Rollback restores the dropped property
- **GIVEN** an object whose property was archived by a migration to version `X`
- **WHEN** rollback is requested with `to_version: "X"`
- **THEN** the archived property SHALL be restored to the object's `properties`
- **THEN** `schema_version` SHALL be set to the archive entry's `from_version`
- **THEN** the response `objects_restored` SHALL count the object

#### Scenario: Rollback consumes the archive entry
- **GIVEN** an object with a single archive entry for version `X`
- **WHEN** rollback is requested with `to_version: "X"`
- **THEN** that archive entry SHALL be removed from `migration_archive`
- **THEN** a subsequent rollback to the same version SHALL be a no-op for that object

### Requirement: Multi-hop migrations append to the archive without clobbering earlier entries
Each successive migration hop SHALL append its archive entry to any entries already present on the object. Earlier hops' entries SHALL remain intact and independently rollback-able.

#### Scenario: Second hop preserves the first hop's archive entry
- **GIVEN** an object migrated from `1.0.0` to `2.0.0`, archiving property `alpha`
- **WHEN** the object is migrated from `2.0.0` to `3.0.0`, archiving property `beta`
- **THEN** `migration_archive` SHALL contain both the `to_version: "2.0.0"` and `to_version: "3.0.0"` entries
- **THEN** rolling back to `3.0.0` SHALL restore `beta` and leave the `2.0.0` entry in place
- **THEN** rolling back to `2.0.0` SHALL restore `alpha`

### Requirement: Batch migration operations visit every matching object
The migrate and rollback operations SHALL visit every matching object in the project, not only the first page. When a project holds more objects than a single list page, the operations SHALL page through the full result set.

#### Scenario: Migration covers objects beyond the first page
- **GIVEN** a project with more objects of an affected type than the configured maximum list page size
- **WHEN** a migration executes
- **THEN** every affected object SHALL be migrated and counted in `objects_migrated`

#### Scenario: Rollback covers objects beyond the first page
- **GIVEN** a project with more archived objects than the configured maximum list page size
- **WHEN** a rollback executes
- **THEN** every archived object SHALL be restored and counted in `objects_restored`

### Requirement: Rollback bounds its object scan to archive-carrying objects
The rollback operation SHALL restrict its object scan to objects that carry at least one `migration_archive` entry (`migration_archive <> '[]'::jsonb`), rather than fetching every object in the project. The scan SHALL still visit every archive-carrying object, including across multiple list pages.

#### Scenario: Rollback does not full-scan an archive-free project
- **GIVEN** a project whose objects carry no `migration_archive` entries
- **WHEN** a rollback executes
- **THEN** the object scan SHALL NOT fetch every object in the project
- **THEN** the rollback SHALL report zero objects restored

#### Scenario: Rollback restores only matching archived objects
- **GIVEN** a project containing one object with an archive entry targeting the rollback version and one archive-free object
- **WHEN** a rollback executes
- **THEN** the archive-carrying object SHALL be restored
- **THEN** the archive-free object SHALL NOT be restored

#### Scenario: Rollback completeness is preserved across pages
- **GIVEN** a project with more archive-carrying objects than one list page
- **WHEN** a rollback executes with the archive predicate
- **THEN** every archive-carrying object SHALL be visited across pages

### Requirement: Forward migration still visits archive-free objects
The forward migration operation SHALL NOT filter its object scan by the presence of a `migration_archive` entry. Objects with an empty archive SHALL still be migrated and counted, because a first-ever migration has zero archived objects and objects created after a prior migration may also have none.

#### Scenario: Migrate counts objects with an empty archive
- **GIVEN** a project whose objects carry no `migration_archive` entries
- **WHEN** a forward migration executes
- **THEN** every object SHALL be migrated and counted

### Requirement: Migrate and rollback stream results page-by-page
Migrate and rollback SHALL iterate the affected object set one list page at a time rather than materialising the full result set in memory. A caller-supplied page callback SHALL be invoked once per page, and the iteration SHALL stop as soon as the callback returns an error without fetching further pages.

#### Scenario: Full set is not materialised
- **GIVEN** a result set larger than one list page
- **WHEN** a migrate or rollback executes
- **THEN** objects SHALL be processed one page at a time
- **THEN** the whole result set SHALL NOT be accumulated in memory before processing

### Requirement: Synchronous request path is bounded by a hard cap
The synchronous migrate and rollback request path SHALL be bounded by a configurable hard cap on the number of objects scanned. When a scan would exceed the cap, the operation SHALL abort with a clear 4xx error stating the cap and that the operation must be narrowed.

#### Scenario: Hard cap aborts with a clear error
- **GIVEN** a configured hard cap of N scanned objects
- **WHEN** a migrate or rollback scans more than N objects
- **THEN** the operation SHALL abort with a 4xx error naming the cap

#### Scenario: Rollback hard-cap abort is atomic
- **GIVEN** a rollback that exceeds the hard cap mid-scan
- **WHEN** the rollback aborts
- **THEN** the single transaction SHALL roll back so no objects are restored and no registry changes are written

#### Scenario: Execute hard-cap abort is not transactional
- **GIVEN** a forward migration that exceeds the hard cap mid-scan
- **WHEN** the migration aborts
- **THEN** the current type MAY be left partially migrated
- **THEN** the `kb.schema_migration_runs` run row SHALL NOT be written

### Requirement: Rollback rejects max_objects combined with restore_type_registry
The rollback operation SHALL reject a request that combines `max_objects` with `restore_type_registry`, because the registry restore is all-or-nothing and cannot be skipped when the `max_objects` cap is reached mid-scan.

#### Scenario: Combined request fails loudly
- **GIVEN** a rollback request with `restore_type_registry: true` and `max_objects: N`
- **WHEN** the rollback executes
- **THEN** the request SHALL fail with a 4xx bad-request error
- **THEN** no objects SHALL be restored and no registry changes SHALL be written
