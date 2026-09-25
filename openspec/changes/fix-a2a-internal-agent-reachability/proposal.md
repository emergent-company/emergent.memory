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

A first fix keyed the gate on the *caller's own visibility* (`external` only).
That premise was wrong — `external` is not the sole A2A-facing level. Three
fail-open paths remained:

1. `a2aPickResolvableDefinition` serves a `project`-visibility fallback by slug,
   so an A2A caller invokes a `project` agent and reaches `internal` through its
   coordination tools.
2. A2A resume with a nil definition defaulted the surface to `project`.
3. agentcompat's `FindDefinitionByName` had no visibility filter, so
   `agent:<internal-name>` resolved and invoked internal agents directly.

## What Changes

- Add `ExecuteRequest.ExternalFacing bool`, set by the transport (not inferred
  from the agent's visibility) on every external-facing entry path: A2A
  `message:send` start+resume, A2A `message:stream` start+resume, agentcompat
  (new run + resume), and public share.
- Thread it into `CoordinationToolDeps.ExternalFacing`; `list_available_agents`
  hides `internal` definitions and `spawn_agents` rejects `internal` targets when
  the run is external-facing. Trusted surfaces leave the flag false and keep full
  internal coordination (internal→internal, project→internal).
- agentcompat: refuse to resolve or invoke an `internal` agent at the
  `HandleChatCompletion` boundary (same "not found" message as a missing agent).

### Fix choice and compatibility analysis

The invariant at `entity.go:279` is about the *surface* a call was invoked
through, not the agent's own visibility. The gate therefore keys on an explicit,
server-derived `ExternalFacing` signal set by each transport. This closes all
three holes without changing trusted-surface behaviour: session UI, scheduled /
worker runs, MCP tools, and agent→agent delegation all keep current behaviour
(no migration, no deprecation window). The only behavioural change is that
`internal` agents become unreachable from A2A, agentcompat, and share — the paths
the documentation already forbids. Default-deny for coordination (option (a)
from the issue) is still not applied, so existing delegators that coordinate
`external`/`project` agents are unaffected.

## Capabilities

### Modified Capabilities

- `agent-delegation`: add a requirement that `internal` agents are unreachable
  from external-facing surfaces (A2A, agentcompat, share), keyed on the transport
  surface, while trusted surfaces retain internal coordination.

## Impact

- `apps/server/domain/agents/executor.go`: `ExecuteRequest.ExternalFacing` field;
  `buildCoordinationTools` threads it into the coordination deps.
- `apps/server/domain/agents/coordination_tools.go`: `CoordinationToolDeps.ExternalFacing`,
  `canReachInternal(bool)`, extracted `buildAgentCatalog` and `spawnTargetBlocked`.
- `apps/server/domain/agents/a2a_message.go`, `a2a_stream.go`: set
  `ExternalFacing: true` on all four A2A start/resume paths.
- `apps/server/domain/agents/share_service.go`: set `ExternalFacing: true` on
  public share runs.
- `apps/server/domain/agentcompat/service.go`: set `ExternalFacing: true` on new
  run + resume; refuse internal agents at resolution.
- Tests: `coordination_tools_test.go` (per-hole fail-first + preserved paths),
  `agentcompat/internal_visibility_test.go` (resolution boundary).
- No API, schema, or config-surface change.
