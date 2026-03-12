## Why

MCP tool names currently use an inconsistent `verb_noun` snake_case format (e.g., `assign_schema`, `list_agent_definitions`, `get_provider_usage`). This is inconsistent with how CLI tools present namespaced commands — the standard convention is `area-action` or `area-noun-action` with hyphens, grouping tools by resource area first. Renaming all 97 tools to the `area[-noun]-action` hyphenated format makes the tool catalog scannable, predictable, and consistent with CLI idioms.

## What Changes

- **BREAKING**: All 97 MCP tool names are renamed from `verb_noun` (snake_case, verb-first) to `area[-noun]-action` (kebab-case, noun-first) format
- No behavioral changes — only the `Name` field in each tool definition changes, plus corresponding case-switch dispatch strings
- Tools are grouped by clear resource areas: `entity`, `relationship`, `schema`, `template`, `migration`, `search`, `graph`, `tag`, `agent`, `adk`, `mcp`, `skill`, `document`, `embedding`, `provider`, `token`, `trace`, `project`, `web`
- The `query_knowledge` tool moves to `search-knowledge` to join the `search-*` group

## Capabilities

### New Capabilities
- `mcp-tool-naming-convention`: Defines and enforces the `area[-noun]-action` naming convention for all MCP tools — the complete rename mapping and the files that must be updated

### Modified Capabilities
<!-- No spec-level behavior changes — this is a rename-only refactor -->

## Impact

**Files changed:**
- `apps/server/domain/mcp/service.go` — 29 tool Name fields + 2 switch dispatch blocks
- `apps/server/domain/mcp/agent_ext_tools.go` — 8 tool Name fields
- `apps/server/domain/mcp/brave_search.go` — 1 Name field
- `apps/server/domain/mcp/documents_tools.go` — 4 Name fields
- `apps/server/domain/mcp/embeddings_tools.go` — 4 Name fields
- `apps/server/domain/mcp/provider_tools.go` — 6 Name fields
- `apps/server/domain/mcp/query_tools.go` — 1 Name field
- `apps/server/domain/mcp/reddit_search.go` — 1 Name field
- `apps/server/domain/mcp/skills_tools.go` — 5 Name fields
- `apps/server/domain/mcp/token_tools.go` — 4 Name fields
- `apps/server/domain/mcp/trace_tools.go` — 2 Name fields
- `apps/server/domain/mcp/webfetch.go` — 1 Name field
- `apps/server/domain/agents/mcp_tools.go` — 14 Name fields + dispatch switch
- `apps/server/domain/mcpregistry/mcp_tools.go` — 7 Name fields + dispatch switch

**Breaking impact:** Any MCP client (AI agent, integration, config file) that references a tool by its old verb_noun name will need to update to the new area-action name. This includes saved agent definitions, external MCP client configs, and integration tests that assert on tool names.

**No server logic changes** — all handler logic, parameters, and return values are unchanged.
