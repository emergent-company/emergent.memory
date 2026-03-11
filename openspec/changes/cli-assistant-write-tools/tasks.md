## 1. create_project MCP Tool

- [x] 1.1 Read `apps/server/domain/mcp/service.go` to understand the existing tool definition + execution pattern
- [x] 1.2 Add `create_project` to `GetToolDefinitions()` with `name` (required) and `org_id` (optional) parameters
- [x] 1.3 Add project store dependency to `mcp.Service` (inject `projects.Store` or use existing project service)
- [x] 1.4 Implement `create_project` case in `ExecuteTool()` — resolve org from context if `org_id` not supplied, call project store create, return `{id, name, orgId}`
- [x] 1.5 Build and verify no compile errors

## 2. cli-assistant-agent Write Tool Whitelist

- [x] 2.1 Read `apps/server/domain/agents/repository.go` `EnsureCliAssistantAgent` tool list
- [x] 2.2 Add graph write tools: `create_entity`, `update_entity`, `delete_entity`, `create_relationship`, `update_relationship`, `delete_relationship`
- [x] 2.3 Add agent definition write tools: `create_agent_definition`, `update_agent_definition`, `delete_agent_definition`
- [x] 2.4 Add runtime agent tools: `create_agent`, `update_agent`, `delete_agent`, `trigger_agent`
- [x] 2.5 Add schema tools: `create_schema`, `delete_schema`, `assign_schema`, `update_template_assignment`
- [x] 2.6 Add MCP registry tools: `create_mcp_server`, `update_mcp_server`, `delete_mcp_server`, `install_mcp_from_registry`, `sync_mcp_server_tools`
- [x] 2.7 Add `create_project` to the tool list

## 3. System Prompt Update

- [x] 3.1 Rewrite `cliAssistantAgentSystemPrompt` to describe the agent as an action-capable assistant
- [x] 3.2 Add guardrail instructions: describe action before executing write tools, warn before delete operations

## 4. e2e Tests

- [x] 4.1 Add `TestCLIInstalled_AskAgentCreateAgentDef` — ask agent to create an agent definition, verify it calls `create_agent_definition` tool and returns a definition ID, clean up via REST API
- [x] 4.2 Add `TestCLIInstalled_AskAgentCreateProject` — ask agent to create a project, verify `create_project` tool is called and response contains a project ID, clean up via REST API
- [x] 4.3 Run all `TestCLIInstalled_Ask*` tests and confirm they pass

## 5. Bug Fix — Org ID Resolution for Standalone/Account-Mode Auth

- [x] 5.1 Fix `executeCreateProject` in `apps/server/domain/mcp/service.go`: when `auth.OrgIDFromContext(ctx)` returns empty, fall back to querying `kb.projects WHERE id = projectIDFromContext` to resolve org
- [x] 5.2 Update `TestCLIInstalled_AskAgentCreateProject` in e2e suite: add step 5 to verify the project actually exists via REST API (`GET /api/projects` by name), register cleanup for agent-created project
- [x] 5.3 Build server and e2e suite — no compile errors

