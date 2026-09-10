## Purpose

Restricts an MCP share instance to a chosen set of agents, so an outside agent connected through a share can only discover and invoke the agents the project selected.

## ADDED Requirements

### Requirement: Agent allowlist limits agent-related tools

When an instance has an agent allowlist, agent-related tools exposed over MCP (`agent-list`, `agent-get`, `agent-list-available`, and any equivalent agent-discovery tool) SHALL return only agents in that allowlist. A null agent allowlist SHALL expose the agents otherwise permitted by the token's scopes.

#### Scenario: Only allowed agents are listed

- **WHEN** an instance allows agents A and B and the project also contains agent C
- **THEN** the agent-listing tool returns A and B and omits C

#### Scenario: Reading a non-allowed agent is rejected

- **WHEN** a client calls `agent-get` for an agent outside the instance allowlist
- **THEN** the call returns not-found and reveals no details of that agent

#### Scenario: Null allowlist exposes scope-permitted agents

- **WHEN** an instance has no agent allowlist
- **THEN** agent-related tools return the agents the token's scopes permit

#### Scenario: Agent-scoped share cannot exceed scope

- **WHEN** an instance allows an agent but the token lacks the scope required to read agents
- **THEN** agent-related tools are unavailable to that instance

### Requirement: Agent execution respects the instance allowlist

A tool that invokes an agent (for example agent-run or agent-exec) SHALL reject execution for any agent not in the instance's agent allowlist, before the agent run is started.

#### Scenario: Allowed agent can be invoked

- **WHEN** a client invokes an agent in the instance allowlist
- **THEN** the run proceeds as it would for a normally authorized caller

#### Scenario: Non-allowed agent cannot be invoked

- **WHEN** a client invokes an agent outside the instance allowlist
- **THEN** the call is rejected before any run starts

### Requirement: Agent allowlist entries are validated on write

An agent allowlist supplied when creating or updating an instance SHALL be validated against the project's agents. Agent IDs that do not belong to the project MUST be rejected, and the allowlist MUST be de-duplicated.

#### Scenario: Unknown agent ID rejected

- **WHEN** a create or update request includes an agent ID that is not in the project
- **THEN** the request is rejected with an unprocessable-entity error and the instance is unchanged

#### Scenario: Duplicate agent IDs collapsed

- **WHEN** a request supplies the same agent ID twice
- **THEN** the stored allowlist contains that agent exactly once

### Requirement: Shares degrade safely when an allowed agent is removed

If an agent in an instance's allowlist is later deleted from the project, the instance SHALL continue to load and that agent ID SHALL simply resolve to no agent; the share MUST NOT fail.

#### Scenario: Deleted agent no longer listed

- **WHEN** an allowed agent is deleted from the project and the instance is used
- **THEN** the agent-listing tool returns the remaining allowed agents without error

### Requirement: Agent references resolve by name or ACP slug and fail closed

Agent-execution gating SHALL resolve the referenced agent by either its runtime name or its ACP slug. If the reference cannot be resolved (unknown agent, non-project agent, or a directory error), the call SHALL be rejected before any run starts.

#### Scenario: ACP slug resolves to an allowlisted agent

- **WHEN** an instance allows an agent and `acp-trigger-run` references that agent by its ACP slug
- **THEN** the referenced agent is resolved and the run proceeds

#### Scenario: Unresolvable agent reference rejected

- **WHEN** an agent-allowlisted instance references an agent that cannot be resolved to a project agent
- **THEN** the call is rejected before any run starts

### Requirement: Agent-definition and run-inspection tools are not exposed to agent-allowlisted instances

When an instance has an agent allowlist, agent run-inspection and agent-mutation tools that cannot be reliably mapped to the allowlist SHALL be hidden from `tools/list` and rejected on call. Agent-definition read tools SHALL be filtered to the allowed agents (and rejected/not-found otherwise). Agent-discovery tools SHALL filter their results and fail closed to an empty result on a parse or shape mismatch.

#### Scenario: Run-inspection tool hidden

- **WHEN** an instance has an agent allowlist
- **THEN** agent run-inspection and agent-mutation tools are not listed and their calls are rejected

#### Scenario: Agent-definition read filtered

- **WHEN** an agent-allowlisted instance lists agent definitions or reads one by ID
- **THEN** only the definitions of allowed agents are returned and any other definition reads as not-found

#### Scenario: Discovery filtering fails closed

- **WHEN** an agent-discovery tool returns a malformed payload under an agent allowlist
- **THEN** the instance receives an empty result, never the unfiltered payload

#### Scenario: ACP agent listing filtered

- **WHEN** an instance allows a subset of agents and `acp-list-agents` is called
- **THEN** only manifests for the allowed agents (matched by name or ACP slug) are returned
