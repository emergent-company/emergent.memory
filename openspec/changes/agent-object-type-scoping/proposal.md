## Why

An agent definition currently has no way to bound the graph data it can see.
Whatever object types and relationship types exist in a project are visible to
every agent the project runs, regardless of what that agent is for. A
specialist agent (a legal-reasoning agent, a support triage agent) is forced to
wade through — and can accidentally reason over — types that are irrelevant to
its job, and operators cannot keep sensitive or out-of-scope types away from a
given agent short of not storing them at all. The MCP tool layer (`search-*`,
`entity-query`, `entity-search`, `graph-traverse`, `entity-type-list`, …)
enforces no per-agent data boundary, so an agent's reach is only as narrow as
the queries the model happens to write.

Refs #1695.

## What Changes

- Add two per-agent-definition config keys — `objectTypes: []string` and
  `relationshipTypes: []string` — stored in the existing
  `kb.agent_definitions.config` JSONB blob. **Empty or absent = all types**
  (backwards compatible). Nil-safe readers; setting an empty list removes the
  key. No migration.
- Derive the type scope once per agent run from the definition inside the
  executor's `runPipeline`, and thread it into the MCP tool layer via context
  (`mcp.NewTypeScope` / `mcp.ContextWithTypeScope` / `mcp.TypeScopeFromContext`),
  so every execution surface — HTTP trigger, A2A `message:send`, chat sessions,
  `/v1/chat/completions`, resume, and sub-agents — enforces the **same** scope.
  HTTP MCP transports with no agent scope remain unrestricted (unchanged).
- Enforce the scope server-side, per tool class:
  - search tools (`search-hybrid`, `search-semantic`, `search-similar`):
    intersect the requested `types` argument with the object allowlist; a
    request naming only excluded types yields an empty result (never a fallback
    to "all"); results never contain an excluded type.
  - single-type query tools (`entity-query`, `entity-search`): a type outside
    the allowlist is rejected with a clear error.
  - `graph-traverse`: excluded relationship types are rejected/intersected, and
    returned relationships and objects are filtered. `relationship-list` /
    `entity-edges-get`: returned relationships are filtered by the relationship
    allowlist.
  - listing/advertising tools (`entity-type-list`, `schema-get`,
    `schema-compiled-types`): only allowed object/relationship types are
    advertised, so excluded types are never offered.
  - audit/read tools (`migration-archive-list`, `migration-archive-get`):
    type-filter results against the object allowlist and reject/omit excluded
    types. `journal-list`: drops entries whose affected graph type is excluded on
    either axis, including relationship-created events whose `object_type` column
    is empty (type carried in metadata) and merge/batch events that name types in
    metadata — writing the relationship type into `object_type` at creation time
    and recursively scrubbing metadata so no excluded type name survives in any
    field.
    Fuzzy key-suggestion errors (the `entity-query` / `entity-search`
    key-not-found path) no longer name an excluded type. `schema-migrate-preview`
    fails closed under any scope, because its plan embeds type names that cannot
    be coherently scrubbed.
  - delegation: synchronous/in-process sub-agents (`trigger_agent` sync,
    `spawn_agents`, resume) inherit the caller's scope via context, intersected
    with the target definition's own allowlist; async/queued `trigger_agent`
    (which cannot carry the caller's scope) refuses delegation when the target
    definition's allowlist would widen beyond the caller's scope.
  - Empty allowlist ⇒ byte-for-byte current behaviour.
- Expose the keys through the agent-definition REST API (create/update request
  DTOs and the read DTO) and through the agent **General settings** form in the
  web UI.

## Capabilities

### New Capabilities

- `agent-object-type-scoping`: how an agent definition's object-type and
  relationship-type allowlists bound what the MCP tool layer will see and return,
  derived once and enforced identically on every execution surface.

### Modified Capabilities

- `agent-definitions`: the read/write contract gains the per-agent
  `objectTypes` and `relationshipTypes` allowlists (config keys `objectTypes` /
  `relationshipTypes`, default empty = all types), surfaced on create, update,
  and read.
- `web-agent-settings`: the agent General settings form gains a type-scope
  control that persists the object-type and relationship-type allowlists.

## Impact

- `apps/server/domain/agents/entity.go` — `AgentDefinition.ObjectTypes()` /
  `RelationshipTypes()` readers and the canonical config keys.
- `apps/server/domain/agents/dto.go` + `handler.go` — `objectTypes` /
  `relationshipTypes` on the read/create/update DTOs and their mapping.
- `apps/server/domain/agents/executor.go` — derive the scope in `runPipeline`
  and thread it through context for the tool layer; delegation paths inherit and
  intersect the caller's scope (sync/in-process) or refuse widening
  (async/queued).
- `apps/server/.../mcp` tool layer — `TypeScope` plumbing
  (`NewTypeScope`/`ContextWithTypeScope`/`TypeScopeFromContext`) and per-tool
  enforcement across search, single-type query, graph-traverse,
  listing/advertising, and audit/read tools (`migration-archive-*`, `journal-list`,
  fuzzy key-suggestion error paths).
- `apps/web-ui/gateway/agent.go` + `agent.templ` — General form type-scope
  control and mapping.
- No database migration. No new dependencies. No breaking API change: absent
  `objectTypes`/`relationshipTypes` preserves today's unrestricted behaviour on
  every surface.

## Follow-up (not in scope)

The issue implies the scope could eventually extend beyond data types (for
example a per-agent property filter, or a project-wide default scope). This
change implements exactly the object-type + relationship-type allowlist and
leaves property-level or project-default scoping as a documented follow-up.

Known limitations found during review and deliberately deferred:

- **Writes are not scoped.** The allowlist bounds read/query/list paths (the
  issue's stated goal: what an agent can *see/query*). `entity-create` /
  `relationship-create` / update paths are unchanged, so a scoped agent could
  still create a type outside its allowlist (and the create response echoes it).
  Extending the boundary to writes is a follow-up.
- **`search-knowledge` fails closed under any scope.** Its HTTP loopback spawns
  a separate, unrestricted internal agent, so the scope cannot be enforced
  without threading it through the query endpoint. Rather than leak, the tool
  returns a clear error for scoped agents (use `search-hybrid` /
  `search-semantic`). Threading the scope through the loopback is a follow-up.
- **`relationship-list` pagination metadata** (`total` / `has_more`) reflects the
  pre-filter page when a relationship scope is set, so a fully-scrubbed page can
  report `has_more: true`. Filtering before pagination is a follow-up. (The
  verbose `inverse_relationship` field was reviewed and is NOT a leak — it only
  repeats relationships already present in the result.)
- **`graph-traverse` dangling edges:** edges are relationship-scoped but their
  endpoints are not cross-checked against the object allowlist, so a filtered
  edge may reference an object id absent from the returned nodes. No type leak;
  reference hygiene is a follow-up.
- **`entity-history`** returns only version metadata (no type/name/properties)
  and is not type-scoped, so a scoped agent can still see version history for a
  type outside its allowlist. Scoping version history is a follow-up.
- **`memory://schema/relationships` resource** remains unscoped. It is served
  only over an HTTP MCP transport with no agent scope (agent-less by design), so
  it is an accepted inconsistency rather than a per-agent leak.
