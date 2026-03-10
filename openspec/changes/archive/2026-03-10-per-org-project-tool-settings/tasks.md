## 1. Database Migrations

- [x] 1.1 Create migration `00044_add_mcp_server_tool_config.sql` — add `config jsonb` column to `kb.mcp_server_tools`
- [x] 1.2 Create migration `00045_create_org_tool_settings.sql` — create `core.org_tool_settings` table with `(id uuid PK, org_id uuid NOT NULL REFERENCES core.orgs(id) ON DELETE CASCADE, tool_name text NOT NULL, enabled bool NOT NULL DEFAULT true, config jsonb, created_at timestamptz, updated_at timestamptz, UNIQUE(org_id, tool_name))`

## 2. Org Tool Settings — Backend

- [x] 2.1 Add `OrgToolSetting` Bun model and DTOs to `apps/server/domain/orgs/entity.go` (or a new `apps/server/domain/orgs/tool_settings_entity.go`)
- [x] 2.2 Add repository methods to `apps/server/domain/orgs/repository.go`: `FindOrgToolSettings(ctx, orgID)`, `FindOrgToolSetting(ctx, orgID, toolName)`, `UpsertOrgToolSetting(ctx, *OrgToolSetting)`, `DeleteOrgToolSetting(ctx, orgID, toolName)`
- [x] 2.3 Add service methods to `apps/server/domain/orgs/service.go`: `GetOrgToolSettings`, `UpsertOrgToolSetting`, `DeleteOrgToolSetting` with org membership auth checks
- [x] 2.4 Add handler methods to `apps/server/domain/orgs/handler.go`: `handleListOrgToolSettings`, `handleUpsertOrgToolSetting`, `handleDeleteOrgToolSetting`
- [x] 2.5 Register routes in `apps/server/domain/orgs/routes.go`: `GET /api/admin/orgs/:orgId/tool-settings`, `PUT /api/admin/orgs/:orgId/tool-settings/:toolName`, `DELETE /api/admin/orgs/:orgId/tool-settings/:toolName`

## 3. Inheritance Resolver — Backend

- [x] 3.1 Add `ResolveBuiltinToolSettings(ctx, projectID, toolName string) (enabled bool, config map[string]any, source string, err error)` method to `apps/server/domain/mcpregistry/service.go` — queries project builtin server tool first, then org tool settings, then falls back to global env
- [x] 3.2 Add `ResolveBuiltinToolConfig(ctx, projectID, toolName string) (config map[string]any, source string, err error)` convenience method to `mcpregistry/service.go` for config-only resolution
- [x] 3.3 Add `ResolveBuiltinToolSettings` and `ResolveBuiltinToolConfig` to the `MCPRegistryToolHandler` interface in `apps/server/domain/mcp/entity.go`
- [x] 3.4 Implement the interface methods on the `MCPToolsHandler` in `apps/server/domain/mcpregistry/mcp_tools.go`

## 4. MCP Server Tools — Backend Changes

- [x] 4.1 Add `Config map[string]any` field to `MCPServerTool` Bun model in `apps/server/domain/mcpregistry/entity.go`
- [x] 4.2 Add `Config map[string]any` field to `MCPServerToolDTO` in `entity.go`; add `InheritedFrom string` field to `MCPServerToolDTO` (populated by the handler when `source != "project"`)
- [x] 4.3 Extend `UpdateMCPServerToolDTO` in `entity.go` with optional `Config *map[string]any` field
- [x] 4.4 Add `UpdateToolConfig(ctx, id string, config map[string]any)` repository method to `apps/server/domain/mcpregistry/repository.go`
- [x] 4.5 Update `UpdateToolEnabled` (or add a new `UpdateTool`) in the repository to accept and persist `config` when provided
- [x] 4.6 Update the toggle tool handler in `apps/server/domain/mcpregistry/handler.go` to persist `config` from the request DTO and invalidate the tool pool cache

## 5. Tool Pool — Wire Builtin Enabled Filter

- [x] 5.1 Remove `WHERE ms.type != 'builtin'` exclusion from `FindAllEnabledTools` in `apps/server/domain/mcpregistry/repository.go` (or add a separate `FindAllEnabledBuiltinTools` query that returns builtin tools filtered by `enabled`)
- [x] 5.2 Update `toolpool.buildCache()` in `apps/server/domain/agents/toolpool.go` to call `EnsureBuiltinServer` before loading tools, then load builtin tools from the DB (instead of calling `mcp.Service.GetToolDefinitions()` unconditionally)
- [x] 5.3 Ensure `EnabledServerTool` struct in `mcpregistry/repository.go` carries the `config` field so it can be passed into the tool execution context if needed
- [x] 5.4 Invalidate tool pool cache when org tool settings change (call `tp.InvalidateAll()` or per-project invalidation in the org tool settings service methods)

## 6. brave_web_search — Use Inherited API Key

- [x] 6.1 Update `executeBraveWebSearch` in `apps/server/domain/mcp/brave_search.go` to call `s.mcpRegistryToolHandler.ResolveBuiltinToolConfig(ctx, projectID, "brave_web_search")` and use the resolved `api_key`, falling back to `s.braveSearchAPIKey` (global env) when not found
- [x] 6.2 Ensure `projectID` is available in the execution context for `executeBraveWebSearch` (verify it is passed through `ExecuteTool`)

## 7. Builtin Tool Settings — List Endpoint Enhancement

- [x] 7.1 Update the `GET /api/admin/mcp-servers/:id/tools` handler to enrich each tool DTO with `inheritedFrom` by calling `ResolveBuiltinToolSettings` for each tool when the server type is `builtin`
- [x] 7.2 Ensure the `EnsureBuiltinServer` is called on `GET /api/admin/mcp-servers` so all builtin tools exist in the DB before the listing

## 8. Frontend — Project Built-in Tool Settings

- [x] 8.1 Add a "Built-in Tools" section to the project MCP settings page in `/root/emergent.memory.ui/src` — list all builtin tools from `GET /api/admin/mcp-servers/:id/tools` (where server type = builtin)
- [x] 8.2 Render each tool with a toggle (enabled/disabled) and show "Inherited from org" / "Inherited from global" badge when `inheritedFrom != "project"`
- [x] 8.3 Render config fields for tools that have known config params (start with `brave_web_search` → `api_key` field); show masked placeholder if value is inherited
- [x] 8.4 Wire toggle and config save to `PATCH /api/admin/mcp-servers/:id/tools/:toolId`
- [x] 8.5 Invalidate / refetch tool list after save

## 9. Frontend — Org Tool Settings

- [x] 9.1 Add an "Org Tool Defaults" section/tab to the org settings page in `/root/emergent.memory.ui/src`
- [x] 9.2 List all known built-in tools with their current org-level setting (fetch from `GET /api/admin/orgs/:orgId/tool-settings`; tools without an org setting show global default state)
- [x] 9.3 Render toggle + config fields for each tool; save via `PUT /api/admin/orgs/:orgId/tool-settings/:toolName`
- [x] 9.4 Allow removing an org override via `DELETE /api/admin/orgs/:orgId/tool-settings/:toolName`

## 10. E2E Tests

- [x] 10.1 Create `apps/server/tests/e2e/tool_settings_test.go` with `ToolSettingsSuite` embedding `testutil.BaseSuite`
- [x] 10.2 Test: `TestProjectToolSettings_ToggleBuiltinTool` — enable/disable a builtin tool at project level and verify it appears / disappears from the MCP tools list endpoint
- [x] 10.3 Test: `TestOrgToolSettings_CRUD` — create, read, update, delete an org tool setting via the new API endpoints
- [x] 10.4 Test: `TestToolInheritance_OrgDefaultUsedWhenNoProjectOverride` — set an org-level setting, verify project with no override reflects `inheritedFrom: "org"` in the tool list response
- [x] 10.5 Test: `TestToolInheritance_ProjectOverridesOrg` — set org setting to enabled and project setting to disabled, verify project tool list shows disabled with `inheritedFrom: "project"`
- [x] 10.6 Test: `TestBraveWebSearch_ProjectApiKey` — configure `brave_web_search` at project level with a test API key, trigger an agent run, verify the tool call is recorded in `kb.agent_run_tool_calls` with `tool_name = "brave_web_search"` (uses a real Brave API key from test env or stubs the HTTP call)
