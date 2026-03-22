## 1. Database Migrations

- [x] 1.1 Write Goose migration: `ALTER TABLE kb.graph_schemas ADD COLUMN IF NOT EXISTS migrations JSONB`
- [x] 1.2 Write Goose migration: create `kb.schema_migration_jobs` table (`id`, `project_id`, `from_schema_id`, `to_schema_id`, `chain` JSONB, `status`, `risk_level`, `objects_migrated`, `objects_failed`, `error`, `created_at`, `started_at`, `completed_at`)
- [x] 1.3 Verify both migrations run cleanly against local Postgres

## 2. Entity and Request/Response Structs

- [x] 2.1 Add `SchemaMigrationHints` struct to `entity.go` with `FromVersion`, `TypeRenames`, `PropertyRenames`, `RemovedProperties` fields and YAML tags
- [x] 2.2 Add `RemovedProperty` struct (`TypeName`, `Name`) to `entity.go`
- [x] 2.3 Add `Migrations *SchemaMigrationHints` field to `GraphMemorySchema` entity with `bun:"migrations,type:jsonb"` tag
- [x] 2.4 Add `Migrations *SchemaMigrationHints` field to `CreatePackRequest` and `UpdatePackRequest`
- [x] 2.5 Add `Force bool` and `AutoUninstall bool` fields to `AssignPackRequest`
- [x] 2.6 Add `MigrationJobID *string`, `MigrationStatus string`, `MigrationBlockReason string`, `MigrationPreview *SchemaMigrationPreviewResponse` fields to `AssignPackResult`
- [x] 2.7 Define `SchemaMigrationJob` Bun entity mapping `kb.schema_migration_jobs`
- [x] 2.8 Define `MigrationHop` struct (`FromSchemaID`, `ToSchemaID`, `Hints *SchemaMigrationHints`) for chain resolution
- [x] 2.9 Define `SchemaMigrationPreviewRequest/Response`, `SchemaMigrationExecuteRequest/Response`, `SchemaMigrationRollbackRequest/Response`, `CommitMigrationArchiveRequest/Response` structs
- [x] 2.10 Add `RestoreTypeRegistry bool` field to `SchemaMigrationRollbackRequest`

## 3. Migration Hints Validation at Publish Time

- [x] 3.1 Implement `validateMigrationHints(hints *SchemaMigrationHints, objectTypeSchemas, relTypeSchemas json.RawMessage) []string` — returns list of validation errors
- [x] 3.2 Validate `from_version` is non-empty when `migrations` block is present
- [x] 3.3 Validate all `type_renames.from`, `property_renames.type_name`, `removed_properties.type_name` reference types that exist in the schema
- [x] 3.4 Validate all `property_renames.from` and `removed_properties.name` reference properties that exist in the referenced type
- [x] 3.5 Call `validateMigrationHints` in `CreatePack` and `UpdatePack` service methods; return 400 with errors list if invalid

## 4. Repository Changes

- [x] 4.1 Update `CreatePack` and `UpdatePack` in `repository.go` to persist `Migrations` JSONB column
- [x] 4.2 Ensure `GetPack` and list queries select the `migrations` column
- [x] 4.3 Add `GetInstalledSchemasByName(ctx, projectID, schemaName) ([]GraphMemorySchema, error)` to repository
- [x] 4.4 Add `CreateMigrationJob(ctx, job *SchemaMigrationJob) error` to repository
- [x] 4.5 Add `GetMigrationJob(ctx, jobID string) (*SchemaMigrationJob, error)` to repository
- [x] 4.6 Add `UpdateMigrationJob(ctx, job *SchemaMigrationJob) error` to repository
- [x] 4.7 Add `FindActiveMigrationJob(ctx, projectID, fromSchemaID, toSchemaID string) (*SchemaMigrationJob, error)` for dedup check

## 5. Migration Chain Resolution

- [x] 5.1 Implement `ResolveMigrationChain(ctx, projectID, toSchemaID string) ([]MigrationHop, error)` in `service.go`
- [x] 5.2 Walk backwards from `toSchema.Migrations.FromVersion`, looking up each intermediate schema in the registry by `(name, version)`
- [x] 5.3 Stop when the installed version is found or when `migrations` is nil (no further chain)
- [x] 5.4 Cap chain at 10 hops; return error if exceeded
- [x] 5.5 Return descriptive error naming the missing version if a hop cannot be resolved from the registry

## 6. SchemaMigrationOrchestrator Service Methods

- [x] 6.1 Add `graph.Service` (or `graph.Store`) dependency to `schemas.Service` via fx for object read/write access
- [x] 6.2 Implement `PreviewSchemaMigration(ctx, projectID, fromSchemaID, toSchemaID, hints) (*SchemaMigrationPreviewResponse, error)` — fetch objects, run `SchemaMigrator.MigrateObject` per object (dry), return aggregated results with `overall_risk_level`
- [x] 6.3 Implement `ExecuteSchemaMigration(ctx, projectID, fromSchemaID, toSchemaID, hints, force, maxObjects) (*SchemaMigrationExecuteResponse, error)` — run rename SQL, then `SchemaMigrator.MigrateObject` (using `RemovedProperties` hints to suppress warnings for declared removals), batch-write updated objects, update `schema_version`, write `kb.schema_migration_runs` record
- [x] 6.4 Implement `RollbackSchemaMigration(ctx, projectID, toVersion string, restoreTypeRegistry bool) (*SchemaMigrationRollbackResponse, error)` — restore property data; if `restoreTypeRegistry`, re-install old schema types and remove new schema's type additions, all in a single DB transaction
- [x] 6.5 Implement `CommitMigrationArchive(ctx, projectID, throughVersion string) (*CommitArchiveResponse, error)` — strip `migration_archive` entries whose `to_version <= throughVersion` from all project objects
- [x] 6.6 Implement `runAutoMigrationAsync(ctx, projectID, chain []MigrationHop, force, autoUninstall bool) (*SchemaMigrationJob, error)` — runs preview, checks risk gate, deduplicates, enqueues job

## 7. Async Migration Job Worker

- [x] 7.1 Create `SchemaMigrationJobWorker` struct in `apps/server/domain/schemas/` (or `jobs/`) that polls `kb.schema_migration_jobs` for pending jobs
- [x] 7.2 Worker executes each hop in the chain sequentially by calling `ExecuteSchemaMigration`
- [x] 7.3 Worker updates job `status` (`pending → running → completed/failed`) and `objects_migrated`, `objects_failed`, `error` fields
- [x] 7.4 Register the worker with the fx scheduler (same pattern as extraction workers)
- [x] 7.5 Handle `auto_uninstall` at job completion: if set and all hops succeeded, uninstall `from_version` schema

## 8. Auto-Migration Enqueue in AssignPack

- [x] 8.1 After successful assignment in `AssignPackWithTypes`, call `ResolveMigrationChain` if schema has `Migrations != nil`
- [x] 8.2 If chain resolved: run `PreviewSchemaMigration` synchronously for the first hop to assess risk
- [x] 8.3 If risk is `dangerous` and `force` is false: set `MigrationStatus="blocked"` in response, do not enqueue
- [x] 8.4 If chain unresolvable: set `MigrationStatus="chain_unresolvable"` with reason, do not enqueue
- [x] 8.5 Otherwise: call `runAutoMigrationAsync`, attach job ID to `AssignPackResult`
- [x] 8.6 If `dry_run: true` on assign request: run `PreviewSchemaMigration` and attach results to `AssignPackResult.MigrationPreview`, do not enqueue

## 9. HTTP Handlers and Routes

- [x] 9.1 Add `POST /api/schemas/projects/:projectId/migrate/preview` handler
- [x] 9.2 Add `POST /api/schemas/projects/:projectId/migrate/execute` handler
- [x] 9.3 Add `POST /api/schemas/projects/:projectId/migrate/rollback` handler
- [x] 9.4 Add `POST /api/schemas/projects/:projectId/migrate/commit` handler
- [x] 9.5 Add `GET /api/schemas/projects/:projectId/migration-jobs/:jobId` handler
- [x] 9.6 Register all new routes in `routes.go`
- [x] 9.7 Verify existing `POST /api/schemas/projects/:projectId/migrate` (System B) still routes correctly

## 10. MCP Tools

- [x] 10.1 Add `schema-migrate-preview` tool definition and implementation in `mcp/service.go`
- [x] 10.2 Add `schema-migrate-execute` tool definition and implementation
- [x] 10.3 Add `schema-migrate-rollback` tool definition and implementation
- [x] 10.4 Add `schema-migrate-commit` tool definition and implementation
- [x] 10.5 Add `schema-migration-job-status` tool definition and implementation
- [x] 10.6 Add dispatch cases for all 5 new tools in the MCP tool dispatch switch

## 11. CLI Subcommands

- [x] 11.1 Add `memory schemas migrate preview` subcommand with `--project`, `--from`, `--to` flags; output a table of type/risk/objects; at the end print the suggested `migrations` YAML block
- [x] 11.2 Add `memory schemas migrate execute` subcommand with `--force`, `--max-objects` flags; add confirmation prompt for risky/dangerous migrations
- [x] 11.3 Add `memory schemas migrate rollback` subcommand with `--project`, `--to-version`, `--restore-registry` flags
- [x] 11.4 Add `memory schemas migrate commit` subcommand with `--project`, `--through-version` flags
- [x] 11.5 Add `memory schemas migrate job` subcommand for polling job status with `--project`, `--job-id`, `--wait` (block until complete, streaming progress)
- [x] 11.6 Extend `memory schemas assign` to accept `--force` and `--auto-uninstall` flags
- [x] 11.7 In `memory schemas assign` CLI: after assign, if `migration_job_id` is returned and stdout is a TTY, automatically call job status polling with streaming progress output

## 12. Property-Level Diff with Suggested migrations Block

- [x] 12.1 Extend `schemasParseSchema` helper to parse `object_type_schemas` into typed maps per property
- [x] 12.2 In `schemasDiffCmd`, after type-name level diff, iterate shared type names and diff property sets (added/removed/type-changed)
- [x] 12.3 Print per-type property diffs: `  [Agreement] +signed_at (string), -legacyId, ~signDate→signed_at`
- [x] 12.4 At the end of diff output, print a suggested `migrations` YAML block auto-populated from the diff (removed properties → `removed_properties`, type-changed with same name → comment, etc.)

## 13. Schema Format (YAML/JSON) and CLI Push

- [x] 13.1 Update `memory schemas push` YAML/JSON parsing to read and forward the `migrations` block
- [x] 13.2 Add YAML tags to `SchemaMigrationHints` and nested structs for snake_case deserialization
- [x] 13.3 Update CLI schema template/example to show optional `migrations` block

## 14. Build Verification and Cleanup

- [x] 14.1 Run `go build ./apps/server/... && go build ./tools/cli/...` — must be clean
- [x] 14.2 Remove or deprecate `apps/server/cmd/migrate-schema/main.go` standalone binary
- [x] 14.3 Update `docs/site/developer-guide/schema.md` with async migration flow, chain resolution, commit operation, new endpoints
- [x] 14.4 Update `.agents/skills/memory-schemas/SKILL.md` with new MCP tools and CLI subcommands
