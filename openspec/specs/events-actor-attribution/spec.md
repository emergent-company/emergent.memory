# events-actor-attribution Specification

## Purpose
Define how the events bus attributes an `EntityEvent` to an actor, so downstream consumers (reaction triggers, real-time clients) can tell agent-originated changes from user-, system-, or unattributed changes.

## Requirements

### Requirement: Events stamp the actor from an explicit option or the context

`events.Service` SHALL populate `EntityEvent.Actor` at emit time. When `EmitOptions.Actor` is non-nil it SHALL be used verbatim (an explicit actor is authoritative). When it is nil, the service SHALL read the actor stamped on the emit `context.Context` via `auth.WithActor` and, if present, stamp it onto the event. When neither is present the event's actor SHALL remain nil.

The typed emit methods (`EmitCreated`, `EmitUpdated`, `EmitDeleted`, `EmitBatch`) SHALL accept the emitting `context.Context` so the stamped actor is reachable.

#### Scenario: Agent actor from context

- **WHEN** an emit runs on a context stamped with `auth.WithActor(ctx, "agent", <id>)` and no explicit `EmitOptions.Actor`
- **THEN** the emitted `EntityEvent.Actor` is non-nil with `ActorType == "agent"` and `ActorID == <id>`

#### Scenario: User and system actors from context

- **WHEN** an emit runs on a context stamped with a user or system actor
- **THEN** the emitted event carries that actor type and id

#### Scenario: No actor anywhere

- **WHEN** an emit runs with no context actor and no explicit `EmitOptions.Actor`
- **THEN** the emitted `EntityEvent.Actor` is nil

#### Scenario: Explicit option wins

- **WHEN** an emit runs on a context stamped with one actor and also passes a non-nil `EmitOptions.Actor`
- **THEN** the emitted event carries the explicit `EmitOptions.Actor`

### Requirement: Stamped actor activates the reaction agent-origin gate

When a graph-object event is emitted under an agent actor, the reaction trigger path SHALL treat the event as agent-originated: matched agents SHALL be skipped unless the agent's `ReactionConfig` explicitly opts in (`ignoreAgentTriggered: false`, and `ignoreSelfTriggered: false` for self-triggers). User- and system-originated events SHALL continue to dispatch matched agents unchanged.

#### Scenario: Agent-originated event is not dispatched by default

- **WHEN** an event is emitted under `auth.WithActor(ctx, "agent", <id>)` and a reaction agent with no explicit `ignoreAgentTriggered` matches it
- **THEN** no run is dispatched for that agent

#### Scenario: User-originated event still dispatches

- **WHEN** an event is emitted under `auth.WithActor(ctx, "user", <id>)` and a reaction agent matches it
- **THEN** the agent is dispatched
