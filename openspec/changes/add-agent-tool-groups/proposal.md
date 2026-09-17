## Why

Agent tool approvals are configured one tool at a time. `kb.agent_definitions.tool_policies`
is a flat `{tool_name: {confirm|disabled}}` map plus a single `default_tool_policy`
fallback. The Settings → Tools panel (`gateway/agent.go` `applyAgentToolsSection`,
`gateway/agent.templ` `agentToolPicker`) renders one `<details>` per MCP server / relay
node and a per-tool `<select>`. With ~90 memory tools plus workspace, native, and
external MCP tools, configuring a realistic agent means dozens of individual selects,
and the panel cannot express "all destructive graph writes require approval" — the
statement a user actually wants to make.

Separately, the server already derives a capability category per tool
(`mcp/share_instance.go:toolCategoryDerived`, driven by `RequiredScope` and
`mcp/service.go:toolRequiredScope`), but that taxonomy only feeds the MCP
share-instance tool catalog. It is not exposed to the agent settings UI and has no
bearing on approval policy. A tool→group notion therefore already exists server-side;
it is simply unused for policy.

## What Changes

- Introduce first-class **tool groups** keyed on capability domain (`Graph · Write`,
  `Schema · Migrate`, `Workspace · Execute`, …), derived from each tool's required
  scope where one exists and from a static map for unscoped tools (workspace, native,
  web, hidden builtins).
- Add **group-level approval policy**: a group policy applies to every member tool that
  has no explicit per-tool override. Stored under reserved `@group:<id>` keys in the
  existing `tool_policies` jsonb — **no migration**.
- Resolution order becomes explicit tool → group → `default_tool_policy`, resolved in
  the existing single chokepoint `AgentDefinition.effectiveToolPolicy`.
- Add **group-level enable/disable** in the Settings → Tools panel: toggling a group
  fans out to its member tools (`Tools` membership + `BannedTools`), matching today's
  per-tool semantics. Disabling a group also bans its members so a later tool-list
  change cannot silently re-enable them.
- Rework the Settings → Tools panel from a source-only list into capability groups with
  the existing MCP-server / relay-node sections nested as sub-groups. Each group header
  gets an enable switch and a tri-state policy control; per-tool overrides remain and
  show what they inherit.
- Expose the computed group catalog on the agent definition read DTO so the gateway
  renders one server-owned taxonomy instead of re-deriving it.

## Capabilities

### New Capabilities

<!-- None: this extends the existing tool-approval-policy and agent-dashboard-ui capabilities. -->

### Modified Capabilities

- `tool-approval-policy`: group policy layer, group resolution precedence, group
  disable semantics.
- `agent-dashboard-ui`: capability-grouped Tools panel with group enable switch and
  group policy control.

## Impact

- **Server** (`apps/server/domain/agents/`): new `toolgroups` package (registry +
  `GroupForTool`); `entity.go` `effectiveToolPolicy` gains the group fallback; `dto.go`
  exposes the computed `toolGroups` list. `apps/server/domain/mcp/`: export a scope
  lookup (`toolRequiredScope` is currently package-private).
- **Gateway** (`apps/web-ui/gateway/`): `agent.go` `applyAgentToolsSection` writes
  `@group:` policies and handles group enable/disable fan-out; `agent.templ` group
  header UI; `memory.go` local `ToolGroup` mirror struct.
- **Spec** (`apps/web-ui/docs/spec/14-assistant-agent.md`): document the group layer.
- **No DB migration**: group policies live in the existing `tool_policies` jsonb.
- **Non-goal (deferred)**: group *policy* on external MCP-server and relay-node
  sections. Those sections get group enable/disable (gateway-local membership
  fan-out) but not a group policy control, because relay instance IDs are known only
  to the gateway and would need a new server-side relay registry lookup to resolve.
