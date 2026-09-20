## 1. Retire ACP server implementation

- [ ] 1.1 Delete `domain/agents/acp_handler.go`, `acp_routes.go`, `acp_dto.go` and their `_test.go` files; remove `RegisterACPRoutes` from `domain/agents/module.go`. Verify `go build ./...` compiles with no ACP handler references remaining.
- [ ] 1.2 Add a route test asserting `/acp/v1/` and `/agent-chat/v1/` are no longer registered (return 404). Verify the test passes.

## 2. Retire ACP SDK and CLI

- [ ] 2.1 Delete `pkg/sdk/acp/client.go`; grep for remaining importers and repoint them to `pkg/sdk/a2a`. Verify `go build ./...` compiles.
- [ ] 2.2 Delete `apps/cli/internal/cmd/acp.go` and unregister the `acp` command group from the CLI root command. Verify `memory acp` no longer resolves to a command group.

## 3. Retire acp-* MCP tools

- [ ] 3.1 Remove the `acp-list-agents`, `acp-trigger-run`, `acp-get-run-status`, `acp-get-run-events` tool definitions and handlers from `domain/agents/mcp_tools.go`. Verify the tool catalog unit test asserts no `acp-*` tool remains.
- [ ] 3.2 Add a unit test asserting `agent-list`, `trigger_agent`, `agent-run-status`, and `agent-run-messages`/`agent-run-tool-calls` cover the retired tools' behavior (no capability regression). Verify the test passes.

## 4. Rename ACPConfig → A2AConfig

- [ ] 4.1 Rename the `ACPConfig` type to `A2AConfig` and the `AgentDefinition.ACPConfig` field to `A2AConfig`; update `a2a_discovery.go` and the `AgentDefinitionDTO` references. Verify `go build ./...` compiles.
- [ ] 4.2 Rename the JSON tag `acpConfig` → `a2aConfig` and the bun tag `acp_config` → `a2a_config`, adding an `ALTER TABLE kb.agent_definitions RENAME COLUMN` migration. Verify a serialization unit test and a migration test pass.
- [ ] 4.3 Update the gateway (alfred/web-ui) and SDK consumers of the `acpConfig` wire field to `a2aConfig`. Verify the gateway builds and the agent-definition round-trip test passes.

## 5. Rename acpslug → agentslug

- [ ] 5.1 Rename `pkg/acpslug` to `pkg/agentslug` and update imports in `a2a_discovery.go` and the MCP domain. Verify `go build ./...` and `go test ./...` pass.

## 6. Verification

- [ ] 6.1 Run `go build ./...` and `go test ./...` across the server and CLI; fix any leftover ACP references. Verify green.
- [ ] 6.2 Run `task lint` and fix findings. Verify clean.
- [ ] 6.3 Run the A2A and MCP integration/e2e suites (agent discovery, message flow, per-agent MCP endpoint). Verify no regression.
