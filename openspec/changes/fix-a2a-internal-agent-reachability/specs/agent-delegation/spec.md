## ADDED Requirements

### Requirement: Internal agents are unreachable from external-facing surfaces

An `internal`-visibility agent SHALL be callable only by other agents and never reachable through an external-facing surface. The external-facing surfaces are the A2A protocol (`message:send` and `message:stream`, both start and resume), the OpenAI-compatible agentcompat API (`/v1/chat/completions`), and public agent-share links. The external-facing determination SHALL come from the transport that started the run, not from the invoked agent's own visibility, so a `project`-visibility agent reached via A2A is external-facing for the duration of that run.

From an external-facing surface: `list_available_agents` SHALL exclude `internal`-visibility agents, `spawn_agents` SHALL reject spawning an `internal` target even when it is inside the spawn-policy allowlist, and the agentcompat resolution SHALL refuse to resolve or invoke an `internal` agent without revealing that the name exists. Trusted surfaces — session UI, scheduled/worker runs, MCP tools, and agent→agent delegation — SHALL retain full internal coordination, so internal→internal and project→internal delegation keep working.

#### Scenario: A2A-reached project agent cannot list internal agents

- **WHEN** an A2A caller invokes a `project`-visibility agent by its slug, and that agent calls `list_available_agents` in a project that also contains an `internal`-visibility agent
- **THEN** the returned catalog excludes the `internal` agent, while `external` and `project` agents remain listed

#### Scenario: External-facing caller cannot spawn internal agents

- **WHEN** a run started through an external-facing surface attempts to spawn an `internal`-visibility target, whether or not the target is in its spawn-policy allowlist
- **THEN** the spawn is rejected with an error and no child run is started

#### Scenario: A2A resume without a definition fails closed

- **WHEN** an A2A resume proceeds with no resolvable agent definition, the run SHALL still be treated as external-facing
- **THEN** its coordination tools hide `internal` agents and reject spawning them, rather than defaulting to a permissive surface

#### Scenario: agentcompat cannot resolve an internal agent

- **WHEN** an OpenAI-compatible caller requests `agent:<internal-name>` on `/v1/chat/completions`
- **THEN** the request is refused at resolution and the internal agent is never invoked

#### Scenario: Trusted surfaces can still coordinate internal agents

- **WHEN** a `project`- or `internal`-visibility delegating agent, invoked through a trusted surface (session UI, scheduler, MCP, or agent→agent delegation), lists available agents or spawns an `internal`-visibility target
- **THEN** the internal agent is listed and spawnable, preserving internal→internal and project→internal delegation
