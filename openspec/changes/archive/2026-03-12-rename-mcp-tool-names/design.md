## Context

The Memory MCP server exposes 97 tools to AI clients. All tool names currently use a `verb_noun` snake_case pattern (e.g., `list_agents`, `assign_schema`, `get_agent_run_messages`). This puts the action first, which makes it hard to visually group or scan tools by resource area — especially once the count grows beyond ~20.

The CLI convention (`area subcommand`) and the majority of modern API naming guides prefer noun-first namespacing: `resource list`, `resource create`, etc. Hyphenated kebab-case is the natural MCP equivalent.

Tool definitions live in 14 Go source files across 3 domains:
- `apps/server/domain/mcp/` — core tools (service.go + 11 feature files)
- `apps/server/domain/agents/mcp_tools.go` — agent lifecycle tools
- `apps/server/domain/mcpregistry/mcp_tools.go` — MCP server registry tools

Each tool has two touch points:
1. A `Name:` field in the `ToolDefinition` struct (the public MCP name)
2. A `case "old_name":` in a switch dispatch block that routes the call

Both must be updated atomically per tool.

## Goals / Non-Goals

**Goals:**
- Rename all 97 MCP tool `Name` strings to `area[-noun]-action` kebab-case format
- Update all dispatch `case` strings to match
- Define and document the naming convention as a spec so future tools follow it
- Preserve all tool parameters, descriptions, and handler logic exactly

**Non-Goals:**
- Changing any tool's behavior, parameters, or return values
- Adding or removing tools
- Updating external clients (agent definitions, test fixtures) — those are a follow-on
- Versioning or aliasing old names

## Decisions

### D1: Noun-first, action-last ordering within a name

`area[-noun]-action` (e.g., `agent-run-list`) rather than `area-action-noun` (e.g., `agent-list-runs`).

**Rationale:** Noun-first groups all operations on the same resource together alphabetically in any sorted list. `agent-def-create`, `agent-def-delete`, `agent-def-get`, `agent-def-list` are adjacent; `agent-run-get`, `agent-run-list`, `agent-run-messages`, `agent-run-status`, `agent-run-tool-calls` are adjacent. Action-first scatters them (`agent-create-def`, `agent-delete-def`…).

**Alternative considered:** verb-first kebab (`list-agents`, `create-agent`) — rejected because it doesn't improve grouping over the current pattern.

### D2: Hyphen separator, no underscores

Hyphens (`-`) throughout. No mixed formats.

**Rationale:** Consistent with CLI conventions and most REST API naming guides. Underscores are reserved for parameter names (which remain snake_case as per JSON/Go convention).

### D3: Abbreviated sub-nouns for compound areas

Use short abbreviations where needed to keep names readable:
- `agent-def-*` (not `agent-definition-*`) — saves 9 chars, still unambiguous
- `adk-session-*` (ADK is already an abbreviation; `session` stays full)
- `mcp-server-*` and `mcp-registry-*` (two MCP sub-areas)

**Rationale:** Overly long names like `agent-definition-list` create noise; `agent-def-list` is immediately clear in context.

### D4: Single `search-*` area for all search/query tools

`hybrid_search`, `semantic_search`, `find_similar`, `query_knowledge` all become `search-*`:
- `search-hybrid`
- `search-semantic`
- `search-similar`
- `search-knowledge`

**Rationale:** These are all search operations; keeping them under one area helps clients discover them together. `query_knowledge` currently sits in its own file but is conceptually a search tool.

### D5: Complete rename mapping (97 tools)

| Old name | New name |
|---|---|
| `get_project_info` | `project-get` |
| `create_project` | `project-create` |
| `schema_version` | `schema-version` |
| `list_entity_types` | `entity-type-list` |
| `query_entities` | `entity-query` |
| `search_entities` | `entity-search` |
| `get_entity_edges` | `entity-edges-get` |
| `create_entity` | `entity-create` |
| `update_entity` | `entity-update` |
| `delete_entity` | `entity-delete` |
| `restore_entity` | `entity-restore` |
| `create_relationship` | `relationship-create` |
| `list_relationships` | `relationship-list` |
| `update_relationship` | `relationship-update` |
| `delete_relationship` | `relationship-delete` |
| `list_schemas` | `schema-list` |
| `get_schema` | `schema-get` |
| `create_schema` | `schema-create` |
| `delete_schema` | `schema-delete` |
| `assign_schema` | `schema-assign` |
| `update_template_assignment` | `schema-assignment-update` |
| `uninstall_schema` | `schema-uninstall` |
| `get_available_templates` | `template-list-available` |
| `get_installed_templates` | `template-list-installed` |
| `preview_schema_migration` | `schema-migration-preview` |
| `list_migration_archives` | `migration-archive-list` |
| `get_migration_archive` | `migration-archive-get` |
| `hybrid_search` | `search-hybrid` |
| `semantic_search` | `search-semantic` |
| `find_similar` | `search-similar` |
| `traverse_graph` | `graph-traverse` |
| `list_tags` | `tag-list` |
| `list_agent_definitions` | `agent-def-list` |
| `get_agent_definition` | `agent-def-get` |
| `create_agent_definition` | `agent-def-create` |
| `delete_agent_definition` | `agent-def-delete` |
| `list_agents` | `agent-list` |
| `get_agent` | `agent-get` |
| `create_agent` | `agent-create` |
| `delete_agent` | `agent-delete` |
| `list_agent_runs` | `agent-run-list` |
| `get_agent_run` | `agent-run-get` |
| `get_agent_run_messages` | `agent-run-messages` |
| `get_agent_run_tool_calls` | `agent-run-tool-calls` |
| `get_run_status` | `agent-run-status` |
| `list_available_agents` | `agent-list-available` |
| `list_agent_questions` | `agent-question-list` |
| `list_project_agent_questions` | `agent-question-list-project` |
| `respond_to_agent_question` | `agent-question-respond` |
| `list_agent_hooks` | `agent-hook-list` |
| `create_agent_hook` | `agent-hook-create` |
| `delete_agent_hook` | `agent-hook-delete` |
| `list_adk_sessions` | `adk-session-list` |
| `get_adk_session` | `adk-session-get` |
| `list_mcp_servers` | `mcp-server-list` |
| `get_mcp_server` | `mcp-server-get` |
| `create_mcp_server` | `mcp-server-create` |
| `delete_mcp_server` | `mcp-server-delete` |
| `inspect_mcp_server` | `mcp-server-inspect` |
| `get_mcp_registry_server` | `mcp-registry-get` |
| `install_mcp_from_registry` | `mcp-registry-install` |
| `list_skills` | `skill-list` |
| `get_skill` | `skill-get` |
| `create_skill` | `skill-create` |
| `update_skill` | `skill-update` |
| `delete_skill` | `skill-delete` |
| `list_documents` | `document-list` |
| `get_document` | `document-get` |
| `upload_document` | `document-upload` |
| `delete_document` | `document-delete` |
| `get_embedding_status` | `embedding-status` |
| `pause_embeddings` | `embedding-pause` |
| `resume_embeddings` | `embedding-resume` |
| `update_embedding_config` | `embedding-config-update` |
| `list_org_providers` | `provider-list-org` |
| `configure_org_provider` | `provider-configure-org` |
| `configure_project_provider` | `provider-configure-project` |
| `list_provider_models` | `provider-models-list` |
| `test_provider` | `provider-test` |
| `get_provider_usage` | `provider-usage-get` |
| `list_project_api_tokens` | `token-list` |
| `create_project_api_token` | `token-create` |
| `get_project_api_token` | `token-get` |
| `revoke_project_api_token` | `token-revoke` |
| `list_traces` | `trace-list` |
| `get_trace` | `trace-get` |
| `query_knowledge` | `search-knowledge` |
| `brave_web_search` | `web-search-brave` |
| `reddit_search` | `web-search-reddit` |
| `webfetch` | `web-fetch` |

## Risks / Trade-offs

**[Risk] Breaking change for existing clients** → Any agent definition, saved config, or integration test that calls tools by name will break silently (tool-not-found). Mitigation: audit and update e2e tests in the same PR; communicate the rename in release notes. A future improvement could emit a deprecation warning for old names, but that is out of scope.

**[Risk] Dispatch switch missed** → Each tool name appears in at least two places (Name field + case statement). A missed case causes a runtime "unknown tool" error. Mitigation: the tasks artifact will enumerate every file+line that needs updating; CI build + existing handler tests will catch regressions.

**[Risk] Inconsistency in the provider area** → `provider-list-org`, `provider-configure-org`, `provider-configure-project`, `provider-models-list` mix action positions (list-org vs models-list). These are intentional: `configure` is the action in configure-org/configure-project; `list` is the action in list-org; `models-list` treats `models` as the sub-noun. If this feels odd, `provider-org-list`, `provider-org-configure`, `provider-project-configure`, `provider-model-list` are a fully consistent alternative — this can be revisited without breaking the pattern.

## Migration Plan

1. Update all 14 source files in a single PR (no intermediate state where some tools have old names)
2. Update any e2e / integration tests that assert on tool names in the same PR
3. Hot-reload picks up changes automatically (no restart needed unless a new fx module is introduced)
4. Release notes must call out the BREAKING rename for external consumers

**Rollback:** revert the PR. No DB migrations, no state changes.

## Open Questions

- Should `provider-list-org` / `provider-models-list` be regularized to `provider-org-list` / `provider-model-list` for full consistency? (Low priority — both follow the spec pattern)
- Are there any saved agent definitions in production that hardcode old tool names? (Needs a DB query before deploying to production)
