## Why

The platform uses inconsistent, overlapping, and non-intuitive terminology that creates confusion for users, developers, and documentation readers. "TemplatePack" sounds too close to "Blueprint" (a different concept), "TypeRegistry" is opaque jargon, and "AgentWorkspace" collides with the common UI meaning of "workspace". Renaming these now — before the sandbox/workspace surface gets exposed in the UI — avoids a larger migration later.

## What Changes

- **BREAKING** `TemplatePack` renamed to `Schema` (user-facing) / `MemorySchema` (internal Go types): CLI command `template-packs` → `schemas`, API prefix `/api/template-packs` → `/api/schemas`, DB tables `kb.graph_template_packs` → `kb.graph_schemas` and related tables, Go structs/packages renamed
- **BREAKING** `TypeRegistry` renamed to `Schema Registry`: API prefix `/api/type-registry` → `/api/schema-registry`, DB table `kb.project_object_type_registry` → `kb.project_object_schema_registry`, Go package `typeregistry` → `schemaregistry`, SDK field `TypeRegistry` → `SchemaRegistry`; user-facing UI label stays "Objects" (no visible change)
- **BREAKING** `AgentWorkspace` renamed to `AgentSandbox` / `Sandbox`: Go package `domain/workspace` → `domain/sandbox`, `domain/workspaceimages` → `domain/sandboximages`, DB tables `kb.agent_workspaces` → `kb.agent_sandboxes`, `kb.workspace_images` → `kb.sandbox_images`, API prefix `/api/v1/agent/workspaces` → `/api/v1/agent/sandboxes`, `container_type` string value `'agent_workspace'` → `'agent_sandbox'` in existing rows, env var `ENABLE_AGENT_WORKSPACES` → `ENABLE_AGENT_SANDBOXES`
- Column renames: `template_pack_id` → `schema_id` everywhere it appears, `workspace_config` → `sandbox_config` on `kb.agent_definitions`
- All docs, markdown files, and test suite assertions updated to match new names
- No backward-compat aliases — hard cut

## Capabilities

### New Capabilities

- `memory-schema-api`: Renamed REST API surface for TemplatePack (now Schema): CRUD endpoints, studio endpoints, project assignment endpoints — all under `/api/schemas`
- `schema-registry-api`: Renamed REST API surface for TypeRegistry (now Schema Registry): project type catalog endpoints under `/api/schema-registry`
- `agent-sandbox-api`: Renamed REST API surface for AgentWorkspace (now Sandbox): compute environment lifecycle endpoints under `/api/v1/agent/sandboxes`

### Modified Capabilities

- `template-packs`: All requirements and behavior unchanged; only naming/identifiers change. Delta spec needed to reflect new API paths, CLI command names, DB table names, and Go type names.
- `template-pack-studio`: Studio session API paths change from `/api/template-packs/studio/*` to `/api/schemas/studio/*`. Behavior unchanged.
- `agent-workspace-config`: Config struct rename (`AgentWorkspaceConfig` → `AgentSandboxConfig`), env var rename (`ENABLE_AGENT_WORKSPACES` → `ENABLE_AGENT_SANDBOXES`). Behavior unchanged.
- `agent-workspace-persistence`: DB table renames (`kb.agent_workspaces` → `kb.agent_sandboxes`, `kb.workspace_images` → `kb.sandbox_images`). Behavior unchanged.

## Impact

- **Go backend**: ~35 files across `domain/templatepacks`, `domain/typeregistry`, `domain/workspace`, `domain/workspaceimages`, `domain/agents`, `domain/extraction`, `domain/discoveryjobs`, `domain/mcp`, `pkg/sdk`, `cmd/server/main.go`, `internal/config`, `internal/testutil`
- **CLI**: `tools/cli/internal/cmd/template_packs.go` renamed to `schemas.go`; all command names and user-visible strings updated; SDK import updated
- **Frontend** (`emergent.memory.ui`): ~10 files in `src/pages`, `src/components`, `src/hooks`, `src/api`; API URL strings updated; TypeScript interface names updated; user-visible labels updated for TemplatePack only (TypeRegistry/Sandbox have no current UI exposure)
- **Database**: Two Goose migration files; 6 table renames, multiple column renames, index/FK/RLS policy renames, one data UPDATE for `container_type` value
- **Test suite**: `tools/opencode-test-suite` assert helpers and test calls updated
- **Docs**: ~13 markdown files renamed and updated
- **Operators**: `ENABLE_AGENT_WORKSPACES` env var renamed — requires update in any deployment config
