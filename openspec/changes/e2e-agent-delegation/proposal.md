## Why

Agent delegation shipped its two halves separately and neither is proven to work
together. The gateway grows a delegation toggle into an agent's definition
(injecting `spawn_agents` + `list_available_agents` and `config.spawnPolicy.allow`),
and memory enforces that policy and runs the spawned sub-agent as a child run.
Coverage today stops at unit level: `apps/web-ui/gateway/delegation_test.go`
asserts the JSON round-trip and the tool injection, and
`apps/server/domain/agents/toolpool_test.go` asserts sub-agents have coordination
tools stripped. Nothing exercises the actual promise — that an agent configured
as able to delegate really does spawn another agent on a live turn, and that the
spawn lands as a real child run of the parent. A regression in the toggle
wiring, the spawn policy, the coordination tool plumbing, or the parent/child
run linkage would therefore ship silently.

## What Changes

Add one Playwright scenario spec,
`apps/web-ui/tests/e2e/scenarios/agent-delegation.spec.ts`, that:

- seeds an isolated project, a target agent B, a source agent A, and a reject
  agent C;
- enables delegation on A **through the agent Settings UI**, selecting B as the
  target, and asserts via `GET /api/agents/:id` that the toggle persisted
  `spawn_agents` and `list_available_agents` into `tools` and B's name into
  `config.spawnPolicy.allow`;
- proves the two validation paths: enabling delegation on C with **no target**
  is refused with a readable error and writes neither delegation tool nor a
  `spawnPolicy`, and unchecking the toggle on A strips both delegation tools and
  the `spawnPolicy` (re-enabling A restores them so the live turn still runs);
- sends a forced live chat turn to A instructing it to call `spawn_agents` for B,
  and asserts the tool invocation appears in the chat stream;
- verifies the spawn is a **real child run**: the child run id is read from the
  completed `spawn_agents` tool result (`result.results[0].run_id`), the run is
  fetched by that id, its parent run is fetched by `parentRunId`, it reaches a
  terminal `completed` status, and B's transcript carries the delegated task's
  result in an assistant message;
- cleans up its project and all three agents in `finally`, pass or fail.

The spec is live-LLM dependent, so it is env-gated exactly like the existing
`mcp-servers-tool-call` scenario: it skips (never fails) when the scenario LLM
credential is unset or the provider/model rejects the turn.

No product behavior change, no new Playwright project, and no config change —
test spec and its OpenSpec artifacts only.

## Capabilities

### New Capabilities

- `e2e-agent-delegation`: Playwright coverage of the delegation loop — configuring
  an agent as a delegator on its Settings page, a live turn that invokes
  `spawn_agents`, and verification that the spawned agent produced a real child
  run linked to its parent.

### Modified Capabilities

None. `agent-delegation` (server-side spawn tools, spawn policy, depth
restrictions) and the gateway delegation toggle are unchanged; this change only
adds end-to-end proof that they hold together.

## Impact

- `apps/web-ui/tests/e2e/scenarios/agent-delegation.spec.ts` — new spec (matched
  by the existing `scenarios` project).
- No changes to gateway Go/templ, server code, SDK, CLI, or Playwright config.
- Requires a live LLM provider configured in the target environment; without it
  the spec reports a skip rather than a failure.
- Creates no durable shared state: project and agents are deleted in `finally`.
