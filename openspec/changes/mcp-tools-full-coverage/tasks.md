<!-- Baseline failures (pre-existing, not introduced by this change):
- apps/server/pkg/tracing/tracer_test.go: compile error — undefined tracing.StartLinked, tracing.RecordErrorWithType
- emergent.memory.e2e/framework/*.go: multiple undefined symbols (e2e framework issues)
-->

## 1. Preparation

- [x] 1.1 Read `apps/server/domain/mcp/service.go` fully — note existing tool patterns, service struct fields, fx wiring
- [x] 1.2 Read `apps/server/domain/mcp/module.go` — note current fx `Provide` and `Invoke` calls
- [x] 1.3 Read handler/service files for each new domain: documents, agents (questions/hooks/ADK sessions), skills, provider, apitoken, tracing
- [x] 1.4 Identify the handler that serves `/api/embeddings/status|pause|resume|config` (likely in chunking or scheduler domain) and the exact service type
- [x] 1.5 Confirm which services can be injected directly vs which require the existing handler-injection pattern (like `SetAgentToolHandler`)

## 2. New Tool Files

- [x] 2.1 Create `apps/server/domain/mcp/skills_tools.go` — add `list_skills`, `get_skill`, `create_skill`, `update_skill`, `delete_skill`
- [x] 2.2 Create `apps/server/domain/mcp/documents_tools.go` — add `list_documents`, `get_document`, `upload_document`, `delete_document`
- [x] 2.3 Create `apps/server/domain/mcp/embeddings_tools.go` — add `get_embedding_status`, `pause_embeddings`, `resume_embeddings`, `update_embedding_config`
- [x] 2.4 Create `apps/server/domain/mcp/agent_ext_tools.go` — add `list_agent_questions`, `list_project_agent_questions`, `respond_to_agent_question`, `list_agent_hooks`, `create_agent_hook`, `delete_agent_hook`, `list_adk_sessions`, `get_adk_session`
- [x] 2.5 Create `apps/server/domain/mcp/provider_tools.go` — add `list_org_providers`, `configure_org_provider`, `configure_project_provider`, `list_provider_models`, `get_provider_usage`, `test_provider`
- [x] 2.6 Create `apps/server/domain/mcp/token_tools.go` — add `list_project_api_tokens`, `create_project_api_token`, `get_project_api_token`, `revoke_project_api_token`
- [x] 2.7 Create `apps/server/domain/mcp/trace_tools.go` — add `list_traces`, `get_trace` (proxy to Tempo via config URL)
- [x] 2.8 Create `apps/server/domain/mcp/query_tools.go` — add `query_knowledge` (SSE collect pattern)

## 3. Service Struct and fx Wiring

- [x] 3.1 Add `*documents.Service` to `mcp.Service` struct and `module.go` fx injection
- [x] 3.2 Add embedding worker controller (identify exact type in step 1.4) to `mcp.Service` struct and `module.go`
- [x] 3.3 Add `*skills.Service` to `mcp.Service` struct and `module.go`
- [x] 3.4 Extend agent tool access for questions/hooks/ADK — either inject `*agents.Repository` or extend `AgentToolHandler` interface
- [x] 3.5 Add `*provider.CredentialService` and `*provider.ModelCatalogService` to `mcp.Service` struct and `module.go`
- [x] 3.6 Add `*apitoken.Service` to `mcp.Service` struct and `module.go`
- [x] 3.7 Add Tempo base URL (from `config.Config`) to `mcp.Service` for trace proxy calls

## 4. GetToolDefinitions() and ExecuteTool() Updates

- [x] 4.1 Register all new tool definitions in `GetToolDefinitions()`
- [x] 4.2 Add all new tool name cases to the `ExecuteTool()` switch
- [x] 4.3 Build — confirm zero compile errors: `task build`

## 5. upload_document Implementation Detail

- [x] 5.1 Accept `content_base64` (string) and `filename` (string) parameters
- [x] 5.2 Decode base64 to bytes; reject if decoded size > 10 MB with descriptive error
- [x] 5.3 Construct multipart/form-data body and call documents service upload method
- [x] 5.4 Return created document `id`, `title`, `status`

## 6. query_knowledge SSE Collection

- [x] 6.1 Accept `question` (required string) and optional `mode` parameters
- [x] 6.2 POST to `/api/projects/:projectId/query` with SSE transport; collect all `data:` event lines
- [x] 6.3 Assemble collected lines into a single response string; enforce 60-second timeout
- [x] 6.4 On timeout, return partial result with a `"truncated": true` field

## 7. cli-assistant-agent Tool Whitelist Update

- [x] 7.1 Read `apps/server/domain/agents/repository.go` `EnsureCliAssistantAgent` tool list
- [x] 7.2 Add to whitelist: `list_documents`, `get_document`, `upload_document`, `delete_document`, `get_embedding_status`, `pause_embeddings`, `resume_embeddings`, `list_skills`, `get_skill`, `create_skill`, `update_skill`, `delete_skill`, `list_agent_questions`, `list_project_agent_questions`, `respond_to_agent_question`, `list_adk_sessions`, `get_adk_session`, `list_traces`, `get_trace`, `query_knowledge`
- [x] 7.3 Do NOT add privilege-sensitive tools: `configure_org_provider`, `configure_project_provider`, `create_project_api_token`, `revoke_project_api_token`, `update_embedding_config`, `create_agent_hook`, `delete_agent_hook`

## 8. Testing

- [x] 8.1 Add e2e test `TestMCP_ListDocuments` — call `list_documents`, verify array response
- [x] 8.2 Add e2e test `TestMCP_ListSkills` — call `list_skills`, verify array response
- [x] 8.3 Add e2e test `TestMCP_GetEmbeddingStatus` — call `get_embedding_status`, verify status fields
- [x] 8.4 Add e2e test `TestMCP_ListADKSessions` — call `list_adk_sessions`, verify array response
- [x] 8.5 Add e2e test `TestMCP_GetTrace` — call `list_traces`, use first trace ID to call `get_trace` (skip if Tempo not configured in test env)
- [x] 8.6 Add e2e test `TestMCP_QueryKnowledge` — call `query_knowledge` with a simple question, verify non-empty response
- [x] 8.7 Run all MCP e2e tests and confirm they pass
