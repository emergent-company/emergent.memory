# agent-object-type-scoping Specification

## Purpose
Server-side enforcement of an agent definition's graph-data boundary. An agent
definition SHALL be able to declare the object types and relationship types it
may see, and the MCP tool layer SHALL enforce that boundary identically on every
execution surface. The scope is derived once per agent run from the definition
and threaded into the tool layer via context, so the same definition yields the
same scope whether it is invoked through the trusted HTTP trigger, A2A
`message:send`, a chat session, the OpenAI-compatible `/v1/chat/completions`
surface, a resumed run, or a delegated sub-agent. An empty or absent allowlist
SHALL mean "all types" and reproduce the current, unrestricted behaviour
byte-for-byte.

## Requirements

### Requirement: The type scope is derived once and threaded to every surface

The effective type scope SHALL be derived once per agent run from the agent
definition's `objectTypes` / `relationshipTypes` allowlists inside the
executor's `runPipeline`, and SHALL be carried into the MCP tool layer via
context (`mcp.NewTypeScope` / `mcp.ContextWithTypeScope` /
`mcp.TypeScopeFromContext`). Every execution surface — HTTP trigger, A2A, chat
sessions, `/v1/chat/completions`, resume, and sub-agents — SHALL resolve the
scope through this single derivation path, so the scope is identical regardless
of entry point.

#### Scenario: Scope is identical across entry points

- **WHEN** the same agent definition runs on the HTTP trigger, A2A, a chat session, `/v1/chat/completions`, a resumed run, and a delegated sub-agent
- **THEN** each surface applies the same object-type and relationship-type allowlists to the tool layer

#### Scenario: The tool layer reads the scope from context

- **WHEN** an MCP tool call executes during an agent run
- **THEN** the scope is read from the request context via `TypeScopeFromContext`, never reconstructed from a second fetch of the definition

#### Scenario: Agent-less transports remain unrestricted

- **WHEN** an HTTP MCP transport is invoked with no agent scope in context
- **THEN** no type-scope restriction is applied and behaviour is unchanged from today

### Requirement: Search tools intersect requested types with the allowlist

For the search tools (`search-hybrid`, `search-semantic`, `search-similar`), the
requested `types` argument SHALL be intersected with the object-type allowlist.
A request that names only excluded types SHALL yield an empty result set and
SHALL NOT fall back to searching all types. Returned results SHALL never contain
an excluded object type. When the allowlist is empty, the intersection SHALL be
a no-op and the tools SHALL behave exactly as today.

#### Scenario: Requested types are narrowed to the allowlist

- **WHEN** a search tool is called with `types` naming both allowed and excluded object types
- **THEN** only the allowed types are searched and no result carries an excluded type

#### Scenario: Only-excluded request returns empty, never all

- **WHEN** a search tool is called with `types` naming only types outside the allowlist
- **THEN** the result set is empty, rather than degrading to a search over all types

#### Scenario: Empty allowlist leaves search unchanged

- **WHEN** a search tool is called against an agent with an empty or absent allowlist
- **THEN** the search behaves exactly as it does without scoping

### Requirement: Single-type query tools reject excluded types

For the single-type query tools (`entity-query`, `entity-search`), a request
whose target type is outside the object-type allowlist SHALL be rejected with a
clear error. A request for an allowed type SHALL proceed unchanged. An empty
allowlist SHALL NOT restrict these tools.

#### Scenario: Excluded type is rejected

- **WHEN** `entity-query` or `entity-search` is called for an object type outside the allowlist
- **THEN** the call fails with a clear error naming the excluded type and no result is returned

#### Scenario: Allowed type is accepted

- **WHEN** `entity-query` or `entity-search` is called for an object type inside the allowlist
- **THEN** the call proceeds and returns results as normal

#### Scenario: Empty allowlist does not restrict

- **WHEN** `entity-query` or `entity-search` is called against an agent with an empty or absent allowlist
- **THEN** the call proceeds as it does without scoping

### Requirement: Graph tools filter relationship types

`graph-traverse` SHALL reject or intersect excluded relationship types, and SHALL
filter returned relationships and objects so no excluded type is surfaced. When
the object axis is restricted, `graph-traverse` SHALL ALSO drop an edge when an
endpoint resolves (by physical or canonical id) to a returned node whose type is
OUTSIDE the object allowlist, so an allowed relationship cannot reference an
object the object allowlist removed. An endpoint that does NOT resolve to a
returned node — e.g. one on another page of a paginated traversal — has an
unknown type and SHALL NOT cause the edge to be dropped, because the unscoped
traverse service already keeps an edge when either endpoint is on the page.
`relationship-list` and `entity-edges-get` SHALL filter returned relationships
by the relationship allowlist.
`relationship-list` SHALL apply that allowlist as a query filter BEFORE
pagination, so its `total` and `has_more` metadata describe the filtered set
rather than the raw pre-filter page; an explicit `type` argument outside the
allowlist SHALL be rejected. An empty relationship allowlist SHALL leave these
tools unchanged.

#### Scenario: Excluded traversal relationship is not followed

- **WHEN** `graph-traverse` requests a relationship type outside the relationship allowlist
- **THEN** that relationship type is not traversed (rejected or intersected out) and no object reached only through it is returned

#### Scenario: Edges to a returned excluded-type node are dropped

- **WHEN** `graph-traverse` runs with an object-type allowlist (with or without a relationship allowlist) and an edge's endpoint resolves to a returned node whose type is outside the object allowlist
- **THEN** that edge is absent from the result

#### Scenario: Edges to an off-page or unknown endpoint are kept

- **WHEN** `graph-traverse` runs with an object-type allowlist and an edge's endpoint does not resolve to a returned node (for example it is on a later page of a paginated traversal)
- **THEN** the edge is kept, because the endpoint's type is unknown and the unscoped traverse service keeps edges whose other endpoint is on the page

#### Scenario: Returned relationships are filtered

- **WHEN** `relationship-list` or `entity-edges-get` returns relationships for a scoped agent
- **THEN** only relationship types within the relationship allowlist appear in the result

#### Scenario: relationship-list pagination metadata reflects the filtered set

- **WHEN** `relationship-list` runs for an agent with a relationship allowlist and the raw page contains excluded relationship types
- **THEN** `total` counts only allowed relationships and `has_more` is true only when more allowed relationships remain, so a fully-scrubbed page never reports `has_more: true`

#### Scenario: Empty relationship allowlist is unrestricted

- **WHEN** `graph-traverse`, `relationship-list`, or `entity-edges-get` runs against an agent with an empty or absent relationship allowlist
- **THEN** behaviour matches the unscoped, current implementation

### Requirement: Listing and advertising tools advertise only allowed types

The listing and advertising tools (`entity-type-list`, `schema-get`,
`schema-compiled-types`) SHALL advertise only object and relationship types that
are within the relevant allowlist, so an excluded type is never offered to the
agent. An empty allowlist SHALL advertise all types as today.

#### Scenario: Excluded types are not advertised

- **WHEN** `entity-type-list`, `schema-get`, or `schema-compiled-types` runs for a scoped agent
- **THEN** the advertised object and relationship types contain no type outside the allowlist

#### Scenario: Empty allowlist advertises everything

- **WHEN** one of these tools runs against an agent with an empty or absent allowlist
- **THEN** the full catalogue is advertised, unchanged

### Requirement: Empty allowlist reproduces current behaviour

An empty or absent `objectTypes` / `relationshipTypes` allowlist SHALL mean "all
types", and SHALL reproduce the current, unscoped behaviour of every affected
tool byte-for-byte. No scoped agent with an empty allowlist SHALL observe a
difference from today's output.

#### Scenario: No behavioural change for unscoped agents

- **WHEN** an agent definition has no `objectTypes` and no `relationshipTypes`
- **THEN** every affected tool returns exactly the results it returns today

### Requirement: Audit and read tools never surface excluded types

`migration-archive-list` and `migration-archive-get` SHALL type-filter their
results against the object-type allowlist, rejecting a request for — and never
returning — an excluded type. `journal-list` SHALL omit journal entries whose
affected graph type is outside the allowlist, on BOTH axes: object-typed entries
against the object allowlist and relationship-typed entries against the
relationship allowlist. This SHALL hold even for relationship events whose
`object_type` column is empty (the relationship type carried in the entry's
metadata) and for aggregate merge/batch events whose metadata names excluded
types: no excluded object or relationship type name SHALL survive in any
top-level column or nested metadata field. Relationship-created events SHALL
also record the relationship type in the `object_type` column at write time so
the column filter catches them. Fuzzy key-suggestion errors (the key-not-found
path of `entity-query` / `entity-search`) SHALL NOT name an excluded type. An
empty allowlist SHALL leave all three behaviours unchanged.

#### Scenario: Migration archive is type-filtered

- **WHEN** a scoped agent lists or gets a migration archive whose affected type is outside the object allowlist
- **THEN** the excluded type's archive is not returned (the request is rejected or the entry omitted), and no excluded type appears in the result

#### Scenario: Journal entries for excluded types are dropped

- **WHEN** `journal-list` runs for a scoped agent
- **THEN** any journal entry whose affected graph type is outside the object allowlist is omitted from the result

#### Scenario: Relationship events do not leak excluded relationship types

- **WHEN** `journal-list` runs for a scoped agent and the journal contains a relationship-created event whose `object_type` column is empty and whose `metadata.rel_type` is an excluded relationship type (or a merge/batch event whose metadata names an excluded type)
- **THEN** the excluded relationship or object type name, and the keys of the excluded entry, appear nowhere in the result — neither in a top-level column nor in any nested metadata field

#### Scenario: Fuzzy suggestions do not leak excluded types

- **WHEN** a key-not-found error from `entity-query` or `entity-search` surfaces for a scoped agent
- **THEN** the error message never names an excluded type

#### Scenario: Empty allowlist leaves these tools unchanged

- **WHEN** `migration-archive-list`, `migration-archive-get`, `journal-list`, or a fuzzy-suggestion path runs against an agent with an empty or absent allowlist
- **THEN** behaviour matches the unscoped, current implementation

### Requirement: Delegation inherits and narrows the caller's scope

When a scoped agent delegates to a sub-agent through a synchronous or in-process
path (`trigger_agent` sync, `spawn_agents`, or resume), the sub-agent SHALL
inherit the caller's scope via context, and its effective scope SHALL be the
INTERSECTION of the caller's scope and the target definition's own allowlist, so
a delegated run never sees more types than its caller. When `trigger_agent` is
queued asynchronously and therefore cannot carry the caller's scope, the
delegation SHALL be REFUSED if the target definition's declared allowlist would
widen beyond the caller's scope — that is, if the target allows any object or
relationship type the caller does not.

#### Scenario: Synchronous delegation intersects the two scopes

- **WHEN** a caller scoped to types `{A, B}` triggers a sub-agent whose definition allows `{A, C}`
- **THEN** the sub-agent's effective scope is `{A}` and it cannot reach type `C`

#### Scenario: Synchronous delegation is bounded by an unrestricted target

- **WHEN** a caller scoped to types `{A, B}` triggers a sub-agent whose definition allowlist is empty (all types)
- **THEN** the sub-agent's effective scope is `{A, B}`, still bounded by the caller

#### Scenario: Asynchronous queued delegation refuses widening

- **WHEN** a scoped caller queues `trigger_agent` for a target whose declared allowlist widens beyond the caller's scope
- **THEN** the delegation is refused with a clear error and the queued run does not start

#### Scenario: Asynchronous queued delegation proceeds when contained

- **WHEN** a scoped caller queues `trigger_agent` for a target whose declared allowlist is within the caller's scope
- **THEN** the queued delegation proceeds

### Requirement: A scoped agent cannot reach an excluded type through any read surface

A scoped agent SHALL NOT be able to reach an excluded object or relationship
type through ANY read/query/list/advertise tool. Every read path SHALL either
filter excluded types out, reject a request naming an excluded type, or fail
closed. `search-knowledge` SHALL thread the caller's scope through its internal
`/query` loopback — the request SHALL carry the caller's object- and
relationship-type allowlists and the internal graph-query-agent SHALL run under
that scope — so a scoped agent receives correctly scoped results rather than an
error, and no excluded type is ever returned. The internal agent's own
definition carries no allowlist, so the threaded scope is preserved end-to-end
and can only NARROW the unrestricted internal run, never widen it.

#### Scenario: No read tool surfaces an excluded type

- **WHEN** a scoped agent invokes any read/query/list/advertise tool with a query that would otherwise surface an excluded type
- **THEN** the excluded type is never present in the result, either because the tool filtered it, rejected the request, or failed closed

#### Scenario: search-knowledge runs scoped instead of failing closed

- **WHEN** a scoped agent calls `search-knowledge`
- **THEN** the caller's object- and relationship-type allowlists are threaded through the `/query` loopback into the internal agent's run, the call returns a scoped answer rather than an error, and no excluded type appears in the result

### Requirement: Write tools enforce the object- and relationship-type allowlists

An agent's `objectTypes` / `relationshipTypes` allowlists SHALL bound writes as
well as reads. `entity-create` SHALL reject creating an object whose type is
outside the object allowlist. `relationship-create` SHALL reject creating a
relationship whose type is outside the relationship allowlist. `entity-update`
and `relationship-update` SHALL reject updating an object or relationship whose
current type is outside the relevant allowlist (an update cannot change a type).
Inline relationships declared on an entity-create spec whose relationship type is
excluded SHALL NOT be created. A rejection SHALL be a clear error that does not
surface the excluded type name beyond what the caller's own request already
contains. An empty or absent allowlist SHALL leave every write path unchanged.

#### Scenario: Excluded object create is rejected

- **WHEN** a scoped agent calls `entity-create` for an object type outside the object allowlist
- **THEN** the create is rejected, the object is not created, and the response does not surface the excluded type name

#### Scenario: Allowed object create succeeds

- **WHEN** a scoped agent calls `entity-create` for an object type inside the object allowlist
- **THEN** the object is created as normal

#### Scenario: Excluded relationship create is rejected

- **WHEN** a scoped agent calls `relationship-create` for a relationship type outside the relationship allowlist
- **THEN** the relationship is not created and the response does not surface the excluded type name

#### Scenario: Excluded type update is rejected

- **WHEN** a scoped agent calls `entity-update` or `relationship-update` for an object or relationship whose current type is outside the relevant allowlist
- **THEN** the update is rejected and no change is applied

#### Scenario: Excluded inline relationship is not created

- **WHEN** a scoped agent creates an allowed object whose spec declares an inline relationship of an excluded relationship type
- **THEN** the object is created, the inline relationship is not created, and the response does not contain it

#### Scenario: Empty allowlist leaves writes unchanged

- **WHEN** a write tool runs against an agent with an empty or absent allowlist
- **THEN** the write proceeds exactly as it does without scoping

### Requirement: entity-history is type-scoped

`entity-history` SHALL reject a history request for an object whose type is
outside the object-type allowlist, with a clear error that does not surface the
excluded type name. A history request for an allowed type SHALL proceed
unchanged. An empty or absent allowlist SHALL leave `entity-history` unchanged.

#### Scenario: Excluded type history is rejected

- **WHEN** a scoped agent calls `entity-history` for an object whose type is outside the object allowlist
- **THEN** the call fails with a clear error that does not name the excluded type, and no version metadata is returned

#### Scenario: Allowed type history works

- **WHEN** a scoped agent calls `entity-history` for an object whose type is inside the object allowlist
- **THEN** the version history is returned as normal

#### Scenario: Empty allowlist leaves entity-history unchanged

- **WHEN** `entity-history` runs against an agent with an empty or absent allowlist
- **THEN** behaviour matches the unscoped, current implementation
