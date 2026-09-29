# object-provenance Specification

## Purpose
TBD - created by archiving change agent-object-provenance. Update Purpose after archive.

## Requirements

### Requirement: Objects record actor on every version write

Every graph object version write SHALL record an actor as a `(actor_type, actor_id)` pair. `actor_type` SHALL be polymorphic: `user` (human / HTTP), `agent` (agent tool writes), or `system` (extraction / background). `actor_id` SHALL hold the user UUID for `user`, the `kb.agent_definitions` UUID for `agent`, and `NULL` for `system`. The `kb.agent_definitions` id is the single canonical `actor_id` for `actor_type='agent'`; the separate `kb.agents` run-entity id is never used for provenance.

#### Scenario: Human write records the user actor

- **WHEN** a signed-in user creates or updates an object through the HTTP API
- **THEN** the object version is stored with `actor_type = user` and `actor_id` set to that user's UUID

#### Scenario: Agent tool write records the agent actor

- **WHEN** an agent's tool writes an object
- **THEN** the object version is stored with `actor_type = agent` and `actor_id` set to the writing agent's `kb.agent_definitions` UUID

#### Scenario: Background write records the system actor

- **WHEN** extraction or another background process writes an object
- **THEN** the object version is stored with `actor_type = system` and `actor_id = NULL`

### Requirement: Relationships record actor

Every relationship create, update, and bulk mutation SHALL record the actor as a `(actor_type, actor_id)` pair on the relationship row, mirroring object provenance.

#### Scenario: Relationship create records actor

- **WHEN** a relationship is created
- **THEN** the relationship row stores `actor_type`/`actor_id` for the creating actor

#### Scenario: Relationship update records actor

- **WHEN** a relationship is updated
- **THEN** the relationship row's actor reflects the updating actor

#### Scenario: Bulk relationship mutation records actor

- **WHEN** relationships are created or updated in bulk
- **THEN** every affected relationship row stores the acting actor

### Requirement: Actor exposed in object and relationship responses

Object and relationship API responses SHALL expose the recorded actor fields so clients can render and filter on provenance.

#### Scenario: Object response carries actor

- **WHEN** an object is returned by the API
- **THEN** the response includes its `actor_type` and `actor_id`

#### Scenario: Relationship response carries actor

- **WHEN** a relationship is returned by the API
- **THEN** the response includes its `actor_type` and `actor_id`

### Requirement: Object provenance filter with created/updated/any modes

Object listing SHALL support filtering by provenance. The filter SHALL accept a `(actor_type, actor_id)` pair and a mode of `created`, `updated`, or `any`. "Created by actor X" SHALL match objects whose earliest SURVIVING version row was authored by X (the root row, `version=1` in the normal case), scoped to the SAME project and branch as the outer query. On a named branch, `version=1` is the branch's fork-time copy, so "created by" there reflects the version author at fork time and may differ from the object's original creator on `main`. "Updated by actor X" SHALL match objects whose HEAD row (`supersedes_id IS NULL`) was authored by X. `any` SHALL match objects where X is either the creator or the latest updater. The API SHALL validate `actor_type` against the known set (`user`, `agent`, `system`) and SHALL reject `provenance` without `actor_type`.

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

### Requirement: Documented provenance limitations

The provenance model SHALL document, as accepted limitations: sandbox-originated writes are not attributable to the driving agent; delete/restore attributes "updated by" to the deleting/restoring actor; and hard-deleting the `version=1` row shifts "created by" to the earliest surviving version.

#### Scenario: Sandbox write is not agent-attributable

- **WHEN** a sandbox-originated write round-trips through the public HTTP API with an ephemeral token principal
- **THEN** the write is recorded against that ephemeral principal, not the driving agent

#### Scenario: Delete/restore updates the updated-by actor

- **WHEN** an object is deleted or restored
- **THEN** the "updated by" attribution reflects the deleting or restoring actor

#### Scenario: Hard-deleted root shifts created-by

- **WHEN** the `version=1` root row is hard-deleted
- **THEN** "created by" falls back to the earliest surviving version's actor, which may differ from the original creator
