## Why

The agent settings surface has subpages for General, Graph, Model, Tools, Skills,
Delegation, and MCP sharing — but no way to edit an agent's object-driven **work**
configuration. `kb.agent_definitions.work_config` already carries the full shape
(status phase mapping, review gate, failure/revision budgets, retry backoff,
deliverable contract, and trigger statuses), and the server already reads and
writes it through the definition DTO. The gateway exposes it read-only to the
board (`boardStatusMapFromDefinitions` consumes only `workConfig.status`), so the
only way to change an agent's work behaviour today is through the API.

Operators configuring a work-driven agent (which statuses mean ready/review/done,
how many failures before escalation, what the agent must produce before it may
complete an item, which status transitions wake it) need a first-class Settings →
Work subpage, matching the pattern of the existing Graph/Tools/Skills subpages.

## What Changes

- Add a **Work** settings subpage (`/agents/:id/settings/work`) with a PRG form
  covering all seven work-config field groups: the six status phases, the review
  gate, failure and revision budgets, retry backoff, the deliverable contract,
  and the trigger statuses that wake the agent.
- Map the form onto the existing `AgentDefinition.WorkConfig` field via a new
  section applier (`applyAgentWorkSection`); empty status names and zero limits
  read as "use the platform default" (the server's existing resolver semantics).
- Expose `sourceBlueprintId` on the agent definition read DTO so the gateway can
  warn before a divergent edit to a blueprint-derived agent, reusing the
  existing `data-requires-confirm` edit gate ("Save anyway").
- Add gateway + server tests (applier mapping, form rendering, sanitize
  preservation, and a full-workConfig DB round-trip) and a Playwright spec.

## Capabilities

### Modified Capabilities

- `web-agent-settings`: new Work subpage with the seven field groups, default
  placeholders, numeric bounds, and the blueprint-derived edit gate.

## Impact

- **Server** (`apps/server/domain/agents/`): `dto.go` exposes `sourceBlueprintId`
  on `AgentDefinitionDTO` (already stored on the entity); a DTO test and a
  DB-backed full-workConfig round-trip test.
- **Gateway** (`apps/web-ui/gateway/`): `memory.go` expands `AgentWorkConfig` to
  the full shape and adds `WorkConfig` + `SourceBlueprintID` to the local
  `AgentDefinition`; `agent.go` adds the `work` section, `uiAgentUpdateWork`,
  `applyAgentWorkSection`, and form-value helpers; `agent.templ` adds the nav
  item, panel case, and `agentWorkSettingsForm` + blueprint warning dialog.
- **Spec** (`apps/web-ui/tests/e2e/`): `agent-work-settings-ui.spec.ts`.
- **No DB migration**: `work_config` and `source_blueprint_id` already exist.
- **Non-goal (deferred)**: per-object-type work-config overrides and the
  per-type deliverable-type config — the form edits the agent-level config only.
