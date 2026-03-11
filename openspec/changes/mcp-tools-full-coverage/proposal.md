## Why

The `memory` CLI covers the full surface area of the Emergent API — documents, graph objects and relationships, schemas, embeddings, agents, agent definitions, ADK sessions, skills, MCP servers, provider configuration, projects, API tokens, traces, and real-time query/ask. AI agents that use the MCP system can only reach the ~63 tools currently registered in `apps/server/domain/mcp/service.go`, which skips large swaths of that surface: no document management, no embedding control, no schema/template-pack operations, no trace inspection, no provider configuration, no token management, and only partial coverage of agents and projects.

Closing this gap means an AI agent can do everything a human user can do from the CLI — without requiring shell access or a separate tooling layer.

## What Changes

- Add MCP tool definitions and handlers in `apps/server/domain/mcp/service.go` (and optionally split into domain-specific files) for every CLI capability that is not already covered:
  - **Documents**: `list_documents`, `get_document`, `upload_document`, `delete_document`
  - **Embeddings**: `get_embedding_status`, `pause_embeddings`, `resume_embeddings`, `get_embedding_config`, `update_embedding_config`
  - **Schemas (template packs)**: `list_schemas`, `get_schema`, `create_schema`, `delete_schema`, `list_installed_schemas`, `install_schema`, `uninstall_schema`, `get_compiled_types`
  - **Agents** (gaps): `list_agent_runs`, `get_agent_run`, `list_agent_questions`, `list_project_agent_questions`, `respond_to_agent_question`, `list_agent_hooks`, `create_agent_hook`, `delete_agent_hook`
  - **ADK Sessions**: `list_adk_sessions`, `get_adk_session`
  - **MCP Servers** (gaps): `list_mcp_server_tools`, `configure_mcp_tool`
  - **Provider**: `configure_org_provider`, `configure_project_provider`, `list_provider_models`, `get_provider_usage`, `test_provider`
  - **Projects** (gaps): `set_project_info`, `list_project_api_tokens`, `create_project_api_token`, `revoke_project_api_token`
  - **Traces**: `list_traces`, `search_traces`, `get_trace`
  - **Query/Ask**: `query_knowledge` (agent-mode streaming), `search_knowledge` (vector search), `ask` (ask endpoint)
- Register all new tools in `GetToolDefinitions()` and route all new cases in `ExecuteTool()`
- Add e2e tests for at least one representative tool from each new domain group
- Update `mcp-guide` output in the CLI to reflect the expanded tool surface

## Capabilities

### New Capabilities
- `mcp-tools-full-coverage`: Every capability reachable via the `memory` CLI is now also reachable as an MCP tool, enabling AI agents to fully manage a Memory instance without shell access

### Modified Capabilities
- `mcp-service`: `GetToolDefinitions()` and `ExecuteTool()` expand to cover all CLI-equivalent operations; tool count grows from ~63 to ~100+

## Impact

- `apps/server/domain/mcp/service.go` — primary file for new tool definitions and handlers (may be split into `mcp/documents_tools.go`, `mcp/embeddings_tools.go`, etc. for readability)
- `apps/server/domain/mcp/` — new dependency injections: document store, embedding policy service, schema registry service, tracing client, provider config service
- `apps/server/domain/agents/repository.go` — `EnsureCliAssistantAgent` tool whitelist updated to include relevant new tools
- `apps/server/domain/mcp/module.go` — fx wiring for any new service dependencies
- E2E test files in the e2e test suite — new tests per domain group
- No DB schema changes required
- No breaking changes to existing tools
