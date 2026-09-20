# Tasks — Retire ACP for A2A

## 1. Delete ACP protocol surface (server)

- [x] 1.1 Delete `apps/server/domain/agents/acp_handler.go`, `acp_dto.go`, `acp_routes.go`, `acp_handler_test.go`, `acp_dto_test.go`
- [x] 1.2 Delete the `apps/server/pkg/sdk/acp/` package (the whole directory)
- [x] 1.3 In `apps/server/domain/agents/module.go`, remove `provideACPHandler` (from `fx.Provide`), `RegisterACPRoutes` (from `fx.Invoke`), and the `provideACPHandler` constructor func

## 2. Delete ACP CLI

- [x] 2.1 Delete `apps/cli/internal/cmd/acp.go` (the `memory acp` command group registers itself via its own `rootCmd.AddCommand(acpCmd)`)

## 3. Re-point shared slug + remove dead metrics code

- [x] 3.1 In `apps/server/domain/agents/repository.go`, replace `ACPSlugFromName(...)` with `acpslug.FromName(...)` (2 call sites) and add the `pkg/acpslug` import
- [x] 3.2 Remove `Repository.GetAgentStatusMetrics` (the `AgentStatusMetrics` type it returns lives in the deleted `acp_dto.go`)

## 4. Remove `acp-*` MCP tools

- [x] 4.1 Remove the four `acp-*` tool definitions from `apps/server/domain/agents/mcp_tools.go` (`acp-list-agents`, `acp-trigger-run`, `acp-get-run-status`, `acp-get-run-events`)
- [x] 4.2 Remove the four `ExecuteACP*` methods from `MCPToolHandler` in `mcp_tools.go`
- [x] 4.3 Remove the four `ExecuteACP*` method signatures from the `AgentToolHandler` interface in `apps/server/domain/mcp/entity.go`
- [x] 4.4 Remove the four `acp-*` dispatch cases from `apps/server/domain/mcp/service.go`
- [x] 4.5 Remove the `acp-*` entries from `apps/server/domain/agentcompat/tool_namer.go`
- [x] 4.6 Remove the `ExecuteACP*` stub methods from `apps/server/domain/mcp/share_instance_test.go` and `relay_tools_test.go`
- [x] 4.7 Remove the three `TestRunToACPObject_*` tests from `apps/server/domain/agents/suspend_resume_test.go` (they exercise the deleted `RunToACPObject`)
- [x] 4.8 Remove the dead `"acp-trigger-run"` key from `agentExecutionTools` in `apps/server/domain/mcp/share_instance.go`

## 5. Relocate shared symbols (discovered during implementation)

- [x] 5.1 Move the still-used `ACPEvent*` run-event constants out of the deleted `acp_dto.go` into `entity.go` (10 kept, 4 unused dropped)
- [x] 5.2 Move the still-used `acpProjectID` helper out of the deleted `acp_handler.go` into `a2a_discovery.go`
- [x] 5.3 Inline the single-part text extraction in `agent_run_once.go` (replaces the deleted `memoryContentToACPParts`)
- [x] 5.4 Remove the 6 ACP endpoint entries from `blueprints/code-memory-blueprint/tools/graph-fix/main.go`

## 6. Swagger + docs

- [x] 6.1 Regenerate swagger docs and confirm the `domain_agents.ACP*` definitions and `/acp/v1` paths are gone

## 7. Spec retirement

- [x] 7.1 Retire the five ACP capability specs via `## REMOVED Requirements` deltas (authored)
- [x] 7.2 Update `a2a-acp-migration` with a `## MODIFIED Requirements` delta recording the removal milestone

## 8. Verification

- [x] 8.1 `go build ./...` from `apps/server` (no errors)
- [x] 8.2 `go build ./...` from `apps/cli`
- [x] 8.3 `go test ./domain/agents/... ./domain/mcp/... ./domain/agentcompat/...`
- [x] 8.4 `golangci-lint run ./...` (no new findings)
- [x] 8.5 Confirm no remaining references to `acp_handler`, `acp_dto`, `acp_routes`, `sdk/acp`, or `cmd/acp.go`; `pkg/acpslug` still resolves

## Deferred (out of scope — naming-cleanup follow-up)

- [ ] Rename `kb.acp_sessions` / `kb.acp_run_events` and the `ACPSession` / `ACPRunEvent` / `ACPSessionID` / `ACPConfig` identifiers
- [ ] Rename `pkg/acpslug`, `acpProjectID`, and the `ACPEvent*` constants
- [ ] Rename `acpTools` / `applyACPRestrictions` / `ToolNameACP*` in `toolpool.go` (live but misnamed)
