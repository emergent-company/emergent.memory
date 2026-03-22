## Why

Upgrading a schema from one version to another currently requires a manual 5-step process (create new schema, migrate data, uninstall old, install new, verify). There is no machine-readable link between schema versions, so the upgrade path is entirely tribal knowledge — nothing in the schema file or registry describes how to get from `v1.0.0` to `v1.1.0`. A richer migration engine already exists in the codebase (`apps/server/domain/graph/migration.go`) but is only reachable via a standalone binary with no REST, MCP, or CLI surface.

## What Changes

- Add an optional `migrations` block to the schema definition format (YAML/JSON) that declares: `from_version`, `type_renames`, `property_renames`, and `removed_properties`
- When assigning a schema version to a project, the server reads the `migrations` block, detects if the declared `from_version` is currently installed, and automatically executes the schema upgrade
- Promote `SchemaMigrator` (System A — `apps/server/domain/graph/migration.go`) from the orphaned standalone binary into the live service stack; expose it via REST API, MCP tool, and CLI
- The new `assign` flow feeds migration hints from the schema's `migrations` block directly into `SchemaMigrator` as rename mappings
- Add a `preview` mode: before executing, return a risk-assessed migration plan without touching data
- Remove/deprecate the System B `MigrateTypes` bare-SQL path once System A is wired in (or keep it as fallback for schemas with no hints)

## Capabilities

### New Capabilities

- `schema-migration-hints`: Schema definition format extended with a `migrations` block; `assign` auto-triggers migration when `from_version` is installed
- `schema-migrator-api`: `SchemaMigrator` engine wired into the REST/MCP/CLI surface — preview, execute, rollback migration operations with risk assessment and data archiving

### Modified Capabilities

- `configuration-management`: Schema assignment (`assign`) behavior changes — it now conditionally triggers auto-migration

## Impact

- **Schema format**: `GraphMemorySchema` entity gains optional `Migrations *SchemaMigrationHints` field
- **Database**: `kb.graph_schemas` gains a `migrations` JSONB column; existing rows default to `NULL` (no hints, no auto-migration)
- **`schemas/repository.go`**: `AssignPackWithTypes` extended to detect installed `from_version` and invoke migrator
- **`schemas/service.go`**: New `PreviewMigration`, `ExecuteMigration`, `RollbackMigration` service methods wrapping `SchemaMigrator`
- **`schemas/handler.go`**: New HTTP routes: `POST /migrate/preview`, `POST /migrate/execute`, `POST /migrate/rollback`
- **`mcp/service.go`**: New MCP tools: `schema-migrate-preview`, `schema-migrate-execute`, `schema-migrate-rollback`
- **`tools/cli/internal/cmd/schemas.go`**: `schemas assign` enhanced; new `schemas migrate preview/execute/rollback` subcommands
- **`apps/server/domain/graph/migration.go`**: `SchemaMigrator` promoted from internal package to shared service (no logic changes expected)
- **Migration**: New DB migration to add `migrations` JSONB column to `kb.graph_schemas`
