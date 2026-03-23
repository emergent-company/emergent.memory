# Tasks

## Feature 1: YAML support for schema files

- [x] Add `loadSchemaFile(path string) ([]byte, error)` helper in `tools/cli/internal/cmd/schemas.go` that reads a file, detects `.yaml`/`.yml` extension, converts YAML→JSON, and returns JSON bytes (error on unsupported extension or parse failure)
- [x] Update `schemas install --file` handler in `tools/cli/internal/cmd/schemas.go` to use `loadSchemaFile()` instead of `os.ReadFile` + raw unmarshal
- [x] Update `schemas create --file` handler in `tools/cli/internal/cmd/schemas.go` to use `loadSchemaFile()` instead of `os.ReadFile` + raw unmarshal

## Feature 2: `schemas diff` subcommand

- [x] Add `diff` subcommand to `tools/cli/internal/cmd/schemas.go` with `<schema-id> --file <path> [--output json]` flags
- [x] Implement diff logic: fetch installed schema via SDK `GetPack`, load incoming file via `loadSchemaFile()`, compare object type names and relationship type names, build added/removed/modified lists
- [x] Print human-readable diff output (default) and JSON output when `--output json`

## Feature 3: Bulk uninstall flags

- [x] Add `--all-except <ids>` and `--keep-latest` flags to the `uninstall` subcommand in `tools/cli/internal/cmd/schemas.go`
- [x] Add `--dry-run` flag to uninstall
- [x] Implement flag-conflict validation (mutually exclusive flags, positional arg + bulk flag conflict)
- [x] Implement bulk uninstall logic: fetch installed schemas, filter by flag, call `UninstallPack` for each (skip if dry-run), print results

## Feature 4: `schemas compiled-types --verbose`

- [x] Add `SchemaID` and `SchemaVersion` fields to `CompiledObjectType` and `CompiledRelationshipType` response structs in `apps/server/domain/schemas/entity.go`
- [x] Update `GetCompiledTypes` service method in `apps/server/domain/schemas/service.go` to populate `SchemaID`/`SchemaVersion` per type by joining against the installed schemas list
- [x] Update `GetCompiledTypes` service to detect shadowed types (same name from multiple schemas) and set a `Shadowed` bool field on the earlier-installed type
- [x] Add `--verbose` flag to `schemas compiled-types` in `tools/cli/internal/cmd/schemas.go`; when set, include `schemaId`, `schemaVersion`, `shadowed` in output and print stderr warnings for shadowed types

## Feature 5: `schemas history` subcommand

- [x] Write Goose migration `apps/server/migrations/<timestamp>_add_project_schemas_removed_at.sql` adding `removed_at TIMESTAMPTZ DEFAULT NULL` to `kb.project_schemas`
- [x] Update `DeleteAssignment` in `apps/server/domain/schemas/repository.go` to `UPDATE ... SET removed_at = NOW()` instead of `DELETE`
- [x] Update `GetInstalledPacks` query in `apps/server/domain/schemas/repository.go` to add `WHERE removed_at IS NULL`
- [x] Add `GetAssignmentHistory` method to `apps/server/domain/schemas/repository.go` returning all rows (no removed_at filter) with `removed_at` in results
- [x] Add `GetSchemaHistory` method to `apps/server/domain/schemas/service.go` calling the new repository method
- [x] Add `GET /api/schemas/projects/:projectId/history` route + handler in `apps/server/domain/schemas/handler.go` and `routes.go`
- [x] Add `GetPackHistory` method to `apps/server/pkg/sdk/schemas/client.go`
- [x] Add `history` subcommand to `tools/cli/internal/cmd/schemas.go` with `[--output json]` flag

## Feature 6: `schemas migrate` subcommand

- [x] Add `MigrateRequest` and `MigrateResponse` structs to `apps/server/domain/schemas/entity.go`
- [x] Add `MigrateTypes` method to `apps/server/domain/schemas/repository.go`: runs UPDATE on `kb.graph_objects.type_name`, property JSONB keys, and `kb.graph_edges.type_name` in a transaction; supports dry-run (rolls back)
- [x] Add `MigrateTypes` method to `apps/server/domain/schemas/service.go` calling the repository method
- [x] Add `POST /api/schemas/projects/:projectId/migrate` route + handler in `apps/server/domain/schemas/handler.go` and `routes.go`
- [x] Add `MigratePack` method to `apps/server/pkg/sdk/schemas/client.go`
- [x] Add `migrate` subcommand to `tools/cli/internal/cmd/schemas.go` with `--rename-type OldName:NewName` and `--rename-property OldType.old_key:OldType.new_key` (repeatable) and `--dry-run` flags; validate at least one rename provided; print results with zero-row warnings
