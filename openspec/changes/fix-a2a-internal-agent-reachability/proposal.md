## Why

An `external`-visibility agent is advertised in the A2A agent card and reachable
over ACP/A2A. When such an agent is configured with delegation (`spawn_agents` +
`list_available_agents` in its tools) and an **open** spawn policy (empty
`config.spawnPolicy`), its coordination tools enumerate and spawn every agent in
the project — including `internal`-visibility agents. This contradicts the
documented intent at `domain/agents/entity.go:279`:

> `internal` = "Hidden from lists; callable only by other agents, never via A2A"

Reproduced on dev: agent `acp-probe-external` (`visibility=external`, `config={}`)
carried `[set_session_title, list_available_agents, spawn_agents]` and a
`list_available_agents` call returned `graph-query-agent` (`visibility=internal`).
An external caller can therefore reach `internal` agents indirectly through the
external agent.

## What Changes

- Thread the running agent's visibility into the coordination tools
  (`CoordinationToolDeps.CallerVisibility`), derived from the agent definition in
  `buildCoordinationTools`.
- `list_available_agents`: hide `internal`-visible definitions from an
  `external`-visibility caller (extracted into `buildAgentCatalog`).
- `spawn_agents`: reject spawning an `internal`-visible target from an
  `external`-visibility caller, even when the target is inside the spawn-policy
  allowlist (extracted into `spawnTargetBlocked`).

### Fix choice and compatibility analysis

Two options were considered:

- **(a) default-deny coordination**: require an explicit `spawnPolicy.allow`
  before an agent receives `spawn_agents`/`list_available_agents`.
- **(b) filter internal**: exclude `internal` targets from the coordination
  surface for `external`-facing callers only.

**(b) is chosen.** Option (a) would break every working delegator that relies on
the open default to coordinate `external`/`project` agents — a behaviour change
with no migration path beyond editing every such agent in dev/prod and adding a
`spawnPolicy.allow`, plus a deprecation window. Option (b) is narrower: it leaves
`external→external`, `external→project`, `project→internal`, and
`internal→internal` coordination untouched (no silent breakage) and only removes
the specific path the documentation already forbids — an `external` (A2A-facing)
agent reaching `internal` agents.

The gate keys on **caller visibility** (`external` vs not), not trigger source.
This is deliberate: `external` is the sole level advertised in the A2A card, so an
`external` agent's coordination surface is itself A2A-reachable regardless of
whether the run was started by an A2A message or a manual trigger. It also avoids
depending on `TriggerSource`, which the ACP/agentcompat path does not set. No
migration or deprecation window is required; this is a documented behaviour
correction, not a capability removal.

## Capabilities

### Modified Capabilities

- `agent-delegation`: add a requirement that `internal` agents are unreachable
  from `external`-facing delegators (list + spawn), while non-external delegators
  retain internal coordination.

## Impact

- `apps/server/domain/agents/coordination_tools.go`: `CallerVisibility` field,
  `canReachInternal`/`callerVisibility` helpers, extracted `buildAgentCatalog`
  and `spawnTargetBlocked`; list and spawn now filter internal for external
  callers.
- `apps/server/domain/agents/executor.go`: `buildCoordinationTools` populates
  `CallerVisibility` from the agent definition.
- `apps/server/domain/agents/coordination_tools_test.go`: fail-first + regression
  coverage (external blocked; internal/project still coordinate; allowlist intact).
- No API, schema, or config-surface change.
