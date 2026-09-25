## ADDED Requirements

### Requirement: Internal agents are unreachable from external-facing surfaces

An `internal`-visibility agent SHALL be callable only by other agents and never reachable through an external-facing surface. The external-facing surfaces are the A2A protocol (`message:send` and `message:stream`, both start and resume), the OpenAI-compatible agentcompat API (`/v1/chat/completions`), public agent-share links, and the public agent webhook receiver (`POST /api/webhooks/agents/:hookId`, authenticated only by a per-hook bearer token). The external-facing determination SHALL come from the transport that started the run, not from the invoked agent's own visibility, so a `project`-visibility agent reached via A2A or webhook is external-facing for the duration of that run.

From an external-facing surface: `list_available_agents` SHALL exclude `internal`-visibility agents, `spawn_agents` SHALL reject spawning an `internal` target even when it is inside the spawn-policy allowlist, and the agentcompat resolution SHALL refuse to resolve or invoke an `internal` agent without revealing that the name exists. Trusted surfaces — session UI, scheduled/worker runs, and agent→agent delegation — SHALL retain full internal coordination, so internal→internal and project→internal delegation keep working. The MCP delegation tools (`trigger_agent` and `call_agent`) are NOT inherently trusted surfaces: they SHALL inherit the invoking run's trust (an authenticated MCP client with no parent run is external-facing and therefore untrusted), and they SHALL respect target visibility, refusing to trigger an `internal`-visibility agent when the caller is untrusted.

The reachability gate is **transitive**: the trust marker (whether the run started through a trusted surface) is fixed at run creation and SHALL be persisted on the run row and inherited unchanged through both delegation (spawned child runs) and resume, so the invariant holds for the whole call chain rather than the first hop only. The marker is fail-closed: the zero value is *untrusted*, so a transport that omits the declaration SHALL resolve to the restrictive case and cannot reach `internal` agents.

#### Scenario: External-facing reach is blocked transitively

- **WHEN** an external-facing run delegates to a `project`-visibility agent, and that child attempts to list or spawn an `internal`-visibility agent
- **THEN** the child inherits the external-facing (untrusted) marker and is rejected or excluded exactly as its parent is, at any delegation depth

#### Scenario: A suspended external-facing run stays external-facing when re-woken

- **WHEN** an external-facing run is suspended and later resumed through a trusted path (parent wake, MCP question response, or session UI)
- **THEN** the resumed run inherits the persisted external-facing (untrusted) marker and cannot reach `internal` agents, rather than being upgraded to trusted

#### Scenario: Omitted trust declaration resolves to the restrictive case

- **WHEN** a transport starts a run without declaring whether the surface is trusted
- **THEN** the run is treated as external-facing (untrusted) and cannot reach `internal` agents

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

#### Scenario: A webhook-triggered run is external-facing

- **WHEN** a holder of a webhook secret POSTs a prompt to `POST /api/webhooks/agents/:hookId` and the bound agent calls `list_available_agents` or `spawn_agents` in a project that also contains an `internal`-visibility agent
- **THEN** the run is external-facing (untrusted) regardless of the per-hook token: the catalog excludes the `internal` agent and spawning it is rejected, because the webhook receiver is a public surface with no `RequireAuth`, not the session UI

#### Scenario: MCP delegation tools inherit the caller's trust

- **WHEN** an external-facing (untrusted) run invokes `trigger_agent` or `call_agent` to run another agent
- **THEN** the triggered child run inherits the caller's untrusted marker (rather than being trusted), so the child cannot itself reach `internal` agents through its own coordination tools

#### Scenario: An untrusted caller cannot trigger_agent an internal agent

- **WHEN** an external-facing (untrusted) run invokes `trigger_agent` on an `internal`-visibility target, whether by `agent_name` or `agent_id`
- **THEN** the trigger is rejected and no child run is started, the same as a blocked `spawn_agents` target

#### Scenario: Trusted surfaces can still coordinate internal agents

- **WHEN** a `project`- or `internal`-visibility delegating agent, invoked through a trusted surface (session UI, scheduler, or agent→agent delegation), lists available agents or spawns an `internal`-visibility target
- **THEN** the internal agent is listed and spawnable, preserving internal→internal and project→internal delegation
