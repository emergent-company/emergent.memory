## Purpose

This capability is retired: project share instances now scope tools only, the agent allowlist and agent picker are removed, and agent sharing is handled solely by the agent-scoped endpoint and its keys.

## REMOVED Requirements

### Requirement: Agent allowlist limits agent-related tools

**Reason**: Project share instances no longer carry an agent allowlist, so agent-related tool filtering has no input to act on. Agent discovery and access are scoped by the agent-scoped endpoint and its keys instead.

**Migration**: No migration. Existing `allowed_agents` values are no longer consulted; instances expose the agents the token's scopes permit.

### Requirement: Agent execution respects the instance allowlist

**Reason**: Agent execution is no longer gated by a project instance allowlist. A credential bound to an agent endpoint can only reach its own agent, so the allowlist gate is redundant.

**Migration**: No migration. Users who need to expose one agent use the agent-scoped endpoint and a labeled key.

### Requirement: Agent allowlist entries are validated on write

**Reason**: Instances no longer accept an `agents` array, so there is nothing to validate or de-duplicate on write. Create and update requests supplying an agent allowlist are rejected.

**Migration**: No migration. Agent selection for sharing moves to the agent-scoped endpoint.

### Requirement: Shares degrade safely when an allowed agent is removed

**Reason**: Instances no longer reference agents, so there is no agent-reference list that could dangle after an agent is deleted. Project share instances continue to load without agent state.

**Migration**: No migration. Agent deletion is handled by the agent-scoped endpoint's cascade.

### Requirement: Agent references resolve by name or ACP slug and fail closed

**Reason**: This gating existed only to enforce the instance agent allowlist. With instance agent sharing removed, the agent-scoped endpoint's own authorization resolves and rejects references.

**Migration**: No migration. Agent reference resolution and fail-closed behavior are provided by the agent-scoped endpoint.

### Requirement: Agent-definition and run-inspection tools are not exposed to agent-allowlisted instances

**Reason**: Run-inspection, agent-mutation, agent-definition read, and discovery filtering were required only to keep an agent-allowlisted instance within its allowlist. With no allowlist, those filters are removed and tools are governed by tool scoping and token scopes as before.

**Migration**: No migration. Agent tooling availability follows the instance's tool allowlist and the token's scopes.
