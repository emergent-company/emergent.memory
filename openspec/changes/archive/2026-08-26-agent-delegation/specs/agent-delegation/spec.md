## Purpose

Lets an agent delegate work to other agents in the same project, so a coordinator or assistant can route tasks to specialist agents instead of being limited to its own tools.

## ADDED Requirements

### Requirement: Configure delegation for an agent

The system SHALL allow an agent to be configured with delegation capability, scoped to an explicit list of allowed target agents.

#### Scenario: Configure with an allowlist

- **WHEN** the owner configures agent A to delegate to agents B and C
- **THEN** A's delegation configuration records B and C as its allowed targets

#### Scenario: Delegation defaults to disabled

- **WHEN** an agent is created without any delegation configuration
- **THEN** the agent has no delegation capability and cannot delegate to any other agent

### Requirement: Enumerate allowed delegate targets

An agent with delegation configured SHALL be able to list the other agents it is allowed to delegate to, and only those agents.

#### Scenario: List allowed targets only

- **WHEN** agent A (allowed to delegate to B and C) requests the list of agents it may delegate to
- **THEN** the list contains B and C, and does not contain other agents in the project

#### Scenario: No targets for a disabled agent

- **WHEN** an agent without delegation requests the list of agents it may delegate to
- **THEN** the list is empty

### Requirement: Enforce the delegation allowlist

The system SHALL permit an agent to delegate only to agents in its configured allowlist, and MUST reject delegation to any other agent.

#### Scenario: Delegation outside the allowlist is rejected

- **WHEN** agent A (allowed to delegate to B only) attempts to delegate to agent D
- **THEN** the delegation is rejected and no target run is started

### Requirement: Delegate to an allowed agent and receive a result

The system SHALL run a delegated task on the allowed target agent and return its result or completion status to the delegating agent.

#### Scenario: Successful delegation

- **WHEN** agent A delegates a task to allowed agent B
- **THEN** B runs the task and A receives B's result or completion status

#### Scenario: Target agent does not exist

- **WHEN** agent A delegates to a target name that has no agent definition in the project
- **THEN** the delegation fails with a clear error and no run is started

### Requirement: Shared project memory between delegating and delegated agents

A delegated (sub) agent run SHALL be scoped to the same project memory as the delegating agent, so the two agents read and write the same knowledge graph and can share context across a delegation.

#### Scenario: Sub-agent recalls supervisor context

- **WHEN** the delegating agent has persisted a fact to project memory, then delegates to an allowed sub-agent
- **THEN** the sub-agent can recall that fact from the same project memory

### Requirement: Revoking delegation takes effect immediately

When delegation is removed from an agent, the agent SHALL lose the ability to delegate on its next action, without requiring a rebuild or restart.

#### Scenario: Delegation removed

- **WHEN** the owner removes delegation configuration from agent A
- **THEN** A can no longer list or trigger any delegate targets on its next action
