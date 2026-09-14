## Why

Alfred is a multi-agent platform, but every agent is currently standalone: the owner talks to each agent directly, and no agent can call another. That makes multi-agent workflows impossible — there is no way to build a coordinator agent that routes work to specialist agents, or an assistant that hands a task to another agent with different tools/prompts. Emergent Memory already ships agent-to-agent (A2A) tooling (`trigger_agent`, `spawn_agents`, `list_available_agents`) but it is deferred in the Alfred vision (decision D9) and never surfaced. This change brings A2A forward: add a per-agent configuration option that grants one agent the ability to choose and delegate to other agents in the project, wired through Emergent Memory's A2A and shared-memory facilities.

## What Changes

- Add a **delegation** configuration to an agent (the control-plane / agent-definition model): a per-agent option that grants delegation and scopes which other agents it may choose, with sensible defaults (off, or allowlist-empty = none).
- Surface the delegation config through the Go gateway's agent CRUD and (where it already exists) the client UI, so the owner can enable/edit it.
- Map the config onto Emergent Memory's A2A tools: grant `trigger_agent` / `spawn_agents` / `list_available_agents` to delegation-enabled agents, scoped so an agent can only see and trigger its configured targets.
- Document how memory is shared between a delegating (supervisor) agent and its sub-agents — project scope, shared knowledge graph, and what context flows on delegation — as an investigation deliverable captured in the design.

## Capabilities

### New Capabilities

- `agent-delegation`: the ability to configure an agent so it can choose and delegate to other agents in the same project, including which agents are selectable and what happens when delegation is granted or revoked.

### Modified Capabilities

<!-- None: no existing spec-level requirement changes. Agent CRUD and the memory
     proxy are implementation surfaces; delegation is new behavior, not a
     change to an existing requirement. -->

## Impact

- Control-plane / agent-definition model: new `delegation` field (and validation).
- Gateway (`gateway/`): agent CRUD request/response mapping for the delegation field; mapping to Emergent Memory `AgentDefinition` A2A config (`tools` / `banned_tools` / `tool_policies` / `dispatch_mode` / `acp_config` — exact knobs confirmed in design).
- Emergent Memory: relies on its existing A2A tools and dispatch queue; no memory-service code changes expected (verified during investigation).
- Clients: agent config UI (web, and iOS agent-management) gains a delegation editor only if the existing agent form already surfaces advanced fields; otherwise out of scope for the first pass.
