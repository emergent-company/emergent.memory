## REMOVED Requirements

### Requirement: A scoped agent cannot reach an excluded type through any read tool

## MODIFIED Requirements

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

## ADDED Requirements

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
