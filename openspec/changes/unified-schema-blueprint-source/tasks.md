## 1. Database Migration

- [x] 1.1 Add Goose migration: `ALTER TABLE kb.graph_schemas ADD COLUMN blueprint_source text;`

## 2. Server — Entity & Repository

- [x] 2.1 Add `BlueprintSource *string` field to `CreatePackRequest` in `entity.go`
- [x] 2.2 Add `BlueprintSource *string` field to `GraphMemorySchema` Bun model in `entity.go`
- [x] 2.3 Add `BlueprintSource *string` field to `InstalledSchemaItem` response type in `entity.go`
- [x] 2.4 Add `BlueprintSource *string` field to `MemorySchemaListItem` response type in `entity.go`
- [x] 2.5 Add `UnifiedSchemaItem` response type in `entity.go` (fields: `ID`, `SchemaID`, `Name`, `Version`, `Description`, `Author`, `Visibility`, `Installed bool`, `InstalledAt *time.Time`, `AssignmentID *string`, `BlueprintSource *string`)
- [x] 2.6 Update `Repository.CreatePack` in `repository.go` to write `blueprint_source` from `req.BlueprintSource` (or `"manual"` when nil — keep existing fallback)
- [x] 2.7 Update `Repository.GetInstalledPacks` raw SQL in `repository.go` to SELECT `gtp.blueprint_source` and populate `InstalledSchemaItem.BlueprintSource`
- [x] 2.8 Update `Repository.GetAvailablePacks` query in `repository.go` to also SELECT `blueprint_source` and populate `MemorySchemaListItem.BlueprintSource`
- [x] 2.9 Add `Repository.GetAllPacks` method in `repository.go` that executes a UNION/JOIN query returning all available + installed schemas as `[]UnifiedSchemaItem`

## 3. Server — Service & Handler

- [x] 3.1 Add `GetAllPacks` method to `Service` in `service.go` delegating to `repo.GetAllPacks`
- [x] 3.2 Register `GET /api/schemas/projects/:projectId/all` route in `handler.go` calling `service.GetAllPacks`

## 4. CLI — Blueprint Applier

- [x] 4.1 Set `BlueprintSource` on `CreatePackRequest` in `applier.go` `createPack()` using `p.SourceFile`
- [ ] 4.2 Update `fetchExistingPacks` in `applier.go` to call the new `/all` endpoint instead of two separate calls (optional optimisation — keep old 2-call path as fallback if SDK not updated)

## 5. CLI — Schemas SDK Client

- [x] 5.1 Add `GetAllPacks` method to the schemas SDK client (`apps/server/pkg/sdk/schemas/`) calling `GET /api/schemas/projects/:id/all` and returning `[]UnifiedSchemaItem`

## 6. CLI — Schemas List Command

- [x] 6.1 Update `schemasListCmd` in `tools/cli/internal/cmd/schemas.go` to call `GetAllPacks` by default
- [x] 6.2 Add STATUS column (`installed` / `available`) to the default table output
- [x] 6.3 Add SOURCE column showing `blueprintSource` (or `manual` / `-` when null) to the table output
- [x] 6.4 Preserve `--installed` flag to filter output to installed-only (existing `-i` / `--installed` behaviour)
- [x] 6.5 Preserve `--available` flag to filter output to available-only (existing behaviour)
