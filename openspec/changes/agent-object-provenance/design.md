## Context

The knowledge graph already stores per-row actor provenance on objects: `kb.graph_objects.actor_type` (polymorphic: `user`, `agent`, `system`) and `actor_id` (user UUID for `user`, `kb.agent_definitions` UUID for `agent`, `NULL` for `system`). `domain/graph/events.go` defines the event/actor constants (`ActorUser`, `ActorSystem`); `pkg/auth/context.go` carries the auth values in `context.Context` via typed keys (`namespaceCtxKey`, etc.). The agent run boundary is where an agent's tool writes originate; the per-agent MCP endpoint is where a specific agent's tool calls arrive.

What is missing is attribution discipline: graph mutators currently hardcode `"user"` instead of reading the actor from context, relationships carry no provenance at all, branch-merge clones build fresh rows without actor columns, and nothing exposes a provenance filter. This change closes those gaps so "what did agent X touch" is answerable uniformly across objects and relationships.

## Goals / Non-Goals

**Goals:**
- Attribute every object version write and every relationship write to the actor that performed it.
- Expose a provenance filter on object listing with created/updated/any semantics.
- Expose actor fields on object and relationship API responses and SDK list options.
- Propagate agent identity through nested/delegated runs and per-agent MCP endpoints.
- Replace the "agent memories" surfaces (web UI link + iOS browser) with agent-scoped object browsing.

**Non-Goals:**
- No new object-table migration — `kb.graph_objects` already carries `actor_type`/`actor_id`.
- No re-decision of the actor-type model (it is fixed in this brief).
- No backfill of historical actor data for rows written before this change lands (only new writes and, where the source row already carries an actor, clone paths are fixed).
- No object-type hardcoding and no reintroduction of "memory" naming.

## Decisions

### 1. Actor model: reuse `kb.graph_objects.actor_type` / `actor_id`

`actor_type` is polymorphic: `user` (human / HTTP), `agent` (agent tool writes), `system` (extraction / background). `actor_id` holds the user UUID for `user`, the `kb.agent_definitions` UUID for `agent`, and `NULL` for `system`. No object migration is needed. Filtering is ALWAYS on the `(actor_type, actor_id)` pair — never `actor_id` alone — because `actor_id` alone is ambiguous across actor types.

The two agent id spaces are deliberately kept distinct: the run record, logging, and question creation key off the `kb.agents` run-entity id, while provenance attribution keys off the `kb.agent_definitions` id (the id the agent-scoped object view filters on). A run whose `kb.agents` entity has no resolvable definition is NOT stamped, so unlinked legacy schedules remain deliberately invisible in the agent-scoped view rather than stamping a `kb.agents` id that would never match. No alias/union filter is introduced.

### 2. Relationships gain provenance

`kb.graph_relationships` gets new `actor_type`/`actor_id` columns via a Goose migration, attributed on create/update/bulk relationship mutators exactly like objects. Relationship DTOs and the relationship API response gain the corresponding actor fields.

### 3. Agent identity propagation: stamp-on-run, not resolve-per-write

Agent identity is propagated by stamping the actor on the context at the agent run boundary (`auth.WithActor`). Nested/delegated runs attribute to the ACTUAL writing agent because each sub-agent re-stamps its own id at its own run boundary. The stamped id is the agent's `kb.agent_definitions` id. The per-agent MCP endpoint stamps its endpoint's agent definition id (resolved from the endpoint's `kb.agents` binding); if no definition resolves, it is not stamped.

- **Why stamp-on-run wins over resolve-per-write:** resolving "which agent is writing right now" at each individual write would require threading agent identity through every graph mutator call site and, crucially, gets the attribution wrong for nested/delegated runs — a delegated sub-agent's write would resolve to the *parent* agent unless the resolver itself knows the current leaf. Stamping at the boundary means the leaf agent's id is already in the context by the time any mutator reads it; mutators simply read `ActorFromContext` and never reason about run topology. The stamping is idempotent and cheap (one `context.WithValue` per run), and re-stamping is exactly what makes delegation correct.

### 4. Created-vs-updated query semantics

- "Created by actor X" = the earliest SURVIVING version row authored by X (the root row, `version=1` in the normal case).
- "Updated by actor X" = the HEAD row (`supersedes_id IS NULL`) authored by X.
- The filter exposes a provenance mode (`created` | `updated` | `any`). `any` matches an object where X is either its creator or its latest updater. All three modes always key on the `(actor_type, actor_id)` pair.

### 5. Merge-clone provenance fix

Branch merge clones currently build fresh rows WITHOUT actor columns (`graph.Service.applyMerge` around `service.go:3985-3998`, plus the fast-forward clone path). These MUST be fixed to carry the source row's actor so merged objects/relationships retain their original attribution instead of becoming actor-less.

### 6. Context plumbing

`auth.WithActor` / `ActorFromContext` live in `pkg/auth/context.go` beside `namespaceCtxKey`. Graph mutators read `ActorFromContext` and default to `user` (with the current user) only when no actor is stamped — preserving today's behavior for direct HTTP calls. MCP write paths (`executeBatchCreateEntities` → `CreateOrUpdate`, `executeUpdateEntity` → `Patch`, plus delete/restore) stop passing a hardcoded actor and read from context.

## Documented Limitations

These are accepted, explicit limitations of the provenance model, recorded as requirements in `object-provenance` so they remain visible:

1. **Sandbox-originated writes are not attributable to the agent.** Sandbox writes round-trip through the public HTTP API with an ephemeral token principal, so they are recorded against that ephemeral principal, not the driving agent.
2. **Delete/restore updates "updated by".** Deleting or restoring an object attributes the "updated by" to the deleting/restoring actor, not the original author.
3. **Hard-deleted version-1 rows shift "created by".** If the `version=1` root row is hard-deleted, "created by" falls back to the earliest surviving version, which may be a different actor than the original creator.
4. **No historical backfill.** Rows written before this change are not retroactively re-attributed.

## Risks / Trade-offs

- **Attribution ambiguity for system writes.** Background/extraction writes are `system` with `NULL` actor_id; the `(actor_type, actor_id)` pairing makes this explicit rather than guessing a user.
- **Ephemeral sandbox principal.** Accepted limitation (above); surfaced so no one assumes sandbox tool writes attribute to the agent.
- **Relationship migration is additive.** New nullable columns on `kb.graph_relationships`; backfill to `user`/NULL defaults is not attempted for pre-existing rows.
- **Agent-scoped views depend on the server filter.** The web UI and iOS views are thin clients over the provenance filter; if the filter is wrong, the views are wrong. Mitigated by the filter's single source of truth in `buildObjectBaseQueryWith`.

## Migration Plan

1. Add the Goose migration for `kb.graph_relationships.actor_type`/`actor_id`.
2. Land the server foundation (PR1): actor constants, `auth.WithActor`/`ActorFromContext`, context reads in mutators, MCP write-path attribution, relationship attribution, repo actor handling, merge-clone fix, `ListParams`/`buildObjectBaseQueryWith` filter, DTO/SDK fields.
3. Land CLI flags (PR2), web UI (PR3), iOS (PR4).
4. Archive the change after the final PR.

## Open Questions

None at the spec level. Remaining unknowns (exact filter SQL shape, whether `any` is expressed as a boolean OR two modes in the query params) are implementation details that do not change the specs or task breakdown.
