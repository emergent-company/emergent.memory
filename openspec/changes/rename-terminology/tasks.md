## 1. Database Migrations

- [x] 1.1 Write migration A: rename `kb.graph_template_packs` → `kb.graph_schemas`, `kb.project_template_packs` → `kb.project_schemas`, `kb.template_pack_studio_sessions` → `kb.schema_studio_sessions`, `kb.template_pack_studio_messages` → `kb.schema_studio_messages`
- [x] 1.2 Migration A: rename column `template_pack_id` → `schema_id` on `kb.project_schemas`, `kb.project_object_type_registry`, and `kb.discovery_jobs`
- [x] 1.3 Migration A: rename `kb.project_object_type_registry` → `kb.project_object_schema_registry`
- [x] 1.4 Migration A: rename all affected indexes, FK constraints, and RLS policies for the four renamed tables
- [x] 1.5 Write migration B: rename `kb.agent_workspaces` → `kb.agent_sandboxes`, `kb.workspace_images` → `kb.sandbox_images`
- [x] 1.6 Migration B: rename column `workspace_config` → `sandbox_config` on `kb.agent_definitions`
- [x] 1.7 Migration B: `UPDATE kb.agent_sandboxes SET container_type = 'agent_sandbox' WHERE container_type = 'agent_workspace'`
- [x] 1.8 Migration B: rename all affected indexes and FK constraints for both renamed tables
- [x] 1.9 Write DOWN sections for both migrations (reverse renames)
- [x] 1.10 Run `goose up` locally and verify both migrations apply cleanly

## 2. Go — TemplatePack → MemorySchema (package: schemas)

- [x] 2.1 Rename directory `apps/server/domain/templatepacks/` → `apps/server/domain/schemas/`; update `package` declaration in all files
- [x] 2.2 Rename directory `apps/server/pkg/sdk/templatepacks/` → `apps/server/pkg/sdk/schemas/`; update `package` declaration
- [x] 2.3 Rename Go structs: `GraphTemplatePack` → `GraphMemorySchema`, `ProjectTemplatePack` → `ProjectMemorySchema`, `TemplatePackListItem` → `MemorySchemaListItem`, `InstalledPackItem` → `InstalledSchemaItem`, all request/response types
- [x] 2.4 Update Bun table tags in renamed structs: `kb.graph_template_packs` → `kb.graph_schemas`, `kb.project_template_packs` → `kb.project_schemas`
- [x] 2.5 Rename `apps/server/domain/extraction/template_pack_schema_provider.go` → `schema_provider.go`; rename `TemplatePackSchemaProvider` → `MemorySchemaProvider`, `NewTemplatePackSchemaProvider` → `NewMemorySchemaProvider`
- [x] 2.6 Rename functions in `extraction/agents/schemas.go`: `BuildEntitySchemaFromTemplatePack` → `BuildEntitySchemaFromMemorySchema`, `BuildRelationshipSchemaFromTemplatePack` → `BuildRelationshipSchemaFromMemorySchema`
- [x] 2.7 Update all call sites in `extraction/agents/entity_extractor.go`, `pipeline.go`, `relationship_builder.go`
- [x] 2.8 Update `domain/extraction/module.go`: rename `provideTemplatePackSchemaProvider` → `provideMemorySchemaProvider`
- [x] 2.9 Update `domain/discoveryjobs/repository.go`: rename `CreateTemplatePack`/`GetTemplatePack`/`UpdateTemplatePack`/`SetJobTemplatePack` → `CreateMemorySchema`/`GetMemorySchema`/`UpdateMemorySchema`/`SetJobMemorySchema`; update Bun table tags
- [x] 2.10 Update `domain/discoveryjobs/entity.go` and `dto.go`: rename field `TemplatePackID` → `SchemaID`, JSON tag `template_pack_id` → `schema_id`
- [x] 2.11 Update `domain/discoveryjobs/service.go`: update all calls to renamed repository functions
- [x] 2.12 Update `domain/typeregistry/entity.go`: rename embedded struct fields and `TemplatePackID` → `SchemaID`
- [x] 2.13 Update `cmd/migrate-schema/main.go`: rename local structs and Bun table references
- [x] 2.14 Update `pkg/sdk/sdk.go`: rename import and field `TemplatePacks` → `Schemas`
- [x] 2.15 Update `pkg/sdk/projects/client.go`: rename embedded `TemplatePack` struct → `MemorySchema`, field `TemplatePacks` → `Schemas`
- [x] 2.16 Update route group in `domain/schemas/routes.go`: `/api/template-packs` → `/api/schemas` (including studio sub-routes)
- [x] 2.17 Update `domain/schemas/module.go`: fx module name `"templatepacks"` → `"schemas"`
- [x] 2.18 Update `cmd/server/main.go`: import path and module reference
- [x] 2.19 Run `go build ./...` and fix any remaining import or type errors

## 3. Go — TypeRegistry → SchemaRegistry (package: schemaregistry)

- [x] 3.1 Rename directory `apps/server/domain/typeregistry/` → `apps/server/domain/schemaregistry/`; update `package` declaration in all files
- [x] 3.2 Rename directory `apps/server/pkg/sdk/typeregistry/` → `apps/server/pkg/sdk/schemaregistry/`; update `package` declaration
- [x] 3.3 Rename Go structs: `ProjectObjectTypeRegistry` → `ProjectObjectSchemaRegistry`, `TypeRegistryEntryDTO` → `SchemaRegistryEntryDTO`, `TypeRegistryStats` → `SchemaRegistryStats`, `TypeRegistryRowDTO` → `SchemaRegistryRowDTO`
- [x] 3.4 Update Bun table tag: `kb.project_object_type_registry` → `kb.project_object_schema_registry`
- [x] 3.5 Update SDK struct: `TypeRegistryEntry` → `SchemaRegistryEntry`, `TypeRegistryStats` → `SchemaRegistryStats`; update API URLs in SDK client to `/api/schema-registry/...`
- [x] 3.6 Update `pkg/sdk/sdk.go`: rename import and field `TypeRegistry` → `SchemaRegistry`
- [x] 3.7 Update route group in `domain/schemaregistry/routes.go`: `/api/type-registry` → `/api/schema-registry`
- [x] 3.8 Update `domain/schemaregistry/module.go`: fx module name `"typeregistry"` → `"schemaregistry"`
- [x] 3.9 Update `cmd/server/main.go`: import path and module reference
- [x] 3.10 Update `internal/testutil/server.go`: import path and variable/function names
- [x] 3.11 Update `domain/mcp/service.go`: raw SQL `FROM kb.project_object_type_registry` → `FROM kb.project_object_schema_registry`
- [x] 3.12 Update `domain/schemas/repository.go` (formerly templatepacks): `INSERT INTO kb.project_object_type_registry` → `kb.project_object_schema_registry`
- [x] 3.13 Run `go build ./...` and fix any remaining errors

## 4. Go — AgentWorkspace → AgentSandbox (packages: sandbox, sandboximages)

- [x] 4.1 Rename directory `apps/server/domain/workspace/` → `apps/server/domain/sandbox/`; update `package workspace` → `package sandbox` in all files
- [x] 4.2 Rename directory `apps/server/domain/workspaceimages/` → `apps/server/domain/sandboximages/`; update `package workspaceimages` → `package sandboximages`
- [x] 4.3 Rename struct `AgentWorkspace` → `AgentSandbox`; update Bun table tag `kb.agent_workspaces` → `kb.agent_sandboxes`
- [x] 4.4 Rename `ContainerTypeAgentWorkspace` constant → `ContainerTypeAgentSandbox`; update value `"agent_workspace"` → `"agent_sandbox"`
- [x] 4.5 Rename `AgentWorkspaceConfig` → `AgentSandboxConfig`, `ParseAgentWorkspaceConfig` → `ParseAgentSandboxConfig`, `DefaultAgentWorkspaceConfig` → `DefaultAgentSandboxConfig`, `workspace_config.go` → `sandbox_config.go`
- [x] 4.6 Rename `WorkspaceImage` → `SandboxImage`, `WorkspaceImageDTO` → `SandboxImageDTO`, `CreateWorkspaceImageRequest` → `CreateSandboxImageRequest`; update Bun table tag `kb.workspace_images` → `kb.sandbox_images`
- [x] 4.7 Update route group in `domain/sandbox/routes.go`: `/api/v1/agent/workspaces` → `/api/v1/agent/sandboxes`
- [x] 4.8 Update route group in `domain/sandboximages/routes.go`: `/api/admin/workspace-images` → `/api/admin/sandbox-images`
- [x] 4.9 Update `domain/sandbox/module.go` and `domain/sandboximages/module.go`: fx module names and log strings
- [x] 4.10 Update `apps/server/internal/config/config.go`: `WorkspaceConfig` struct → `SandboxConfig`, field `Workspace` → `Sandbox`, env var reference `ENABLE_AGENT_WORKSPACES` → `ENABLE_AGENT_SANDBOXES`
- [x] 4.11 Update `domain/agents/workspace_tools.go`: import path and all type references (`workspace.*` → `sandbox.*`)
- [x] 4.12 Update `domain/agents/executor.go`: import path, type references, log strings
- [x] 4.13 Update `domain/agents/handler.go`: import path, type references, swagger comments
- [x] 4.14 Update `domain/sandboximages/module.go`: import of `domain/sandbox`, all type references
- [x] 4.15 Update `cmd/server/main.go`: import paths for `domain/sandbox` and `domain/sandboximages`, module references
- [x] 4.16 Update `kb.agent_definitions` Go struct field `WorkspaceConfig` → `SandboxConfig` with updated Bun column tag `sandbox_config`
- [x] 4.17 Run `go build ./...` and fix any remaining errors

## 5. CLI — TemplatePack → Schema

- [x] 5.1 Rename file `tools/cli/internal/cmd/template_packs.go` → `schemas.go`
- [x] 5.2 Update `Use: "template-packs"` → `Use: "schemas"` and all subcommand `Use` strings
- [x] 5.3 Update all user-visible strings: "template pack" → "schema", "Template pack created!" → "Schema created!", "Template pack installed." → "Schema installed.", etc.
- [x] 5.4 Update import from `sdk/templatepacks` → `sdk/schemas`; update all SDK type references
- [x] 5.5 Rename command variable names: `templatePacksCmd` → `schemasCmd`, etc.
- [x] 5.6 Update `tools/cli/internal/cmd/root.go` (or wherever commands are registered) to use the new command variable name
- [x] 5.7 Build CLI with `task cli:install` and verify `memory schemas list` works and `memory template-packs` returns unknown command error

## 6. Frontend (emergent.memory.ui)

- [x] 6.1 Update `src/pages/admin/pages/settings/project/templates.tsx`: rename interface `TemplatePack` → `MemorySchema`, function `loadTemplatePacks` → `loadMemorySchemas`, all `/api/template-packs/...` URLs → `/api/schemas/...`, JSON field `template_pack_id` → `schema_id`, all user-visible strings ("Template Pack" → "Schema", "Template Packs" → "Schemas")
- [x] 6.2 Update `src/components/organisms/DiscoveryWizard/Step4_5_ConfigurePack.tsx`: interface `TemplatePack` → `MemorySchema`, API URL → `/api/schemas`
- [x] 6.3 Update `src/components/organisms/DiscoveryWizard/Step5_Complete.tsx`: field `template_pack_id` → `schema_id`
- [x] 6.4 Update `src/components/organisms/DiscoveryWizard/DiscoveryWizard.tsx`: field `template_pack_id` → `schema_id`
- [x] 6.5 Update `src/components/organisms/ExtractionConfigModal.tsx`: API URL → `/api/schemas/projects/${projectId}/compiled-types`
- [x] 6.6 Update `src/components/organisms/ObjectDetailModal/ObjectDetailContent.tsx`: field `template_pack_name` → `schema_name`
- [x] 6.7 Update `src/components/organisms/ObjectDetailModal/ObjectDetailModal.tsx`: field `template_pack_name` → `schema_name`
- [x] 6.8 Update `src/hooks/use-template-studio-chat.ts`: interface `TemplatePack` → `MemorySchema`, all `/api/template-packs/studio/...` URLs → `/api/schemas/studio/...`
- [x] 6.9 Update `src/pages/admin/pages/settings/project/auto-extraction.tsx`: API URL → `/api/schemas/projects/${...}/compiled-types`
- [x] 6.10 Rename `src/api/type-registry.ts` → `src/api/schema-registry.ts`; rename `TypeRegistryEntryDto` → `SchemaRegistryEntryDto`, `TypeRegistryStats` → `SchemaRegistryStats`, function `createTypeRegistryClient` → `createSchemaRegistryClient`, all API URLs → `/api/schema-registry/...`; update field names `template_pack_id` → `schema_id`, `template_pack_name` → `schema_name`
- [x] 6.11 Update all imports of `@/api/type-registry` → `@/api/schema-registry` in `ObjectDetailContent.tsx`, `ObjectDetailModal.tsx`, `src/pages/admin/pages/objects/index.tsx`
- [x] 6.12 Run `pnpm run build` in `/root/emergent.memory.ui` and fix any type or import errors

## 7. Test Suite

- [x] 7.1 Update `tools/opencode-test-suite/internal/assert/assert.go`: rename `HasTemplatePack` → `HasMemorySchema`; update CLI call from `"template-packs", "installed"` → `"schemas", "installed"`
- [x] 7.2 Update `tools/opencode-test-suite/tests/onboard_test.go`: update `assert.HasTemplatePack(...)` → `assert.HasMemorySchema(...)`
- [x] 7.3 Run the test suite to confirm no regressions

## 8. Swagger / OpenAPI

- [x] 8.1 Regenerate Swagger docs (`swag init` from `apps/server/`) after all Go renames are complete
- [x] 8.2 Verify generated `docs/swagger/docs.go` contains `/api/schemas`, `/api/schema-registry`, `/api/v1/agent/sandboxes` paths and no old paths

## 9. Documentation

- [x] 9.1 Rename `docs/site/developer-guide/template-packs.md` → `schema.md`; update all content references
- [x] 9.2 Rename `docs/site/go-sdk/reference/templatepacks.md` → `schemas.md`; update content
- [x] 9.3 Rename `docs/public/guides/template-pack-creation.md`; update content
- [x] 9.4 Rename `docs/site/developer-guide/type-registry.md` → `schema-registry.md`; update content
- [x] 9.5 Rename `docs/site/go-sdk/reference/typeregistry.md` → `schemaregistry.md`; update content
- [x] 9.6 Rename `docs/agent-workspace/` directory → `docs/agent-sandbox/`; update content in all 4 files (DEPLOYMENT.md, GVISOR_SETUP.md, MCP_HOSTING.md, PROVIDER_SELECTION.md)
- [x] 9.7 Rename `docs/site/developer-guide/workspace.md` → `sandbox.md`; update content
- [x] 9.8 Update `apps/server/domain/DOMAIN_GUIDE.md`: update rows for `templatepacks`, `typeregistry`, `workspace`, `workspaceimages` domains
- [x] 9.9 Update `AGENTS.md` top-level: domain layout list (remove `workspace`, `workspaceimages`; add `sandbox`, `sandboximages`; update `templatepacks` → `schemas`; update `typeregistry` → `schemaregistry`)
- [x] 9.10 Update any remaining markdown files found by `grep -r "template.pack\|type.registry\|agent.workspace" docs/ --include="*.md" -l`

## 10. Deployment / Config

- [x] 10.1 Update all `.env` / `.env.example` files: `ENABLE_AGENT_WORKSPACES` → `ENABLE_AGENT_SANDBOXES`
- [x] 10.2 Verify Docker Compose files (if any) referencing `ENABLE_AGENT_WORKSPACES` are updated
- [ ] 10.3 Add release note / migration guide entry documenting the env var rename and all breaking API/CLI changes
