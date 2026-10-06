## Why

The `flowType` field on agent definitions is dead: it has no runtime consumer —
every agent runs the same ADK ReAct loop regardless of the stored value. Worse,
its vocabulary disagrees across surfaces. The server SDK examples use `adk`
and `single`; the CLI documents `single`/`multi`/`coordinator`; the gateway and
blueprints use `single`; and blueprint examples carry `conversational` and
`agentic`. A field that is never read, and whose allowed values contradict each
other across clients, is pure drift — it confuses users, bloats payloads, and
blocks clean blueprint/CLI validation.

## What Changes

- Remove `flowType` from the agent definition model, DTOs, REST payloads, and
  the CLI's `--flow-type` flag.
- Drop the `kb.agent_definitions.flow_type` column via migration.
- Remove `flowType` from the `agent` proposal payload, the dashboard summary,
  and every docs/skills/e2e reference.

## Capabilities

### New Capabilities
<!-- none -->

### Modified Capabilities

- `agent-dashboard-ui`: the agent summary no longer displays a "flow type".
- `agent-proposals`: the `agent` proposal kind no longer carries `flowType`.

## Impact

- **Server** (`apps/server/domain/agents/`): drop `FlowType` from the entity,
  summary struct, request/response DTOs, and validation.
- **MCP** (`apps/server/domain/mcp/`): drop `flowType` from any tool schema that
  emits it, including the `ask_user` `agent` proposal body shape.
- **Migration** (`apps/server/migrations/`): drop `flow_type`.
- **CLI** (`apps/cli/`): remove the `--flow-type` flag and `FlowType` output
  fields.
- **Gateway** (`apps/web-ui/gateway/`): drop `FlowType` from the agent-definition
  client and any templ that renders the dashboard summary.
- **SDK** (`apps/server/pkg/sdk/agentdefinitions`): drop `FlowType` from the
  client types and regenerate the reference doc.
- **Blueprints** (`blueprints/`): stop emitting and validating `flowType`.
- **Docs / skills / e2e**: remove `flowType`/`--flow-type`/`flow_type` mentions.
- **Specs**: `agent-dashboard-ui` and `agent-proposals` synced via MODIFIED deltas.
