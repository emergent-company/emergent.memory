## Purpose

Lets the owner observe agent-to-agent delegation: list the runs a delegating agent triggered and read each spawned agent's transcript, so the inter-agent conversation is visible instead of hidden inside the agent-run tree.

## ADDED Requirements

### Requirement: List agent delegation runs

The system SHALL allow the owner to list agent runs — the executions triggered by delegation — so they can see which agents ran and when.

#### Scenario: Runs listed

- **WHEN** the owner opens the runs view
- **THEN** the system lists the project's agent runs, most recent first, each identified by its agent and status

#### Scenario: No runs

- **WHEN** no agent runs exist
- **THEN** the system shows an empty state, not an error

### Requirement: Read a spawned agent's transcript

The system SHALL allow the owner to read the message transcript of any agent run, including the messages a delegating agent sent to its sub-agent and the sub-agent's replies.

#### Scenario: Transcript rendered

- **WHEN** the owner selects a run
- **THEN** the system shows the run's messages in order (sender role and content) plus the tool calls made during the run

#### Scenario: Run not found

- **WHEN** the owner requests a run that does not exist
- **THEN** the system returns a not-found error

### Requirement: Trace a delegation tree

The system SHALL let the owner follow a delegation from a parent run to the child runs it spawned, so the whole chain of delegation is traceable.

#### Scenario: Parent links to children

- **WHEN** a delegating agent spawned sub-agent runs
- **THEN** each child run is associated with its parent run, and the parent links to its children

#### Scenario: Group by root run

- **WHEN** one top-level run spawned a tree of child runs
- **THEN** all runs in that tree share a common root run identifier, so the whole delegation can be viewed together

### Requirement: Show in-progress runs

The system SHALL reflect a run that is still executing, including the transcript produced so far, rather than hiding it until completion.

#### Scenario: Partial transcript during execution

- **WHEN** an agent run is still in progress
- **THEN** the system shows the messages and tool calls produced so far, updating as the run continues

### Requirement: Surface delegation tool calls

The system SHALL include delegation tool calls (`spawn_agents` / `trigger_agent`) in a run's transcript so the owner can see when and to whom a delegating agent handed off work.

#### Scenario: Delegation call visible

- **WHEN** a delegating agent spawned a sub-agent
- **THEN** the transcript shows the delegation call with its target agent and the task passed to it
