## ADDED Requirements

### Requirement: Document Management MCP Tools

The MCP service SHALL expose tools to manage documents in the knowledge base, mirroring the `memory documents` CLI command group.

Tools: `list_documents`, `get_document`, `upload_document`, `delete_document`

`upload_document` SHALL accept `content_base64` (base64-encoded file bytes, required) and `filename` (required). It SHALL reject payloads whose decoded size exceeds 10 MB with a descriptive error.

#### Scenario: Agent lists documents
- **WHEN** the agent calls `list_documents` (with the project ID in context)
- **THEN** the tool returns an array of documents with `id`, `title`, `status`, and `createdAt`

#### Scenario: Agent uploads a document
- **WHEN** the agent calls `upload_document` with `content_base64` and `filename`
- **THEN** the tool posts the file to the server and returns the created document `id`, `title`, and `status`

#### Scenario: upload_document rejects oversized payload
- **WHEN** the agent calls `upload_document` with a `content_base64` value that decodes to more than 10 MB
- **THEN** the tool returns an error: `"file too large: decoded size exceeds 10 MB limit"`

#### Scenario: Agent deletes a document
- **WHEN** the agent calls `delete_document` with a valid `id`
- **THEN** the document is removed and the tool returns `{"deleted": true}`

---

### Requirement: Embedding Control MCP Tools

The MCP service SHALL expose tools to inspect and control the embedding pipeline, mirroring the `memory embeddings` CLI command group.

Tools: `get_embedding_status`, `pause_embeddings`, `resume_embeddings`, `get_embedding_config`, `update_embedding_config`

#### Scenario: Agent checks embedding status
- **WHEN** the agent calls `get_embedding_status`
- **THEN** the tool returns the current pipeline status including queue depth, running state, and last processed timestamp

#### Scenario: Agent pauses embeddings
- **WHEN** the agent calls `pause_embeddings`
- **THEN** the embedding pipeline is paused and the tool returns `{"paused": true}`

#### Scenario: Agent updates embedding config
- **WHEN** the agent calls `update_embedding_config` with a `model` parameter
- **THEN** the embedding configuration is updated and the tool returns the updated config object

---

### Requirement: Skills MCP Tools

The MCP service SHALL expose tools to manage skills, mirroring the `memory skills` CLI command group. Skills are currently completely absent from MCP.

Tools: `list_skills`, `get_skill`, `create_skill`, `update_skill`, `delete_skill`

`list_skills` SHALL accept an optional `project_id` parameter to filter skills to a specific project. Without `project_id`, it returns global and org-level skills visible to the caller.

#### Scenario: Agent lists skills available to a project
- **WHEN** the agent calls `list_skills` with a `project_id`
- **THEN** the tool returns an array of skill objects with `id`, `name`, `description`, and `type`

#### Scenario: Agent creates a skill
- **WHEN** the agent calls `create_skill` with `name`, `description`, and skill configuration
- **THEN** the skill is created and the tool returns the created skill `id` and `name`

#### Scenario: Agent deletes a skill
- **WHEN** the agent calls `delete_skill` with a valid `id`
- **THEN** the skill is removed and the tool returns `{"deleted": true}`

---

### Requirement: Agent Question and Hook MCP Tools

The MCP service SHALL expose tools to interact with agent questions (human-in-the-loop) and manage agent webhook hooks.

Question tools: `list_agent_questions`, `list_project_agent_questions`, `respond_to_agent_question`
Hook tools: `list_agent_hooks`, `create_agent_hook`, `delete_agent_hook`

Note: `list_agent_runs` and `get_agent_run` are already implemented in `agents/mcp_tools.go`.

#### Scenario: Agent responds to a pending question
- **WHEN** an agent run is paused awaiting human input
- **AND** the agent calls `respond_to_agent_question` with `question_id` and `response`
- **THEN** the question is answered and the run resumes

---

### Requirement: Agent Hook MCP Tools

The MCP service SHALL expose tools to manage webhook hooks on agents, mirroring `memory agents hooks`.

Tools: `list_agent_hooks`, `create_agent_hook`, `delete_agent_hook`

#### Scenario: Agent creates a webhook hook
- **WHEN** the agent calls `create_agent_hook` with `agent_id`, `url`, and optional `secret`
- **THEN** the hook is registered and the tool returns the hook `id` and `url`

#### Scenario: Agent deletes a hook
- **WHEN** the agent calls `delete_agent_hook` with `agent_id` and `hook_id`
- **THEN** the hook is removed and the tool returns `{"deleted": true}`

---

### Requirement: ADK Session MCP Tools

The MCP service SHALL expose tools to inspect ADK sessions, mirroring the `memory adk-sessions` CLI command group.

Tools: `list_adk_sessions`, `get_adk_session`

#### Scenario: Agent lists ADK sessions
- **WHEN** the agent calls `list_adk_sessions`
- **THEN** the tool returns an array of session objects with `id`, `agentId`, `status`, and `createdAt`

#### Scenario: Agent retrieves a single ADK session
- **WHEN** the agent calls `get_adk_session` with a `session_id`
- **THEN** the tool returns the full session object including messages

---

### Requirement: Provider Configuration MCP Tools

The MCP service SHALL expose tools to manage AI provider configuration, mirroring the `memory ai provider` CLI command group.

Tools: `list_org_providers`, `configure_org_provider`, `configure_project_provider`, `list_provider_models`, `get_provider_usage`, `test_provider`

Routes are under `/api/v1/organizations/:orgId/providers` and `/api/v1/projects/:projectId/providers`.

`configure_org_provider` and `configure_project_provider` SHALL accept provider-specific config. API keys SHALL be write-only and SHALL NOT be returned in any response.

#### Scenario: Agent configures a provider for an org
- **WHEN** the agent calls `configure_org_provider` with `provider` (e.g., `"openai"`) and `config` (API key, model settings)
- **THEN** the provider configuration is saved and the tool returns `{"configured": true, "provider": "<name>"}`

#### Scenario: Agent lists available models
- **WHEN** the agent calls `list_provider_models` with a `provider` name
- **THEN** the tool returns an array of model identifiers available for that provider

#### Scenario: Agent tests a provider connection
- **WHEN** the agent calls `test_provider` with a `provider` name
- **THEN** the tool returns `{"ok": true}` if the provider responds, or `{"ok": false, "error": "<message>"}` on failure

---

### Requirement: Project API Token MCP Tools

The MCP service SHALL expose tools to manage project-scoped API tokens, mirroring the `memory account tokens` CLI command group.

Tools: `list_project_api_tokens`, `create_project_api_token`, `get_project_api_token`, `revoke_project_api_token`

Routes are under `/api/projects/:projectId/tokens`.

Token values SHALL only be returned at creation time. Subsequent reads (`get_project_api_token`) SHALL return metadata only (id, name, createdAt, lastUsedAt) — never the raw token value.

#### Scenario: Agent creates a project API token
- **WHEN** the agent calls `create_project_api_token` with a `name`
- **THEN** the tool returns `{"id": "...", "name": "...", "token": "<raw-value>"}` — the only time the raw value is available

#### Scenario: Agent revokes a token
- **WHEN** the agent calls `revoke_project_api_token` with a `token_id`
- **THEN** the token is deleted and the tool returns `{"revoked": true}`

---

### Requirement: Trace Inspection MCP Tools

The MCP service SHALL expose tools to search and retrieve distributed traces, mirroring the `memory traces` CLI command group.

Tools: `list_traces`, `get_trace`

Routes are proxied to Tempo at `GET /api/traces` (search with query params) and `GET /api/traces/:id`.

#### Scenario: Agent lists recent traces
- **WHEN** the agent calls `list_traces` with optional `since` (duration string, e.g. `"30m"`) and `limit` parameters
- **THEN** the tool returns an array of trace summaries with `traceId`, `rootSpanName`, `duration`, and `startTime`

#### Scenario: Agent retrieves full trace span tree
- **WHEN** the agent calls `get_trace` with a `trace_id`
- **THEN** the tool returns the full span tree for that trace

---

### Requirement: Knowledge Query MCP Tool

The MCP service SHALL expose a `query_knowledge` tool that submits a question to the project's agent-mode query pipeline and returns the complete answer, mirroring `memory query --agent`.

The tool SHALL collect the full SSE response before returning. It SHALL enforce a 60-second timeout and SHALL return a `"truncated": true` field alongside any partial content if the timeout is reached.

#### Scenario: Agent queries the knowledge graph
- **WHEN** the agent calls `query_knowledge` with `question: "What entities are in the graph?"`
- **THEN** the tool streams the SSE response internally, assembles the complete answer, and returns `{"answer": "<text>", "truncated": false}`

#### Scenario: query_knowledge times out
- **WHEN** the SSE stream is still open after 60 seconds
- **THEN** the tool closes the connection and returns `{"answer": "<partial text>", "truncated": true}`

---

### Requirement: cli-assistant-agent tool whitelist includes new tools

The `cli-assistant-agent` tool whitelist SHALL be updated to include the following new tools so the assistant can use them on the user's behalf:

- `list_documents`, `get_document`, `upload_document`, `delete_document`
- `get_embedding_status`, `pause_embeddings`, `resume_embeddings`
- `list_skills`, `get_skill`, `create_skill`, `update_skill`, `delete_skill`
- `list_agent_questions`, `list_project_agent_questions`, `respond_to_agent_question`
- `list_adk_sessions`, `get_adk_session`
- `list_traces`, `get_trace`
- `query_knowledge`

The following new tools SHALL NOT be added to the assistant whitelist due to elevated privilege:
- `configure_org_provider`, `configure_project_provider` (mutates billing-sensitive API keys)
- `list_org_providers`, `get_provider_usage`, `test_provider` (read-only but sensitive)
- `create_project_api_token`, `revoke_project_api_token` (token lifecycle management)
- `update_embedding_config` (infrastructure-level change)
- `create_agent_hook`, `delete_agent_hook` (webhook registration)

#### Scenario: Assistant uses list_documents when asked about uploaded files
- **WHEN** the user asks "what documents have I uploaded?"
- **THEN** the assistant calls `list_documents` and summarizes the results

#### Scenario: Assistant uses query_knowledge to answer domain questions
- **WHEN** the user asks a question about the contents of their knowledge base
- **THEN** the assistant calls `query_knowledge` and returns the answer from the graph agent

#### Scenario: Assistant uses list_skills when asked what skills are available
- **WHEN** the user asks "what skills are configured?"
- **THEN** the assistant calls `list_skills` and returns the skill names and descriptions
