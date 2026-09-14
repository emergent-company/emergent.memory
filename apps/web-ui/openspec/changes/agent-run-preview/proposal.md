## Why

Agent delegation (capability `agent-delegation`) lets one agent spawn others, but the inter-agent conversation is invisible: spawned runs live in a separate `agent_runs` tree and never appear in the human chat UI, so the owner cannot see what the parent asked a sub-agent, what the sub-agent did, or what came back. To trust and debug multi-agent workflows, the owner needs to observe the delegation tree and read each spawned agent's transcript.

## What Changes

- The gateway proxies Emergent Memory's agent-run endpoints (list runs, get a run's messages / tool calls / steps / full bundle), reusing the existing server-side memory token.
- A new runs view surfaces delegation runs, grouped by their root run so the parent → child(s) tree is followable, and renders each run's transcript (agent messages + tool calls, including the `spawn_agents`/`trigger_agent` invocations).
- Because memory has no live message stream for runs, the view tails in-progress runs by polling the run's messages (memory persists them in real time).

## Capabilities

### New Capabilities

- `agent-run-preview`: the ability to list agent delegation runs and read each run's inter-agent transcript, with the delegation tree traceable from a parent run to its spawned children.

### Modified Capabilities

<!-- None: agent-delegation's configure/delegate behavior is unchanged; this is
     purely new observability on top of it. -->

## Impact

- Gateway (`gateway/`): new `MemoryClient` methods + proxy handlers for `GET /api/projects/:id/agent-runs` (list) and `/agent-runs/:id/{messages,tool-calls,steps,full}`; new run/message/tool-call DTOs mirrored from memory.
- UI: a runs view (list + transcript + tree), reachable from an agent or conversation.
- No Emergent Memory code changes — it already exposes these endpoints (`agents:read` scope).
