## ADDED Requirements

### Requirement: Internal agents are unreachable from external-facing delegators

An `internal`-visibility agent SHALL be callable only by other agents and never reachable through the coordination surface of an `external`-visibility delegator. A delegating agent whose own visibility is `external` SHALL NOT see `internal` agents in `list_available_agents`, and spawning an `internal` target from an `external` caller SHALL be rejected, even when the target is inside the delegator's spawn-policy allowlist. `project`- and `internal`-visibility delegators SHALL retain the ability to list and spawn `internal` agents, so internal→internal and project→internal delegation keep working.

#### Scenario: External delegator cannot list internal agents

- **WHEN** an `external`-visibility delegating agent calls `list_available_agents` in a project that also contains an `internal`-visibility agent
- **THEN** the returned catalog excludes the `internal` agent, while `external` and `project` agents remain listed

#### Scenario: External delegator cannot spawn internal agents

- **WHEN** an `external`-visibility delegating agent attempts to spawn an `internal`-visibility target, whether or not the target is in its spawn-policy allowlist
- **THEN** the spawn is rejected with an error and no child run is started

#### Scenario: Non-external delegators can still coordinate internal agents

- **WHEN** a `project`- or `internal`-visibility delegating agent lists available agents or spawns an `internal`-visibility target
- **THEN** the internal agent is listed and spawnable, preserving internal→internal and project→internal delegation
