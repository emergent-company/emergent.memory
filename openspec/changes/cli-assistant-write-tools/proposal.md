## Why

The `cli-assistant-agent` is currently read-only — it can answer questions about the Memory platform but cannot take actions on the user's behalf. Since the MCP system already has 40+ write tools covering agent definitions, agents, graph entities, schemas, and MCP servers, enabling the assistant to manage the instance is largely a matter of adding those tools to its whitelist and updating its system prompt. The one missing piece is a `create_project` MCP tool (projects are currently REST-only), which blocks the most common setup flow.

## What Changes

- Add `create_project` MCP tool to `apps/server/domain/mcp/service.go` so the assistant can create projects on behalf of the user
- Expand `EnsureCliAssistantAgent` tool list in `apps/server/domain/agents/repository.go` to include write tools across graph, agents, schemas, and MCP registry
- Update `cliAssistantAgentSystemPrompt` to reflect that the agent can take actions, not just answer questions, and add guardrails (confirm before destructive operations)
- Add e2e tests covering the agent actually executing write actions: creating an agent definition and creating a project

## Capabilities

### New Capabilities
- `cli-assistant-manage`: The cli-assistant-agent can now manage the Memory instance — create/update/delete agent definitions, agents, entities, relationships, schemas, and MCP servers, and create projects

### Modified Capabilities
- `graph-query-agent`: The `create_project` MCP tool is a shared addition to the MCP service, also available to any agent that lists it

## Impact

- `apps/server/domain/mcp/service.go` — new `create_project` tool handler + definition
- `apps/server/domain/agents/repository.go` — expanded tool list + updated system prompt for `cli-assistant-agent`
- `apps/server/domain/chat/handler.go` — no changes needed (auth context already flows through)
- `/root/emergent.memory.e2e/ask_test.go` — new tests: `TestCLIInstalled_AskAgentCreateAgentDef`, `TestCLIInstalled_AskAgentCreateProject`
- No DB migrations required (tool list is stored as a Postgres array column in `kb.agent_definitions`; the `EnsureCliAssistantAgent` upsert updates it)
- No breaking changes
