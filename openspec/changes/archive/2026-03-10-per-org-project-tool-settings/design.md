## Context

Built-in MCP tools (e.g., `brave_web_search`, graph tools) are loaded into every agent's tool pool identically regardless of project or org. The only control is a global server-side env var (`BRAVE_SEARCH_API_KEY`). The DB already has `kb.mcp_servers` (with a `type='builtin'` row per project) and `kb.mcp_server_tools` (with an `enabled` bool per tool), but `toolpool.buildCache()` bypasses all of this with `WHERE ms.type != 'builtin'`. There is no org-level concept for tool settings.

## Goals / Non-Goals

**Goals:**
- Allow org admins to set default enabled/disabled state and config (e.g. API key) per built-in tool for all projects in the org.
- Allow project admins to override any tool's enabled state and config at the project level.
- Inheritance: project setting → org default → global env var. Project always wins.
- Show "inherited from org" indicator in the project UI when effective value comes from org.
- Add a `config jsonb` column to `kb.mcp_server_tools` to carry per-project tool config.
- Add a `core.org_tool_settings` table for org-level defaults.
- Wire the tool pool to respect the `enabled` flag on builtin tool rows and resolve config via the inheritance chain.
- E2e test: configure `brave_web_search` at project level and verify it executes via an agent run.
- Plain-text storage for config values (no encryption in this phase).

**Non-Goals:**
- Encrypted secret storage for API keys.
- Per-user tool settings.
- Tool settings for external (non-builtin) MCP servers (they already work via existing toggle API).
- Removing or replacing the global env var fallback (it remains as the final fallback).

## Decisions

### Decision 1: Org-level settings — new dedicated table (`core.org_tool_settings`)

**Chosen:** New table `core.org_tool_settings (id, org_id, tool_name, enabled, config jsonb)`.

**Alternatives considered:**
- Add `org_id` column to `kb.mcp_servers` and create org-scoped builtin server rows — reuses the existing model but makes `project_id NOT NULL` a lie; NULL project_id rows are awkward.
- JSONB column on `core.orgs` — no new table but unbounded, hard to query per tool, no per-tool defaults.

**Rationale:** Clean, typed, easy to query per tool. Separate from the project-scoped MCP tables. Follows the pattern of other org-scoped settings tables.

### Decision 2: Project tool config — `config jsonb` on `kb.mcp_server_tools`

**Chosen:** Add `config jsonb` column to `kb.mcp_server_tools`.

**Alternatives considered:**
- Separate `kb.mcp_server_tool_configs` table — more normalized but unnecessary joins for a simple key-value bag per tool.
- Config on `kb.mcp_servers` row — loses per-tool granularity, all tools share one blob.

**Rationale:** Config is naturally scoped to (server, tool_name). One column, no new joins. JSONB is flexible for future tool-specific config shapes.

### Decision 3: Inheritance resolver — thin service in `mcpregistry` domain

**Chosen:** Add `ResolveBuiltinToolSettings(ctx, projectID, toolName) (enabled bool, config map[string]any, source string)` on `mcpregistry.Service`. It queries project row first, org row second, falls back to global env.

**Rationale:** Keeps resolution logic in one place. `source` field ("project" | "org" | "global") is returned to the API layer so the UI can show the "inherited from org" indicator.

### Decision 4: Tool pool change — replace direct `GetToolDefinitions()` call with DB-aware version

**Chosen:** `toolpool.buildCache()` calls the existing `GetEnabledToolsForProject` but removes the `type != 'builtin'` exclusion. Built-in tools are loaded from `kb.mcp_server_tools` like external tools — respecting their `enabled` flag. The `mcp.Service.GetToolDefinitions(ctx, projectID)` signature gains a `projectID` parameter and filters by the resolved enabled set.

**Alternatively considered:** Keep `buildCache` calling `mcp.Service.GetToolDefinitions()` and add a separate filtering step — two sources of truth for the same data.

**Rationale:** Single source of truth: the DB is authoritative for which builtin tools are enabled for a project. `EnsureBuiltinServer()` already syncs tool metadata on first access. Removing the exclusion filter is the minimal change.

### Decision 5: `brave_web_search` API key resolution

`brave_web_search` currently reads `s.braveSearchAPIKey` (set at server startup from env). With per-project config, `executeBraveWebSearch` needs the project-scoped key. The project ID flows through the tool execution context (`ctx`). The execution path is:

```
toolpool.wrapSingleTool() → mcp.Service.ExecuteTool(ctx, projectID, name, args)
```

`projectID` is already passed to `ExecuteTool`. The change: `executeBraveWebSearch` calls `s.mcpRegistryToolHandler.ResolveBuiltinToolConfig(ctx, projectID, "brave_web_search")` to get the effective API key, falling back to `s.braveSearchAPIKey` (env) if no project/org config exists.

### Decision 6: UpdateMCPServerToolDTO — extend to carry `config`

The existing `PATCH /api/admin/mcp-servers/:id/tools/:toolId` endpoint accepts `UpdateMCPServerToolDTO`. Extend the DTO with an optional `Config map[string]any` field. The repository's `UpdateToolEnabled` is extended to also persist config when provided.

### Decision 7: Org tool settings API — new routes under `/api/admin/orgs/:orgId/tool-settings`

New handler methods on the orgs handler (or a thin new handler in mcpregistry):
- `GET /api/admin/orgs/:orgId/tool-settings` — list all org tool settings
- `PUT /api/admin/orgs/:orgId/tool-settings/:toolName` — upsert one setting (enabled + config)
- `DELETE /api/admin/orgs/:orgId/tool-settings/:toolName` — remove override (revert to global)

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│  toolpool.buildCache(projectID)                                 │
│                                                                 │
│  1. EnsureBuiltinServer(ctx, projectID) → sync tool metadata    │
│  2. FindAllEnabledTools(ctx, projectID)  ← NO type exclusion    │
│     returns both builtin + external tools, filtered by enabled  │
│  3. For builtin tools: wrapSingleTool uses projectID in ctx     │
└────────────────────────────┬────────────────────────────────────┘
                             │ agent executes brave_web_search
                             ▼
┌─────────────────────────────────────────────────────────────────┐
│  mcp.Service.ExecuteTool(ctx, projectID, "brave_web_search", …) │
│                                                                 │
│  executeBraveWebSearch:                                         │
│    key = ResolveBuiltinToolConfig(ctx, projectID, tool)         │
│      1. mcp_server_tools.config->>'api_key' for this project   │
│      2. org_tool_settings.config->>'api_key' for this org      │
│      3. global env BRAVE_SEARCH_API_KEY                         │
└─────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────┐
│  Inheritance resolution for enabled flag                        │
│                                                                 │
│  ResolveBuiltinToolSettings(ctx, projectID, toolName):          │
│    1. SELECT enabled, config FROM kb.mcp_server_tools           │
│       JOIN kb.mcp_servers WHERE project_id=? AND type=builtin   │
│       AND tool_name=?  → if found: return with source="project" │
│    2. SELECT enabled, config FROM core.org_tool_settings        │
│       WHERE org_id = (project's org) AND tool_name=?            │
│       → if found: return with source="org"                      │
│    3. enabled = (env key present), config = env key             │
│       → source = "global"                                       │
└─────────────────────────────────────────────────────────────────┘
```

## Migration Plan

Two migrations, sequential:

1. `00044_add_mcp_server_tool_config.sql` — adds `config jsonb` to `kb.mcp_server_tools`.
2. `00045_create_org_tool_settings.sql` — creates `core.org_tool_settings`.

Both are additive (no existing data changed). Rollback: drop the column / table.

`EnsureBuiltinServer` call in `BulkUpsertTools` already uses `ON CONFLICT DO UPDATE` and does NOT overwrite `enabled`; it will also not overwrite `config` (update set excludes config column by default — config is only written by explicit toggle/update calls).

The `type != 'builtin'` filter removal in `FindAllEnabledTools` is the only behavioral change at the store layer. Existing projects that have never had `EnsureBuiltinServer` called will get it called automatically on first agent run (existing lazy init logic unchanged).

## Risks / Trade-offs

| Risk | Mitigation |
|------|-----------|
| `EnsureBuiltinServer` not called before toolpool build | `buildCache` already calls `GetEnabledToolsForProject` which is only called after `EnsureBuiltinServer` in the existing service flow. Explicit call added to `buildCache` if builtin server not yet registered. |
| Removing `type != 'builtin'` filter doubles tool rows returned from DB for projects with builtin server | Negligible — 30-40 extra rows per query, all indexed on `project_id`. |
| Circular import: `mcp.Service` needing mcpregistry to resolve config | Already solved: `MCPRegistryToolHandler` interface in `mcp/entity.go` is the injection point. Add `ResolveBuiltinToolConfig` and `ResolveBuiltinToolEnabled` to this interface. |
| `EnsureBuiltinServer` in-memory `builtinRegistered` flag blocks re-sync of tool metadata after server restart | Acceptable: tool metadata (name/description/schema) almost never changes. Operators can force re-sync via existing "sync tools" API endpoint. |
| Project's org_id not directly available in mcpregistry repository | `kb.projects` table has `org_id`; one JOIN in the resolver query. Alternatively, pass `orgID` as a parameter via service layer that already has it. |

## Open Questions

- None blocking implementation. Plain-text config storage is confirmed acceptable for this phase.
