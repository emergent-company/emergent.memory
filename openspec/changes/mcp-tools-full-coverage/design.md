## Context

The MCP service (`apps/server/domain/mcp/service.go`) is the single registration point for every MCP tool. It exposes two methods: `GetToolDefinitions()` returning a slice of tool schemas, and `ExecuteTool(ctx, name, params)` dispatching by name. The current ~63 tools cover graph CRUD (entities, relationships), agent definitions, runtime agents, schema assignment helpers, and a handful of project/org read tools.

The CLI (`tools/cli/`) is organized into command groups that map one-to-one to API domains. Reading CLI source against server routes reveals exactly which endpoints are uncovered. The goal is to fill that gap so an MCP-connected agent has identical capability to a CLI user.

## Goals / Non-Goals

**Goals:**
- Add one MCP tool per meaningful CLI operation that has no equivalent MCP tool today
- Keep tool naming consistent with existing conventions: `{verb}_{noun}` in snake_case (e.g., `list_documents`, `get_trace`)
- Reuse existing service/store dependencies where possible; add new fx injections only where necessary
- Add at least one e2e test per new domain group to guard against regressions

**Non-Goals:**
- Replacing the CLI — MCP tools and CLI commands are complementary
- Streaming SSE tools — MCP tools return complete JSON responses; `query_knowledge` and `ask` collect the full streamed response before returning
- UI changes — no frontend work required
- Auth model changes — existing JWT + project scope middleware applies unchanged

## Tool Inventory Gap Analysis

### Already Covered (do not re-add)

These tools already exist across `mcp/service.go`, `agents/mcp_tools.go`, and `mcpregistry/mcp_tools.go`:

- Graph CRUD: `create_entity`, `update_entity`, `delete_entity`, `restore_entity`, `query_entities`, `search_entities`, `get_entity_edges`, `create_relationship`, `update_relationship`, `delete_relationship`, `list_relationships`, `list_tags`
- Graph search: `hybrid_search`, `semantic_search`, `find_similar`, `traverse_graph`
- Schema/template packs (full CRUD already present): `list_schemas`, `get_schema`, `get_available_templates`, `get_installed_templates`, `assign_schema`, `update_template_assignment`, `create_schema`, `delete_schema`, `uninstall_schema`, `preview_schema_migration`, `list_migration_archives`, `get_migration_archive`
- Agents (runtime): `list_agents`, `get_agent`, `create_agent`, `update_agent`, `delete_agent`, `trigger_agent`, `list_agent_runs`, `get_agent_run`, `get_agent_run_messages`, `get_agent_run_tool_calls`, `get_run_status`, `list_available_agents`
- Agent definitions: `list_agent_definitions`, `get_agent_definition`, `create_agent_definition`, `update_agent_definition`, `delete_agent_definition`
- MCP servers: `list_mcp_servers`, `get_mcp_server`, `create_mcp_server`, `update_mcp_server`, `delete_mcp_server`, `toggle_mcp_server_tool`, `sync_mcp_server_tools`, `search_mcp_registry`, `get_mcp_registry_server`, `install_mcp_from_registry`, `inspect_mcp_server`
- Projects: `get_project_info`, `create_project`, `schema_version`
- Web tools: `brave_web_search`, `webfetch`, `reddit_search`

### Gaps to Fill

Skills and ADK sessions have zero MCP coverage. Agent questions/hooks, documents, embeddings, provider config, API tokens, traces, and the query/knowledge pipeline are also absent.

| Group | New Tools | API Endpoint |
|---|---|---|
| Skills | `list_skills` | `GET /api/projects/:projectId/skills` |
| Skills | `get_skill` | `GET /api/skills/:id` |
| Skills | `create_skill` | `POST /api/projects/:projectId/skills` |
| Skills | `update_skill` | `PATCH /api/skills/:id` |
| Skills | `delete_skill` | `DELETE /api/skills/:id` |
| Documents | `list_documents` | `GET /api/documents` |
| Documents | `get_document` | `GET /api/documents/:id` |
| Documents | `upload_document` | `POST /api/documents/upload` (multipart) |
| Documents | `delete_document` | `DELETE /api/documents/:id` |
| Embeddings | `get_embedding_status` | `GET /api/embeddings/status` |
| Embeddings | `pause_embeddings` | `POST /api/embeddings/pause` |
| Embeddings | `resume_embeddings` | `POST /api/embeddings/resume` |
| Embeddings | `update_embedding_config` | `PATCH /api/embeddings/config` |
| Agent questions | `list_agent_questions` | `GET /api/projects/:projectId/agent-runs/:runId/questions` |
| Agent questions | `list_project_agent_questions` | `GET /api/projects/:projectId/agent-questions` |
| Agent questions | `respond_to_agent_question` | `POST /api/projects/:projectId/agent-questions/:questionId/respond` |
| Agent hooks | `list_agent_hooks` | `GET /api/admin/agents/:agentId/hooks` |
| Agent hooks | `create_agent_hook` | `POST /api/admin/agents/:agentId/hooks` |
| Agent hooks | `delete_agent_hook` | `DELETE /api/admin/agents/:agentId/hooks/:hookId` |
| ADK sessions | `list_adk_sessions` | `GET /api/projects/:projectId/adk-sessions` |
| ADK sessions | `get_adk_session` | `GET /api/projects/:projectId/adk-sessions/:sessionId` |
| Provider | `list_org_providers` | `GET /api/v1/organizations/:orgId/providers` |
| Provider | `configure_org_provider` | `PUT /api/v1/organizations/:orgId/providers/:provider` |
| Provider | `configure_project_provider` | `PUT /api/v1/projects/:projectId/providers/:provider` |
| Provider | `list_provider_models` | `GET /api/v1/providers/:provider/models` |
| Provider | `get_provider_usage` | `GET /api/v1/organizations/:orgId/usage` |
| Provider | `test_provider` | `POST /api/v1/providers/:provider/test` |
| API tokens | `list_project_api_tokens` | `GET /api/projects/:projectId/tokens` |
| API tokens | `create_project_api_token` | `POST /api/projects/:projectId/tokens` |
| API tokens | `get_project_api_token` | `GET /api/projects/:projectId/tokens/:tokenId` |
| API tokens | `revoke_project_api_token` | `DELETE /api/projects/:projectId/tokens/:tokenId` |
| Traces | `list_traces` | `GET /api/traces` (proxied to Tempo) |
| Traces | `get_trace` | `GET /api/traces/:traceId` (proxied to Tempo) |
| Query | `query_knowledge` | `POST /api/projects/:projectId/query` (SSE, collect) |

## Decisions

### 1. Tool file organization — split by domain

`mcp/service.go` currently has 4,485 lines. Adding ~35 more tools inline would make it unmaintainable. Split new tools into domain-specific files:
- `mcp/documents_tools.go` (documents CRUD + upload)
- `mcp/embeddings_tools.go` (embedding status/pause/resume/config)
- `mcp/agent_ext_tools.go` (agent questions, hooks, ADK sessions — extends existing `agents/mcp_tools.go` pattern)
- `mcp/provider_tools.go` (provider config, models, usage, test)
- `mcp/token_tools.go` (project API tokens)
- `mcp/trace_tools.go` (traces via Tempo proxy)
- `mcp/query_tools.go` (query_knowledge SSE collection)
- `mcp/skills_tools.go` (skills CRUD)

`service.go` remains the hub: its `GetToolDefinitions()` calls `allToolDefinitions()` which aggregates from each file; `ExecuteTool()` dispatches via a unified `switch`.

Alternative: keep everything in `service.go`. Rejected — file would exceed 3,000 lines.

### 2. SSE streaming tools (`query_knowledge`, `ask`) — collect-and-return

The CLI streams SSE events to the terminal. MCP tools must return a complete JSON value. The tool implementation will open an SSE connection, accumulate all `data:` events, close the connection, and return the assembled text as a single `content` string. Timeout: 60 seconds.

Alternative: expose a polling pattern (submit job → poll for result). Rejected — existing `/query` and `/ask` endpoints are SSE-only; adding a polling layer requires server changes.

### 3. `upload_document` — base64 body, not multipart

MCP tool parameters are JSON. The tool will accept `content_base64` (base64-encoded file bytes) and `filename`, then POST multipart/form-data to the server. This keeps the MCP schema clean (no binary transport) while reusing the existing upload endpoint.

Alternative: accept a local file path and read from disk. Rejected — MCP tools run server-side; the agent doesn't have access to the client's filesystem.

### 4. New service dependencies

The MCP service currently injects only `graph.Service`, `search.Service`, `config.Config`, and `slog.Logger`. Agent tools use a handler-injection pattern via `SetAgentToolHandler`. The following new dependencies are needed:

- `*documents.Service` — documents CRUD + upload
- `*chunking.Service` or embedding worker controller — embedding status/pause/resume/config (routes are at `/api/embeddings/*`, handled by the chunking or scheduler domain — verify the actual handler during implementation)
- `*agents.Repository` or reuse `AgentToolHandler` — agent questions, hooks, ADK sessions are already on the agents handler
- `*provider.CredentialService` + `*provider.ModelCatalogService` — provider config and models
- `*apitoken.Service` — project API tokens
- `*tracing.Handler` or the Tempo base URL from config — traces proxy
- `*skills.Service` — skills CRUD

All are fx-provided singletons. Agent/questions/hooks/ADK routes are on the same `agents.Handler` already wired; the MCP service can call the agent service layer directly rather than go through HTTP.

### 5. Tool naming — exact mapping to CLI command names

Existing tools use verbs like `create_`, `update_`, `delete_`, `list_`, `get_`. New tools follow the same pattern. Where CLI uses composite verbs (e.g., `resume_embeddings`, `pause_embeddings`), the tool name mirrors that exactly to aid discoverability.

## Risks / Trade-offs

- [Risk] `upload_document` base64 payload can be large for big files → Mitigation: document a 10 MB size limit in the tool description; return a clear error if exceeded.
- [Risk] `query_knowledge` SSE collection adds latency → Mitigation: 60-second timeout with partial-result return on timeout; document in tool description.
- [Risk] Provider `configure_*` tools expose sensitive API keys → Mitigation: keys pass through the existing server-side encrypted storage; the MCP tool description notes that keys are write-only and never returned.
- [Risk] Many new fx dependencies in `mcp.Module` could introduce circular imports → Mitigation: all new dependencies are already wired into other modules; no new packages are being created.

## Migration Plan

1. No DB migrations — all new tools are API pass-throughs.
2. Deploy updated server — new tools appear in `GetToolDefinitions()` immediately on next MCP client connection.
3. Rollback: remove new tool cases from `ExecuteTool()` switch; tools silently disappear from the list on next reconnect.
