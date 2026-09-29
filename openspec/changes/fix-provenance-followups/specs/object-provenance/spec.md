## MODIFIED Requirements

### Requirement: Object provenance filter with created/updated/any modes

Object listing SHALL support filtering by provenance. The filter SHALL accept a `(actor_type, actor_id)` pair and a mode of `created`, `updated`, or `any`. "Created by actor X" SHALL match objects whose earliest SURVIVING version row was authored by X (the root row, `version=1` in the normal case), scoped to the SAME project and branch as the outer query. "Updated by actor X" SHALL match objects whose HEAD row (`supersedes_id IS NULL`) was authored by X. `any` SHALL match objects where X is either the creator or the latest updater. The API SHALL validate `actor_type` against the known set (`user`, `agent`, `system`) and SHALL reject `provenance` without `actor_type`.

#### Scenario: Filter by creator

- **WHEN** an object list query filters by an actor in `created` mode
- **THEN** only objects whose earliest surviving version was authored by that actor are returned

#### Scenario: Filter by updater

- **WHEN** an object list query filters by an actor in `updated` mode
- **THEN** only objects whose HEAD row was authored by that actor are returned

#### Scenario: Filter by any

- **WHEN** an object list query filters by an actor in `any` mode
- **THEN** objects the actor created OR most recently updated are returned

#### Scenario: Filtering always uses the actor pair

- **WHEN** any provenance filter is applied
- **THEN** matching keys on the `(actor_type, actor_id)` pair together, never on `actor_id` alone

#### Scenario: Created filter is project- and branch-scoped

- **WHEN** an object list query filters by a `system` actor (NULL `actor_id`) in `created` mode
- **THEN** only that project's (and branch's) system-authored version=1 rows are considered, never system rows from other projects or branches

#### Scenario: Invalid actor_type rejected

- **WHEN** an object list query supplies an `actor_type` outside `user`/`agent`/`system`
- **THEN** the API rejects it with a 400

#### Scenario: Provenance without actor_type rejected

- **WHEN** an object list query supplies `provenance` without `actor_type`
- **THEN** the API rejects it with a 400

### Requirement: Branch-merge provenance preservation

Branch-merge clones SHALL preserve the source row's actor. Merged, fast-forward-cloned, conflict-resolved, and similarity-absorbed objects and relationships SHALL NOT be built as fresh actor-less rows.

#### Scenario: Merged object retains actor

- **WHEN** a branch merge clones an object into the target branch
- **THEN** the cloned row carries the source row's `actor_type`/`actor_id`

#### Scenario: Fast-forward clone retains actor

- **WHEN** a fast-forward merge clones a relationship or object
- **THEN** the cloned row carries the source row's actor

#### Scenario: Conflict-resolved object retains actor

- **WHEN** a branch merge resolves a conflict by creating a new object version on the target
- **THEN** the new version carries the source row's `actor_type`/`actor_id`, never `user`+NULL

#### Scenario: Similarity-absorbed object retains actor

- **WHEN** a branch merge absorbs a similar object into an existing target entity
- **THEN** the new version carries the source row's `actor_type`/`actor_id`

#### Scenario: Similarity-merged relationship retains actor

- **WHEN** a branch merge similarity-merges a relationship into an existing target relationship
- **THEN** the new version carries the source row's `actor_type`/`actor_id`

### Requirement: Agent identity propagation through nested and delegated runs

Agent identity SHALL be propagated by stamping the actor on the context at the agent run boundary, and nested or delegated runs SHALL attribute writes to the ACTUAL writing agent. The stamped id SHALL be the `kb.agent_definitions` id of the writing agent. A run whose definition cannot be resolved SHALL CLEAR any inherited parent agent actor so its writes fall back to user/system attribution rather than inheriting the parent's agent id.

#### Scenario: Delegated sub-agent re-stamps its own identity

- **WHEN** a parent agent delegates a sub-run to a sub-agent that writes an object
- **THEN** the write is attributed to the sub-agent's `kb.agent_definitions` UUID, not the parent agent's

#### Scenario: Per-agent MCP endpoint attributes to its agent's definition

- **WHEN** a tool call arrives through a per-agent MCP endpoint
- **THEN** the resulting write is attributed to the endpoint's agent definition id, resolved from its `kb.agents` endpoint binding

#### Scenario: Run whose definition cannot be resolved is not stamped

- **WHEN** a run's `kb.agents` entity has no resolvable `kb.agent_definitions` (e.g. an unlinked legacy schedule)
- **THEN** the run clears any inherited parent agent actor and its writes fall back to the user/system attribution path, rather than inheriting the parent agent's id or stamping a `kb.agents` id the agent-scoped view cannot match
