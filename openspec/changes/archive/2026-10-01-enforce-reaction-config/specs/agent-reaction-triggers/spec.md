## ADDED Requirements

### Requirement: Reaction config agent-origin controls are optional and default to loop-safe

`ReactionConfig.ignoreAgentTriggered` and `ReactionConfig.ignoreSelfTriggered` SHALL be optional booleans (JSON `omitempty`). When either field is absent, the system SHALL treat it as `true`, preserving the historical behaviour in which an agent-originated event never triggers a reaction agent.

#### Scenario: Unset controls ignore agent-originated events

- **WHEN** a reaction agent has no explicit `ignoreAgentTriggered` value and an event whose actor is an agent matches the agent's object types and events
- **THEN** no run is started for that agent

#### Scenario: Explicit false opts in

- **WHEN** a reaction agent sets `ignoreAgentTriggered` to `false` and an event originated by a different agent matches
- **THEN** the agent is triggered

### Requirement: Self-trigger is ignored by default

When an event is agent-originated and the originating agent is the same agent, the system SHALL ignore the event unless the agent explicitly sets both `ignoreAgentTriggered` and `ignoreSelfTriggered` to `false`. The same-agent identity SHALL be the canonical agent actor id (the agent definition id), matching how agent-originated graph mutations are attributed.

#### Scenario: Self-trigger skipped by default

- **WHEN** an agent with `ignoreAgentTriggered:false` and no explicit `ignoreSelfTriggered` re-triggers itself
- **THEN** the event is ignored

#### Scenario: Self-trigger allowed when both explicit

- **WHEN** an agent sets `ignoreAgentTriggered:false` and `ignoreSelfTriggered:false` and re-triggers itself
- **THEN** the agent is triggered

### Requirement: Concurrency strategy controls duplicate runs per object

`ConcurrencyStrategy` SHALL accept `""`, `"parallel"`, or `"skip"`. Empty and `"parallel"` SHALL both mean no concurrency control. `"skip"` SHALL drop a matching trigger when a non-terminal run (status in queued/running/paused/cancelling) already exists for the same agent and the same target object, where the object key is the agent id plus the target object id and type.

#### Scenario: Skip drops a duplicate in-flight run

- **WHEN** an agent with `concurrencyStrategy:"skip"` matches an event for an object that already has a non-terminal run for the same agent
- **THEN** the new trigger is skipped and no second run is started

#### Scenario: Empty and parallel do not skip

- **WHEN** an agent has `concurrencyStrategy` empty or `"parallel"` and a non-terminal run already exists for the same agent and object
- **THEN** the new trigger proceeds

### Requirement: Non-agent-originated events are unaffected

The per-agent agent-origin gate SHALL apply only to agent-originated events. Events with no actor, a user actor, or a system actor SHALL trigger matched agents exactly as before.

#### Scenario: User event still triggers

- **WHEN** a user-originated event matches an agent whose `ignoreAgentTriggered` is unset
- **THEN** the agent is triggered
