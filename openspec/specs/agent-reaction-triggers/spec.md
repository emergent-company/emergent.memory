# agent-reaction-triggers Specification

## Purpose
Define how `ReactionConfig` decides whether an event triggers a reaction agent: agent-origin controls (`ignoreAgentTriggered`, `ignoreSelfTriggered`) that keep loop risk opt-in, and `concurrencyStrategy` that optionally suppresses duplicate in-flight runs for the same agent and target object.

## Requirements

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

### Requirement: Deleting a runtime agent removes its trigger registrations

Deleting a runtime agent SHALL remove that agent's in-memory trigger registrations — its cron schedule in the scheduler and its reaction event listeners — in the same logical step as the row delete, so no deleted agent id keeps dispatching. This SHALL hold for every delete path: the API agent-delete endpoint, the agent MCP delete tool, and a blueprint Unapply that deletes blueprint-owned runtime agents. When one delete removes many agents, every deleted agent id SHALL be unregistered. A failed row delete SHALL NOT remove the registrations.

#### Scenario: API delete unregisters

- **WHEN** a runtime agent with a registered cron and/or reaction trigger is deleted through the API
- **THEN** its scheduler task and event listener entries are removed

#### Scenario: Blueprint Unapply unregisters every deleted runtime agent

- **WHEN** a blueprint Unapply deletes multiple blueprint-owned runtime agents
- **THEN** each deleted agent id's trigger registrations are removed

#### Scenario: Failed delete leaves registrations intact

- **WHEN** the row delete fails
- **THEN** the agent's trigger registrations remain
