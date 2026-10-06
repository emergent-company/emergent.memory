## 1. Server — remove flowType from model and DTO

- [x] 1.1 Remove `FlowType` from the `AgentDefinition` entity and summary struct in `apps/server/domain/agents/`, and drop the `flow_type` JSON tags. Verify: `go build ./...` compiles.
- [x] 1.2 Remove `flowType` from the create/update request DTOs and their validation. Verify: `go build ./...` compiles and agent CRUD unit tests pass.
- [x] 1.3 Remove `flowType` from any MCP tool schema and the `ask_user` `agent` proposal body shape that emits it. Verify: schema/agent unit tests pass.

## 2. Migration

- [x] 2.1 Add `apps/server/migrations/00221_drop_agent_flow_type.sql`: Up drops `kb.agent_definitions.flow_type`; Down re-adds it as `VARCHAR(50) NOT NULL DEFAULT 'single'`. Verify: `task migrate:up` then `task migrate:status` shows it applied, and `\d kb.agent_definitions` has no `flow_type`.

## 3. CLI — remove --flow-type flag and output

- [x] 3.1 Remove the `--flow-type` flag from `agent-definitions create` and `update`, and remove `FlowType` from `list`/`get` output. Verify: `go build ./...` and CLI flag tests pass.

## 4. Gateway + SDK clients

- [x] 4.1 Remove `FlowType` from the gateway agent-definition client and any templ that renders the dashboard summary. Verify: `templ generate`, `go build ./...`, and `go test ./...` in `gateway/` pass.
- [x] 4.2 Remove `FlowType` from `apps/server/pkg/sdk/agentdefinitions` types and regenerate the reference doc. Verify: `go build ./...` in the SDK module.

## 5. Blueprints

- [x] 5.1 Stop emitting and validating `flowType` in blueprint agent definitions and the `blueprints validate` enum checks. Verify: blueprint unit tests pass.

## 6. Docs, skills, and e2e

- [x] 6.1 Remove `flowType`/`--flow-type` references from docs (`docs/guides`, `docs/site`, `docs/plans`, `docs/features`, `docs/integrations`, `apps/web-ui/docs/spec`), leaving the `overflowType` false positive in `docs/spec/19-dynamic-object-graph.md` and the historical `docs/features/multi-agent-coordination/**` untouched. Verify: `grep -rn -i "flowtype\|flow_type\|flow-type" docs e2e .opencode/skills .agents/skills` returns only those two exclusions.
- [x] 6.2 Remove `flowType`/`--flow-type`/`flow_type` from agent skills (`.opencode/skills/blueprint-creator`, `.agents/skills/blueprint-creator`, `.agents/skills/memory-blueprints`, `.agents/skills/memory-cli-reference`). Verify: same grep is clean.
- [x] 6.3 Update e2e tests (`e2e/tests/api/agents_test.go`, `e2e/tests/cli/agents_test.go`, `e2e/tests/docs/cli_help_test.go`, `e2e/tests/docs/cli_commands_test.go`) to drop `flowType`/`--flow-type`. Verify: e2e suite compiles.

## 7. Spec

- [x] 7.1 Add `openspec/changes/remove-agent-flow-type/specs/agent-dashboard-ui/spec.md` and `.../agent-proposals/spec.md` MODIFIED deltas. Verify: `openspec validate remove-agent-flow-type` passes.
