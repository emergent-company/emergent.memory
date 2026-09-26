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

- Add `ExecuteRequest.TrustedInternal bool`, set by the transport (not inferred
  from the agent's visibility). The polarity is **fail-closed**: the zero value
  is `false` = untrusted/external-facing, so a transport that forgets to declare
  itself cannot reach `internal` agents. Trusted surfaces (session UI,
  scheduled/worker runs, MCP tools, agent→agent delegation) set it `true`.
- Persist the marker on the run row (`kb.agent_runs.trusted_internal`, migration
  00180, default `false`) and inherit it unchanged through delegation and
  resume: spawned children carry the parent's marker, and a resume inherits the
  prior run's persisted marker rather than re-deriving it from the resume
  transport. The invariant therefore holds for the whole call chain, not just
  the first hop.
- Thread it into `CoordinationToolDeps.TrustedInternal`; `list_available_agents`
  hides `internal` definitions and `spawn_agents` rejects `internal` targets when
  the run is untrusted. Trusted surfaces keep full internal coordination
  (internal→internal, project→internal).
- agentcompat: refuse to resolve or invoke an `internal` agent at the
  `HandleChatCompletion` boundary (same "not found" message as a missing agent).

### Fix choice and compatibility analysis

The invariant at `entity.go:279` is about the *surface* a call was invoked
through, not the agent's own visibility. The gate therefore keys on an explicit,
server-derived `TrustedInternal` signal set by each transport, whose zero value
fails closed (untrusted) so an omitted declaration is never silently trusted.
This closes the reachability holes — including the transitive (child-spawn /
resume) paths — without changing trusted-surface behaviour: session UI,
scheduled / worker runs, MCP tools, and agent→agent delegation all keep internal
coordination because they set `TrustedInternal: true`. The only behavioural
change is that `internal` agents become unreachable from A2A, agentcompat, and
share — the paths the documentation already forbids.

## Capabilities

### Modified Capabilities

- `agent-delegation`: add a requirement that `internal` agents are unreachable
  from external-facing surfaces (A2A, agentcompat, share), keyed on the transport
  surface, while trusted surfaces retain internal coordination.

## Impact

- `apps/server/domain/agents/executor.go`: `ExecuteRequest.TrustedInternal` field;
  `Execute`/`ExecuteWithRun`/`Resume` persist and inherit the marker;
  `runPipeline` overrides the request with the run row's persisted value.
- `apps/server/domain/agents/coordination_tools.go`: `CoordinationToolDeps.TrustedInternal`,
  `canReachInternal(bool)`, extracted `buildAgentCatalog` and `spawnTargetBlocked`,
  and child-spawn propagation.
- `apps/server/domain/agents/entity.go`, `repository.go`: `AgentRun.TrustedInternal`,
  `CreateRunOptions.TrustedInternal`, `UpdateRunTrustedInternal`.
- `apps/server/domain/agents/a2a_message.go`, `a2a_stream.go`,
  `share_service.go`, `domain/agentcompat/service.go`: leave the marker untrusted
  (the fail-closed default) on external-facing paths.
- `apps/server/domain/agents/handler.go`, `triggers.go`, `worker_pool.go`,
  `mcp_tools.go`, `agent_run_once.go`, `domain/chat/handler.go`: set
  `TrustedInternal: true` on trusted surfaces.
- `apps/server/migrations/00180_add_agent_runs_trusted_internal.sql`: persist the
  marker (default `false`).
- Tests: `coordination_tools_test.go` (fail-closed polarity + preserved paths),
  `trust_propagation_test.go` (transitive spawn + resume inheritance, DB-backed),
  `agentcompat/internal_visibility_test.go` (resolution boundary).
