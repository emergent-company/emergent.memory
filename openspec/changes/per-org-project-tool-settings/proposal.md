## Why

Built-in agent tools (e.g., `brave_web_search`) are currently enabled globally via server-side env vars and applied uniformly to every project. There is no way for an org admin to set defaults or a project admin to selectively enable/disable tools or supply per-project credentials (e.g., a dedicated Brave Search API key), making it impossible to control tool access or costs at the project or org level.

## What Changes

- Introduce a `core.org_tool_settings` table to store org-level tool defaults (enabled flag + config JSONB per tool name).
- Add a `config JSONB` column to `kb.mcp_server_tools` to store per-project tool configuration (e.g., API keys).
- Change the tool pool build path to respect the per-project `enabled` flag on built-in tool rows (currently bypassed by a `type != 'builtin'` filter).
- Implement a three-tier inheritance resolver: project setting → org default → global env var.
- Expose org-level tool settings via new API endpoints (`GET/PUT /orgs/:id/tool-settings`).
- Extend the existing MCP server tools API (`PATCH /mcp-servers/:id/tools/:toolId`) to accept a `config` payload.
- Add a "Built-in Tools" section to the project MCP settings UI with toggle + config fields per tool, showing an "inherited from org" indicator when the effective value comes from the org default.
- Add an "Org Defaults → Tools" section in org settings UI.
- Add an e2e test that configures `brave_web_search` at the project level and executes it via an agent run.

## Capabilities

### New Capabilities

- `org-tool-settings`: Org-level default tool enable/disable and config (API keys) stored in `core.org_tool_settings`, managed by org admins, applied as fallback when no project-level override exists.
- `project-tool-settings`: Per-project built-in tool enable/disable and config stored in `kb.mcp_server_tools.config`, managed by project admins, always takes precedence over org defaults.
- `tool-settings-inheritance`: Three-tier resolution logic (project → org → global env) used when building the agent tool pool and when executing configurable tools like `brave_web_search`.

### Modified Capabilities

- `agent-tool-pool`: Tool pool initialization now reads `mcp_server_tools.enabled` for built-in tools (the `type != 'builtin'` exclusion is removed); built-in tools absent or disabled in the DB are excluded from the agent's tool pool.
- `mcp-integration`: `GetToolDefinitions` gains a `projectID` parameter; `brave_web_search` resolves its API key via the inheritance chain rather than solely from the global env var.

## Impact

- **Database**: 1 new table (`core.org_tool_settings`), 1 new column (`kb.mcp_server_tools.config jsonb`).
- **Backend**: `apps/server/domain/mcp/`, `apps/server/domain/mcpregistry/`, `apps/server/domain/orgs/`, `apps/server/domain/agents/`.
- **Frontend**: `/root/emergent.memory.ui` — project MCP settings page, org settings page.
- **Tests**: New e2e test in `apps/server/tests/` covering tool config + agent execution.
- **No breaking API changes** — existing toggle endpoint extended, not replaced.
