## Context

The schemas domain has two disconnected migration systems:

- **System A** (`apps/server/domain/graph/migration.go`): A full `SchemaMigrator` that diffs schemas field-by-field, archives dropped properties to `migration_archive` JSONB, assigns risk levels (safe/cautious/risky/dangerous), and supports rollback. Currently accessible only via an orphaned standalone binary (`apps/server/cmd/migrate-schema/main.go`) — no REST, MCP, or CLI.
- **System B** (`apps/server/domain/schemas/repository.go` `MigrateTypes`): A bare SQL path that renames `type_name` and JSONB property keys. Has no risk assessment, no archiving, and no rollback. Exposed via `POST /api/schemas/projects/:projectId/migrate` and `memory schemas migrate` CLI.

The schema definition format (`kb.graph_schemas`) carries no migration metadata. There is no machine-readable link between `v1.0.0` and `v1.1.0` of a schema — upgrade is a fully manual 5-step process.

The `SchemaMigrator` is already well-built and only needs to be connected to the service layer. Its operations work per-object: it iterates objects, applies the field diff, archives dropped data, and updates `schema_version`.

## Goals / Non-Goals

**Goals:**
- Add an optional `migrations` block to the schema definition format so upgrade paths are self-describing
- Validate the `migrations` block at publish time against the schema's own type list
- When a schema with migration hints is assigned, auto-trigger migration as an **async background job** if `from_version` is installed; return a job ID immediately
- Support **multi-hop migration chaining**: if `v1.0.0` is installed and the chain `v1.0.0 → v1.1.0 → v1.2.0` can be resolved from registry, execute all hops in sequence
- Promote `SchemaMigrator` (System A) into the REST/MCP/CLI surface with preview, execute, and rollback operations
- Feed `migrations` hints (type_renames, property_renames) from the schema definition into `SchemaMigrator` as rename mappings on assignment
- Extend `SchemaDiff` CLI command to report property-level changes (not just type names), and auto-suggest a `migrations` block
- Add a `migration_archive` **commit** operation that clears old archive entries once a migration is confirmed stable
- Integrate auto-migration preview into the `assign --dry-run` path
- Rollback restores both property data **and** the type registry to the prior state
- Validate `migrations.removed_properties` as explicit intent markers (suppress warnings for declared removals)

**Non-Goals:**
- Removing System B (`MigrateTypes`) — it remains as-is; System A operates alongside it for now
- Automatic migration on push to schema registry (CI/CD integration)
- Cross-project migrations

## Decisions

### 1. `migrations` block stored in `kb.graph_schemas.migrations` JSONB column

**Decision**: Add a nullable `migrations` JSONB column to `kb.graph_schemas`. Parsed into a new `SchemaMigrationHints` struct in Go.

**Rationale**: The migration hints belong to the schema version — they describe how to get from `from_version` to this version. Storing alongside the schema definition (not as a separate table) keeps the upgrade path self-contained when a schema file is shared or exported. A nullable column keeps backward compatibility — existing schemas have `NULL` migrations and no auto-migration triggers.

**Alternative considered**: A separate `kb.schema_migration_plans` table. Rejected because it separates the upgrade path from the schema artifact and complicates export/import workflows.

**Schema of `SchemaMigrationHints`**:
```go
type SchemaMigrationHints struct {
    FromVersion       string            `json:"from_version"`
    TypeRenames       []TypeRename      `json:"type_renames,omitempty"`
    PropertyRenames   []PropertyRename  `json:"property_renames,omitempty"`
    RemovedProperties []RemovedProperty `json:"removed_properties,omitempty"`
}

type RemovedProperty struct {
    TypeName string `json:"type_name"`
    Name     string `json:"name"`
}
```

`RemovedProperties` serves as **explicit intent markers**: when a property listed here is dropped during migration, the `SchemaMigrator` archives it but does NOT emit a warning issue. This suppresses noise for deliberate removals while still archiving data for rollback.

**`CreatePackRequest` and `UpdatePackRequest`** get a corresponding `Migrations *SchemaMigrationHints` field.

**Validation at publish time**: When a schema with a `migrations` block is created or updated, the server validates:
1. `from_version` is present
2. All type names in `type_renames.from`, `property_renames.type_name`, and `removed_properties.type_name` reference types that exist in the schema's `object_type_schemas` or `relationship_type_schemas` (after applying renames)
3. All property names in `property_renames.from` and `removed_properties.name` exist in the referenced type

Returns 400 with a list of validation errors if any check fails.

### 2. Auto-migration is asynchronous — returns a job ID

**Decision**: When `assign` triggers auto-migration, the migration runs as a **background job** (same scheduler infrastructure used by extraction jobs). The `assign` response returns immediately with:
- The assignment ID
- A `migration_job_id` (if auto-migration was triggered)
- `migration_status: "pending"` | `"skipped"` (with reason)

The client can poll `GET /api/schemas/projects/:projectId/migration-jobs/:jobId` for status, or use the existing job monitoring UI.

**Flow**:
1. `AssignPackWithTypes` creates the assignment row and registers types synchronously
2. If `schema.Migrations != nil` and `from_version` is detected:
   a. Resolve the migration chain (see Decision 3)
   b. Enqueue a `SchemaMigrationJob` via the scheduler
   c. Return job ID in the response
3. The job worker runs `ExecuteSchemaMigration` for each hop in sequence
4. On completion, writes to `kb.schema_migration_runs` and optionally uninstalls `from_version`

**Risk gate in async context**: The preview is run synchronously as part of the enqueue step. If the overall risk is `dangerous` and `force` is not set, the job is NOT enqueued and `migration_status: "blocked"` is returned with `block_reason`. The assignment itself still succeeds.

**Rationale**: Synchronous migration on `assign` would block HTTP responses for minutes on large datasets. Async jobs match the existing pattern for long-running operations in the codebase and give users visibility into progress. The preview-before-enqueue step preserves the safety guarantees.

**`assign --dry-run` integration**: When `dry_run: true` is set on an assign request AND the schema has migration hints, the response includes a `migration_preview` field with the full risk assessment (same output as `POST /migrate/preview`) — no job is enqueued, no data is changed.

### 3. Multi-hop migration chain resolution

**Decision**: When assigning `v1.2.0` and only `v1.0.0` is installed, the server resolves a migration chain by walking the registry backwards:
1. Start from `to_version` = `v1.2.0`, read its `migrations.from_version` = `v1.1.0`
2. Check if `v1.1.0` is installed. If not, look it up in the registry by `(schema_name, version)`
3. Read `v1.1.0`'s `migrations.from_version` = `v1.0.0`
4. `v1.0.0` is installed → chain resolved: `[v1.1.0, v1.2.0]`

The chain is capped at **10 hops** to prevent infinite loops. If the chain cannot be resolved (a hop is missing from the registry entirely), the assign returns `migration_status: "chain_unresolvable"` with a message describing the gap (e.g., "v1.1.0 not found in registry — publish it first").

Each hop's migration job runs sequentially within the same background job. Type renames from hop N are applied before hop N+1 starts, so rename mappings compose correctly.

**Intermediate versions do not need to be installed** on the project — only present in the registry. The chain walk uses the registry, not the project's installed list.

### 4. New service methods wrapping SchemaMigrator

**Decision**: Add to `schemas/service.go`:
- `PreviewSchemaMigration(ctx, projectID, fromSchemaID, toSchemaID, hints) → *SchemaMigrationPreviewResponse`
- `ExecuteSchemaMigration(ctx, projectID, fromSchemaID, toSchemaID, hints, force, maxObjects) → *SchemaMigrationExecuteResponse`
- `RollbackSchemaMigration(ctx, projectID, toVersion, restoreTypeRegistry bool) → *SchemaMigrationRollbackResponse`
- `CommitMigrationArchive(ctx, projectID, throughVersion string) → *CommitArchiveResponse`
- `ResolveMigrationChain(ctx, projectID, toSchemaID) → ([]MigrationHop, error)`

These wrap the `graph.SchemaMigrator` and delegate DB reads/writes through the `graph` store.

**Rationale**: Keeps `SchemaMigrator` in the `graph` domain (it operates on graph objects) while the schemas service orchestrates the schema-level concerns.

### 5. Rollback restores the type registry

**Decision**: `RollbackSchemaMigration` accepts a `restore_type_registry bool` flag. When true:
1. Restore property data from `migration_archive` (existing behavior)
2. Re-install the `from_version` schema types into the registry (call `AssignPackWithTypes` with the old schema ID, skip migration hints)
3. Remove the `to_version` schema's type registry entries that didn't exist in `from_version`
4. Update `schema_version` on objects back to `from_version`

When `false`, only property data is restored (existing behavior, cheaper).

**Rationale**: A rollback that only restores data but leaves the type registry in the new state is inconsistent — objects have old property shapes but are typed against new schemas. Full rollback requires both layers. The flag lets users choose the scope.

### 6. New HTTP routes under `/api/schemas/projects/:projectId/`

- `POST /migrate/preview` — risk assessment, no data changes
- `POST /migrate/execute` — full migration with optional `force`
- `POST /migrate/rollback` — restore from `migration_archive` (optionally restore type registry)
- `POST /migrate/commit` — prune `migration_archive` entries through a given version
- `GET /migration-jobs/:jobId` — poll async migration job status

Existing `POST /migrate` (System B) is **unchanged** to avoid breaking existing callers.

### 7. `migration_archive` commit operation

**Decision**: Add `POST /migrate/commit` with body `{ "through_version": "1.1.0" }`. This removes all `migration_archive` entries whose `to_version` is `<= through_version` from all objects in the project. Once committed, rollback to those versions is no longer possible.

This is an explicit user action — nothing auto-prunes. The recommended workflow: run `commit` after a migration has been stable in production for a few days.

**Rationale**: Without a commit operation, `migration_archive` grows unboundedly. Requiring an explicit commit gives users control over when they sacrifice rollback ability.

### 8. Property-level `diff` with suggested `migrations` block

**Decision**: Extend `memory schemas diff` to:
1. Report added/removed/type-changed properties per type (not just type-name level)
2. At the end of the diff output, print a suggested `migrations` block in YAML that the user can paste into their schema file

The suggested block is computed from the diff: removed types become nothing (no rename hint), renamed types can't be auto-detected (user must fill in), removed properties are listed under `removed_properties`, type-changed properties are listed as warnings.

**Rationale**: Closes the feedback loop — diff output directly tells the user what to put in the schema file.

### 9. `SchemaMigrator` needs a `graph.Store` reference for bulk object reads/writes

**Decision**: The `SchemaMigrator` stays stateless (takes objects as input). A new `SchemaMigrationOrchestrator` in `schemas/service.go` handles:
1. Fetching all objects of affected types from `graph.Store`
2. Calling `SchemaMigrator.MigrateObject` per object
3. Batch-writing updated objects back via `graph.Store`
4. Writing a `kb.schema_migration_runs` record on completion

**Rationale**: Avoids coupling `SchemaMigrator` to a DB dependency and keeps it unit-testable.

## Risks / Trade-offs

- **Async job visibility** → Users must poll or check job status separately after `assign`. The response includes `migration_job_id` to make this easy, but it's a UX step that didn't exist before.  
  → Mitigation: CLI `assign` command will poll and stream progress automatically if run interactively.

- **Multi-hop chain with missing intermediate version** → If `v1.1.0` is not in the registry, the chain cannot be resolved. The assign still succeeds but migration is blocked.  
  → Mitigation: Return a clear error message naming exactly which version is missing and where to get it.

- **`migration_archive` growth** → Each migration appends to the JSONB array per object. Without a commit, this grows across every migration.  
  → Mitigation: Explicit `commit` endpoint gives users control. Document the recommended workflow.

- **Full rollback complexity** → Restoring the type registry during rollback touches multiple tables and could fail partway through. Must be wrapped in a transaction.  
  → Mitigation: Wrap entire rollback (data + registry) in a single DB transaction. If any step fails, the whole rollback rolls back atomically.

- **Race condition on async enqueue** → Two simultaneous `assign` calls for the same schema could enqueue two migration jobs.  
  → Mitigation: Check for an existing in-progress migration job for the same `(projectID, fromSchemaID, toSchemaID)` before enqueuing. Return the existing job ID if found.

- **Validation of `migrations` block at publish time** → Validating property names requires parsing `object_type_schemas` JSONB, which adds latency to the create/update schema endpoint.  
  → Mitigation: Validation is O(small) — schemas are small JSONB blobs. Acceptable latency. Can be made async/soft-warn in a future iteration if needed.

## Migration Plan

1. Add Goose migration: `ALTER TABLE kb.graph_schemas ADD COLUMN migrations JSONB`
2. Add Goose migration: `CREATE TABLE kb.schema_migration_jobs (...)` for async job tracking
3. Update entity, request/response structs — fully backward-compatible (nullable column)
4. Wire `SchemaMigrator` into service layer
5. Add new HTTP routes, MCP tools, CLI subcommands
6. Extend `assign` to enqueue async migration job when hints present
7. No downtime required; no breaking changes to existing API surface

## Open Questions

- Should `memory schemas assign` in CLI block and stream job progress by default, or return immediately with job ID? Leaning toward: block + stream for interactive TTY, return job ID for non-TTY (piped output).
- Should the chain resolution also look up schemas from the public registry (not just the project/org registry)?
